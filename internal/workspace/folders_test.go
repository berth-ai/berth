package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func mkdir(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// readOnly makes dir unwritable until the test ends; root ignores modes.
func readOnly(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	os.Chmod(dir, 0o555)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
}

func errCode(err error) string {
	var we *Error
	if errors.As(err, &we) {
		return we.Code
	}
	return ""
}

func TestProbeFolder(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "plain", "repo/.git/info", "worktree")
	os.WriteFile(filepath.Join(root, "worktree", ".git"), []byte("gitdir: /elsewhere\n"), 0o644)
	os.WriteFile(filepath.Join(root, "file"), nil, 0o644)
	for p, want := range map[string]Probe{
		"plain":    {Writable: true},
		"repo":     {Writable: true, Git: true},
		"worktree": {Writable: true, Git: true},
	} {
		if got, err := ProbeFolder(root, p); err != nil || got != want {
			t.Errorf("ProbeFolder(%s) = %+v, %v; want %+v", p, got, err, want)
		}
	}
	for p, code := range map[string]string{"missing": "missing", "file": "not_dir", "": "invalid_request", "../x": "invalid_request"} {
		if _, err := ProbeFolder(root, p); errCode(err) != code {
			t.Errorf("ProbeFolder(%q) = %v, want %s", p, err, code)
		}
	}
}

func TestProbeReadOnly(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "ro", "rorepo/.git/info")
	readOnly(t, filepath.Join(root, "ro"))
	readOnly(t, filepath.Join(root, "rorepo", ".git", "info"))
	if got, _ := ProbeFolder(root, "ro"); got != (Probe{}) {
		t.Errorf("ro = %+v", got)
	}
	if got, _ := ProbeFolder(root, "rorepo"); got != (Probe{Git: true}) {
		t.Errorf("rorepo = %+v; .git/info must be writable too", got)
	}
}

func TestCandidates(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "a/b", "a/c", "a-b", "repo/.git", "repo/sub", "ws/x", "holder/x", "holder/y", ".hidden/z", ".berth-tmp-1/q")
	os.WriteFile(filepath.Join(root, "file.txt"), nil, 0o644)
	os.Symlink(filepath.Join(root, "a"), filepath.Join(root, "link"))
	got, err := Candidates(root, []string{"ws", "holder/x"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Candidate{{"a", false}, {"a-b", false}, {"a/b", false}, {"a/c", false}, {"holder/y", false}, {"repo", true}}
	if !slices.Equal(got, want) {
		t.Errorf("Candidates = %+v\nwant         %+v", got, want)
	}
}

func TestRemove(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(outside, "keep"), []byte("x"), 0o644)
	mkdir(t, root, "acme/api/src")
	os.WriteFile(filepath.Join(root, "acme", "api", "src", "main.go"), []byte("package main"), 0o644)
	os.Symlink(outside, filepath.Join(root, "link"))
	os.Symlink(outside, filepath.Join(root, "door"))
	for _, p := range []string{"acme/api", "acme/api", "link"} {
		if res, err := Remove(root, p); err != nil || !res.Removed || res.Left == nil || len(res.Left) != 0 {
			t.Errorf("Remove(%s) = %+v, %v", p, res, err)
		}
	}
	if names := entries(t, root); !slices.Equal(names, []string{"acme", "door"}) {
		t.Errorf("root holds %v", names)
	}
	for _, p := range []string{"door/keep", "", ".", "../x", "acme/.git"} {
		if _, err := Remove(root, p); errCode(err) != "invalid_request" {
			t.Errorf("Remove(%q) = %v, want invalid_request", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Errorf("a file outside the root was touched: %v", err)
	}
}

func TestRemoveReportsLeftovers(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "acme/api/locked")
	os.WriteFile(filepath.Join(root, "acme", "api", "locked", "f"), nil, 0o644)
	readOnly(t, filepath.Join(root, "acme", "api", "locked"))
	res, err := Remove(root, "acme/api")
	if err != nil || res.Removed || !slices.Equal(res.Left, []string{"acme/api/locked/f"}) {
		t.Errorf("Remove = %+v, %v", res, err)
	}
}
