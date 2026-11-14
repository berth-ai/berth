package toolstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/runner"
)

// fakeMise installs one executable per tool in the config's data dir.
type fakeMise struct {
	tools map[string]string
	fail  string
	cmds  []runner.Cmd
}

func (f *fakeMise) Run(_ context.Context, c runner.Cmd) error {
	f.cmds = append(f.cmds, c)
	if c.Args[0] == f.fail {
		return errors.New("exit status 1")
	}
	data := ""
	for _, kv := range c.Env {
		if v, ok := strings.CutPrefix(kv, "MISE_DATA_DIR="); ok {
			data = v
		}
	}
	switch c.Args[0] {
	case "install":
		for name, v := range f.tools {
			bin := filepath.Join(data, "installs", name, v, "bin")
			if err := os.MkdirAll(bin, 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(bin, "README"), nil, 0o644); err != nil {
				return err
			}
		}
	case "bin-paths":
		for name, v := range f.tools {
			fmt.Fprintln(c.Stdout, filepath.Join(data, "installs", name, v, "bin"))
		}
	}
	return nil
}

func installer(t *testing.T, r runner.Runner) Installer {
	t.Helper()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	return Installer{Store: agentstore.Store{Root: t.TempDir()}, Mise: "mise", Runner: r, Now: func() time.Time { return now }}
}

func TestInstall(t *testing.T) {
	tools := map[string]string{"gh": "2.104.0", "uv": "0.14.0"}
	r := &fakeMise{tools: tools}
	in := installer(t, r)
	v, built, err := in.Install(context.Background(), tools)
	if err != nil || !built {
		t.Fatalf("Install = %v, %v", built, err)
	}
	if v.Hash != Hash(tools) || v.Manifest.Packages["gh"] != "2.104.0" {
		t.Fatalf("version = %+v", v)
	}
	for _, c := range r.cmds {
		if !c.AsAgent || c.Dir != v.Dir {
			t.Errorf("mise %v ran as root or outside the set", c.Args)
		}
	}
	cfg, _ := os.ReadFile(filepath.Join(v.Dir, "mise.toml"))
	if !strings.Contains(string(cfg), `"gh" = "2.104.0"`) {
		t.Errorf("mise.toml = %s", cfg)
	}
	wrapper, err := os.ReadFile(filepath.Join(v.Dir, "wrappers", "gh"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "exec '" + filepath.Join(v.Dir, "mise", "installs", "gh", "2.104.0", "bin", "gh") + "' \"$@\""; !strings.Contains(string(wrapper), want) {
		t.Errorf("wrapper = %s", wrapper)
	}
	if _, err := os.Stat(filepath.Join(v.Dir, "wrappers", "README")); err == nil {
		t.Error("a non-executable got a wrapper")
	}
	if _, err := os.Stat(filepath.Join(v.Dir, ".cache")); err == nil {
		t.Error("the mise cache stayed in the set")
	}
	if info, _ := os.Stat(filepath.Join(v.Dir, "wrappers")); info.Mode().Perm()&0o022 != 0 {
		t.Error("the set is not sealed")
	}
	n := len(r.cmds)
	if _, built, err := in.Install(context.Background(), tools); err != nil || built || len(r.cmds) != n {
		t.Errorf("second Install rebuilt: %v, %v", built, err)
	}
}

// A descriptor agent kept open from the build must not reach the sealed set.
func TestReinodeOrphansOpenDescriptors(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tool")
	_ = os.WriteFile(bin, []byte("good"), 0o755)
	held, err := os.OpenFile(bin, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := reinode(dir); err != nil {
		t.Fatal(err)
	}
	_, _ = held.WriteAt([]byte("evil"), 0)
	got, _ := os.ReadFile(bin)
	if string(got) != "good" {
		t.Fatalf("the sealed file reads %q", got)
	}
	if info, _ := os.Stat(bin); info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v", info.Mode())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("leftovers: %v", entries)
	}
}

func TestInstallFailureRemovesTheSet(t *testing.T) {
	tools := map[string]string{"gh": "9.9.9"}
	in := installer(t, &fakeMise{tools: tools, fail: "install"})
	_, _, err := in.Install(context.Background(), tools)
	var se *agentstore.StepError
	if !errors.As(err, &se) || se.Step != "install" {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(in.Store.VersionDir(Hash(tools))); !errors.Is(err, os.ErrNotExist) {
		t.Error("the failed set stayed")
	}
	if _, _, err := in.Install(context.Background(), nil); err == nil {
		t.Error("an empty set installed")
	}
}

func TestWriteWrappers(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a/bin", "b/bin"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o755)
		_ = os.WriteFile(filepath.Join(root, d, "x"), nil, 0o755)
	}
	if _, err := WriteWrappers(filepath.Join(root, "w"), root, []string{filepath.Join(root, "a/bin"), filepath.Join(root, "b/bin")}); err == nil {
		t.Error("a duplicate name was accepted")
	}
	if _, err := WriteWrappers(filepath.Join(root, "w2"), root, []string{"/usr/bin"}); err == nil {
		t.Error("a bin dir outside the set was accepted")
	}
	names, err := WriteWrappers(filepath.Join(root, "w3"), root, []string{filepath.Join(root, "a/bin"), filepath.Join(root, "missing")})
	if err != nil || !slices.Equal(names, []string{"x"}) {
		t.Errorf("WriteWrappers = %v, %v", names, err)
	}
}

func TestHash(t *testing.T) {
	a := Hash(map[string]string{"gh": "1", "uv": "2"})
	if a != Hash(map[string]string{"uv": "2", "gh": "1"}) || a == Hash(map[string]string{"gh": "1", "uv": "3"}) || len(a) != 12 {
		t.Error("Hash is not a stable name for the set")
	}
}
