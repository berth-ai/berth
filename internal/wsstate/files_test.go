package wsstate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// gitRepo makes an empty git repository and returns its folder.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return dir
}

func read(t *testing.T, p string) string {
	t.Helper()
	raw, _ := os.ReadFile(p)
	return string(raw)
}

func TestWriteExcludeKeepsUserLines(t *testing.T) {
	ws := gitRepo(t)
	ex := filepath.Join(ws, ".git", "info", "exclude")
	os.WriteFile(ex, []byte("# mine\n*.log"), 0o644)
	if err := WriteExclude(ws, []string{"/.claude/skills/pdf", "/.mcp.json"}); err != nil {
		t.Fatal(err)
	}
	want := "# mine\n*.log\n" + BlockStart + "\n/.claude/skills/pdf\n/.mcp.json\n" + BlockEnd + "\n"
	if got := read(t, ex); got != want {
		t.Fatalf("exclude =\n%s\nwant\n%s", got, want)
	}
	// A user line added after the block survives a rewrite.
	os.WriteFile(ex, []byte(want+"tmp/\n"), 0o644)
	if err := WriteExclude(ws, []string{"/.mcp.json"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, ex); got != "# mine\n*.log\ntmp/\n"+BlockStart+"\n/.mcp.json\n"+BlockEnd+"\n" {
		t.Fatalf("rewrite = %q", got)
	}
	if err := WriteExclude(ws, nil); err != nil {
		t.Fatal(err)
	}
	if got := read(t, ex); got != "# mine\n*.log\ntmp/\n" {
		t.Fatalf("emptied = %q", got)
	}
	// git honours the block.
	os.MkdirAll(filepath.Join(ws, "x"), 0o755)
	os.WriteFile(filepath.Join(ws, "x", "f"), []byte("x"), 0o644)
	WriteExclude(ws, []string{"/x"})
	if out, _ := exec.Command("git", "-C", ws, "status", "--porcelain").Output(); strings.TrimSpace(string(out)) != "" {
		t.Errorf("git status = %s", out)
	}
}

func TestWriteExcludeRefusesBrokenBlock(t *testing.T) {
	ws := gitRepo(t)
	ex := filepath.Join(ws, ".git", "info", "exclude")
	os.WriteFile(ex, []byte(BlockStart+"\n/x\nuser line\n"), 0o644)
	if err := WriteExclude(ws, nil); err == nil {
		t.Error("an unterminated block was rewritten")
	}
	if got := read(t, ex); got != BlockStart+"\n/x\nuser line\n" {
		t.Errorf("file changed: %q", got)
	}
}

func TestExcludePath(t *testing.T) {
	plain := t.TempDir()
	if p, err := ExcludePath(plain); p != "" || err != nil {
		t.Errorf("not a repository = %q %v", p, err)
	}
	repo := gitRepo(t)
	if p, _ := ExcludePath(repo); p != filepath.Join(repo, ".git", "info", "exclude") {
		t.Errorf("repository = %q", p)
	}
	// A worktree: .git is a file naming its gitdir, whose commondir is the
	// main repository's .git.
	wt := t.TempDir()
	gitdir := filepath.Join(repo, ".git", "worktrees", "wt")
	os.MkdirAll(gitdir, 0o755)
	os.WriteFile(filepath.Join(gitdir, "commondir"), []byte("../..\n"), 0o644)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644)
	if p, _ := ExcludePath(wt); p != filepath.Join(repo, ".git", "info", "exclude") {
		t.Errorf("worktree = %q", p)
	}
	link := t.TempDir()
	os.Symlink(filepath.Join(repo, ".git"), filepath.Join(link, ".git"))
	if _, err := ExcludePath(link); err == nil {
		t.Error("a symlinked .git was followed")
	}
}

func TestPlainRefusesSymlinks(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(ws, ".claude"))
	os.Symlink(filepath.Join(outside, "x.json"), filepath.Join(ws, ".mcp.json"))
	for rel, bad := range map[string]bool{
		".claude/skills": true, ".claude/skills/pdf": true, ".mcp.json": true,
		".agents/skills": false, "opencode.json": false,
	} {
		if err := Plain(ws, rel); (err != nil) != bad {
			t.Errorf("Plain(%s) = %v", rel, err)
		}
	}
	if _, err := MkdirAll(ws, ".claude/skills"); err == nil {
		t.Error("MkdirAll went through a symlink")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("wrote outside the workspace: %v", entries)
	}
}

func TestMkdirAllAndRemoveEmpty(t *testing.T) {
	ws := t.TempDir()
	os.MkdirAll(filepath.Join(ws, ".claude"), 0o755)
	made, err := MkdirAll(ws, ".claude/skills/deep")
	if err != nil || !slices.Equal(made, []string{".claude/skills", ".claude/skills/deep"}) {
		t.Fatalf("made = %v, %v", made, err)
	}
	os.WriteFile(filepath.Join(ws, ".claude", "skills", "keep"), nil, 0o644)
	if left := RemoveEmpty(ws, made); !slices.Equal(left, []string{".claude/skills"}) {
		t.Errorf("left = %v", left)
	}
	if _, err := os.Stat(filepath.Join(ws, ".claude")); err != nil {
		t.Error("removed a folder Berth did not create")
	}
	if got := AddDirs([]string{"b"}, "a", "b"); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("AddDirs = %v", got)
	}
}

func TestTracked(t *testing.T) {
	ws := gitRepo(t)
	os.WriteFile(filepath.Join(ws, ".mcp.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(ws, "opencode.json"), []byte("{}"), 0o644)
	exec.Command("git", "-C", ws, "add", ".mcp.json").Run()
	ctx := context.Background()
	if ok, err := Tracked(ctx, "", ws, ".mcp.json"); !ok || err != nil {
		t.Errorf(".mcp.json = %v %v", ok, err)
	}
	if ok, err := Tracked(ctx, "", ws, "opencode.json"); ok || err != nil {
		t.Errorf("opencode.json = %v %v", ok, err)
	}
	if ok, err := Tracked(ctx, "", t.TempDir(), ".mcp.json"); ok || err != nil {
		t.Errorf("no repository = %v %v", ok, err)
	}
}
