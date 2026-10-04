package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Dir: filepath.Join(t.TempDir(), "berth")}
}

func writeFile(t *testing.T, s *Store, name, content string) {
	t.Helper()
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingIsDefaults(t *testing.T) {
	c, err := newStore(t).Load()
	if err != nil || c.Revision != 0 || c.Agents.Keep != 3 {
		t.Fatalf("Load = %+v %v", c, err)
	}
}

func TestApplyWritesNextRevision(t *testing.T) {
	s := newStore(t)
	old, cur, err := s.Apply(json.RawMessage(`{"agents":{"keep":7,"disabled":["z-pkg","a-pkg"]}}`), 0)
	if err != nil {
		t.Fatal(err)
	}
	if old.Revision != 0 || cur.Revision != 1 || cur.Agents.Keep != 7 {
		t.Fatalf("old %+v cur %+v", old, cur)
	}
	if strings.Join(cur.Agents.Disabled, ",") != "a-pkg,z-pkg" {
		t.Errorf("disabled not normalized: %v", cur.Agents.Disabled)
	}
	loaded, err := s.Load()
	if err != nil || loaded.Revision != 1 || loaded.Agents.Keep != 7 {
		t.Fatalf("reload = %+v %v", loaded, err)
	}
	for _, name := range []string{"config.json", "config.prev.json"} {
		fi, err := os.Stat(filepath.Join(s.Dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", name, fi.Mode())
		}
	}
	entries, _ := os.ReadDir(s.Dir)
	if len(entries) != 2 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestApplyConflict(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.Apply(json.RawMessage(`{"agents":{"keep":4}}`), 0); err != nil {
		t.Fatal(err)
	}
	old, cur, err := s.Apply(json.RawMessage(`{"agents":{"keep":5}}`), 0)
	if !errors.Is(err, ErrConflict) || old.Revision != 1 || cur.Agents.Keep != 4 {
		t.Fatalf("stale apply = %+v %v", cur, err)
	}
}

func TestApplyInvalidWritesNothing(t *testing.T) {
	s := newStore(t)
	for _, patch := range []string{`{"agents":{"keep":99}}`, `{"agents":{"nope":1}}`} {
		_, _, err := s.Apply(json.RawMessage(patch), 0)
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("%s: err = %v", patch, err)
		}
	}
	if _, err := os.Stat(s.Dir); !os.IsNotExist(err) {
		t.Errorf("store dir created: %v", err)
	}
}

func TestLoadCorrupt(t *testing.T) {
	for name, content := range map[string]string{
		"not json":     `{"schema":`,
		"newer schema": `{"schema":5}`,
		"unknown key":  `{"schema":1,"revision":1,"surprise":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			writeFile(t, s, "config.json", content)
			_, err := s.Load()
			var ce *CorruptError
			if !errors.As(err, &ce) {
				t.Fatalf("err = %v", err)
			}
			if name == "newer schema" && !strings.Contains(err.Error(), "newer than this version") {
				t.Errorf("message = %q", err)
			}
		})
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	s := newStore(t)
	writeFile(t, s, "config.json", `{"schema":1,"revision":3,"agents":{"keep":-1}}`)
	var ce *CorruptError
	if _, err := s.Load(); !errors.As(err, &ce) || !strings.Contains(err.Error(), "agents.keep") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFillsMissingKeysWithDefaults(t *testing.T) {
	s := newStore(t)
	writeFile(t, s, "config.json", `{"schema":1,"revision":2,"agents":{"keep":5}}`)
	c, err := s.Load()
	if err != nil || c.Agents.Keep != 5 || c.Agents.Ref != "main" || c.Runtime.TZ != "UTC" {
		t.Fatalf("Load = %+v %v", c, err)
	}
}

func TestRevertToggles(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.Revert(); !errors.Is(err, ErrNoPrevious) {
		t.Fatalf("revert without previous: %v", err)
	}
	s.Apply(json.RawMessage(`{"agents":{"keep":4}}`), 0)
	s.Apply(json.RawMessage(`{"agents":{"keep":5}}`), 1)
	_, cur, err := s.Revert()
	if err != nil || cur.Agents.Keep != 4 || cur.Revision != 3 {
		t.Fatalf("revert = %+v %v", cur, err)
	}
	_, cur, err = s.Revert()
	if err != nil || cur.Agents.Keep != 5 || cur.Revision != 4 {
		t.Fatalf("second revert = %+v %v", cur, err)
	}
}

func TestResetFromCorrupt(t *testing.T) {
	s := newStore(t)
	s.Apply(json.RawMessage(`{"agents":{"keep":4}}`), 0)
	writeFile(t, s, "config.json", `garbage`)
	_, cur, err := s.Reset()
	if err != nil || cur.Agents.Keep != 3 || cur.Revision != 1 {
		t.Fatalf("reset = %+v %v", cur, err)
	}
	kept, err := os.ReadFile(filepath.Join(s.Dir, "config.corrupt.json"))
	if err != nil || string(kept) != "garbage" {
		t.Errorf("corrupt file not kept: %q %v", kept, err)
	}
	if c, err := s.Load(); err != nil || c.Revision != 1 {
		t.Errorf("Load after reset = %+v %v", c, err)
	}
}

func TestRevertFromCorrupt(t *testing.T) {
	s := newStore(t)
	s.Apply(json.RawMessage(`{"agents":{"keep":4}}`), 0)
	s.Apply(json.RawMessage(`{"agents":{"keep":6}}`), 1)
	writeFile(t, s, "config.json", `{"schema":9}`)
	_, cur, err := s.Revert()
	if err != nil || cur.Agents.Keep != 4 || cur.Revision != 2 {
		t.Fatalf("revert = %+v %v", cur, err)
	}
}

func TestWriteFileAtomicReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	for _, content := range []string{"one", "two"} {
		if err := WriteFileAtomic(path, []byte(content), 0o640, -1, -1); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(path)
	fi, _ := os.Stat(path)
	if string(got) != "two" || fi.Mode().Perm() != 0o640 {
		t.Errorf("content %q mode %v", got, fi.Mode())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left: %v", entries)
	}
}

func TestSchemaOneFileIsRead(t *testing.T) {
	s := newStore(t)
	writeFile(t, s, "config.json", `{"schema":1,"revision":4,"agents":{"keep":5}}`)
	c, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Schema != Schema || c.Revision != 4 || c.Agents.Keep != 5 || c.MCP.Servers == nil {
		t.Errorf("config = %+v", c)
	}
}

func TestReadUpgradesSchema2(t *testing.T) {
	dir := t.TempDir()
	raw := `{"schema":2,"revision":4,"mcp":{"servers":{},"available":{}}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := (&Store{Dir: dir}).Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Schema != Schema || c.Revision != 4 || c.Skills == nil {
		t.Errorf("loaded = %+v", c)
	}
}

func TestReadUpgradesSchema3(t *testing.T) {
	dir := t.TempDir()
	raw := `{"schema":3,"revision":7,"agents":{"ref":"main","disabled":["opencode-ai"],"keep":3,"boot_timeout":600,"mcp_timeout":120}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := (&Store{Dir: dir}).Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Schema != Schema || c.Revision != 7 || c.Agents.Added == nil || len(c.Agents.Added) != 0 ||
		!slices.Equal(c.Agents.Disabled, []string{"opencode-ai"}) {
		t.Errorf("loaded = %+v", c)
	}
}

func TestApplySortsAdded(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	_, next, err := s.Apply(json.RawMessage(`{"agents":{"added":["zz-agent","@acme/agent"]}}`), 0)
	if err != nil || !slices.Equal(next.Agents.Added, []string{"@acme/agent", "zz-agent"}) {
		t.Fatalf("next = %+v, %v", next.Agents, err)
	}
}
