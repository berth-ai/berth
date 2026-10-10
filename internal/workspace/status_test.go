package workspace

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStatus(t *testing.T) {
	r := newRepo(t, map[string]string{"a.txt": "a", "b.txt": "b"})
	os.WriteFile(filepath.Join(r.dir, "a.txt"), []byte("changed"), 0o644)
	os.WriteFile(filepath.Join(r.dir, "new.txt"), []byte("new"), 0o644)
	detached := newRepo(t, map[string]string{"a.txt": "a"})
	gitIn(t, detached.dir, "checkout", "-q", "--detach")
	plain := t.TempDir()
	tm, _ := fakeTmux(t, `list-windows) printf 'ws-repo\tclaude\nws-repo\tzsh\nmain\tzsh\n' ;;`)
	got := Status(context.Background(), Git{}, tm, map[string]string{
		"repo": r.dir, "detached": detached.dir, "plain": plain, "gone": filepath.Join(plain, "gone"),
	}, time.Minute)
	want := map[string]State{
		"repo": {Exists: true, Writable: true, Git: true, Branch: "main", Head: gitIn(t, r.dir, "rev-parse", "--short=12", "HEAD"),
			Dirty: 2, Session: true, Windows: []string{"claude", "zsh"}},
		"detached": {Exists: true, Writable: true, Git: true, Head: gitIn(t, detached.dir, "rev-parse", "--short=12", "HEAD"),
			Windows: []string{}},
		"plain": {Exists: true, Writable: true, Dirty: -1, Windows: []string{}},
		"gone":  {Dirty: -1, Windows: []string{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Status =\n%+v\nwant\n%+v", got, want)
	}
}

func TestStatusBudgetAndNoServer(t *testing.T) {
	r := newRepo(t, map[string]string{"a.txt": "a"})
	slow := filepath.Join(t.TempDir(), "git")
	os.WriteFile(slow, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755)
	tm, _ := fakeTmux(t, `list-windows) echo "no server running on /tmp/tmux-1000/default" >&2; exit 1 ;;`)
	start := time.Now()
	got := Status(context.Background(), Git{Path: slow}, tm, map[string]string{"repo": r.dir}, 200*time.Millisecond)
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("Status took %v; the budget is 200ms", d)
	}
	if want := (map[string]State{"repo": {Unknown: true, Dirty: -1, Windows: []string{}}}); !reflect.DeepEqual(got, want) {
		t.Errorf("Status = %+v", got)
	}
}
