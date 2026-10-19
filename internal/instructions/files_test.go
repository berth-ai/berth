package instructions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/wsstate"
)

func sum(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// filesWorld publishes workspace api at a fresh git repository.
func filesWorld(t *testing.T, shared bool) (wsstate.Store, string) {
	t.Helper()
	ws := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", ws).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	pub := filepath.Join(t.TempDir(), "workspaces.json")
	config.WriteWorkspacesFile(pub, map[string]config.PublishedWorkspace{"api": {Path: ws, Shared: shared}}, -1, -1)
	return wsstate.Store{Home: t.TempDir(), Published: pub}, ws
}

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestValidName(t *testing.T) {
	for name, ok := range map[string]bool{"CLAUDE.md": true, "AGENTS.md": true, "a/CLAUDE.md": false, "../x.md": false, ".x.md": false, "x.txt": false} {
		if ValidName(name) != ok {
			t.Errorf("ValidName(%q) = %v", name, !ok)
		}
	}
}

func TestReadStates(t *testing.T) {
	store, ws := filesWorld(t, false)
	ctx := context.Background()
	f, err := Read(ctx, store, "", "api", "CLAUDE.md")
	if err != nil || f.State != "missing" || !f.Editable || f.SHA256 != "" {
		t.Fatalf("missing = %+v, %v", f, err)
	}
	os.WriteFile(filepath.Join(ws, "CLAUDE.md"), []byte("# hi\n"), 0o644)
	f, _ = Read(ctx, store, "", "api", "CLAUDE.md")
	if f.State != "file" || f.Content != "# hi\n" || f.SHA256 != sum("# hi\n") || f.Size != 5 || f.Tracked {
		t.Errorf("file = %+v", f)
	}
	exec.Command("git", "-C", ws, "add", "CLAUDE.md").Run()
	if f, _ = Read(ctx, store, "", "api", "CLAUDE.md"); !f.Tracked {
		t.Error("tracked file not reported")
	}
	os.Symlink("AGENTS.md", filepath.Join(ws, "GEMINI.md"))
	f, _ = Read(ctx, store, "", "api", "GEMINI.md")
	if f.State != "symlink" || f.Target != "AGENTS.md" || f.Editable || f.Owned || f.Content != "" {
		t.Errorf("symlink = %+v", f)
	}
	if _, err := Read(ctx, store, "", "api", "../x.md"); code(err) != "not_allowed" {
		t.Errorf("bad name: %v", err)
	}
	if _, err := Read(ctx, store, "", "nope", "CLAUDE.md"); !errors.Is(err, wsstate.ErrGone) {
		t.Errorf("unknown workspace: %v", err)
	}
}

func TestReadNotEditable(t *testing.T) {
	store, ws := filesWorld(t, false)
	big := strings.Repeat("x", MaxSize+1)
	os.WriteFile(filepath.Join(ws, "CLAUDE.md"), []byte(big), 0o644)
	f, err := Read(context.Background(), store, "", "api", "CLAUDE.md")
	if err != nil || f.Editable || f.Content != "" || f.Size != int64(MaxSize+1) || !strings.Contains(f.Reason, "256 KiB") {
		t.Errorf("big = %+v, %v", f, err)
	}
	if _, err := Write(store, "api", "CLAUDE.md", "small", f.SHA256); code(err) != "too_large" && f.SHA256 == "" {
		t.Errorf("write over a big file: %v", err)
	}
	if raw, _ := os.ReadFile(filepath.Join(ws, "CLAUDE.md")); len(raw) != MaxSize+1 {
		t.Error("the big file changed")
	}
	os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte("ok \xff\xfe"), 0o644)
	if f, _ := Read(context.Background(), store, "", "api", "AGENTS.md"); f.Editable || !strings.Contains(f.Reason, "UTF-8") {
		t.Errorf("binary = %+v", f)
	}
	os.Mkdir(filepath.Join(ws, "GEMINI.md"), 0o755)
	if f, _ := Read(context.Background(), store, "", "api", "GEMINI.md"); f.State != "other" || f.Editable {
		t.Errorf("folder = %+v", f)
	}
}

func TestWrite(t *testing.T) {
	store, ws := filesWorld(t, false)
	p := filepath.Join(ws, "CLAUDE.md")
	h, err := Write(store, "api", "CLAUDE.md", "one\n", "")
	if err != nil || h != sum("one\n") {
		t.Fatalf("new = %s, %v", h, err)
	}
	if raw, _ := os.ReadFile(p); string(raw) != "one\n" {
		t.Errorf("content = %q", raw)
	}
	os.Chmod(p, 0o600)
	if _, err := Write(store, "api", "CLAUDE.md", "two\n", h); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode not kept: %v", fi.Mode())
	}
	if _, err := Write(store, "api", "CLAUDE.md", strings.Repeat("x", MaxSize+1), sum("two\n")); code(err) != "too_large" {
		t.Errorf("too large: %v", err)
	}
	if _, err := Write(store, "api", "CLAUDE.md", "\xff", sum("two\n")); code(err) != "not_text" {
		t.Errorf("not text: %v", err)
	}
	os.Symlink("AGENTS.md", filepath.Join(ws, "GEMINI.md"))
	if _, err := Write(store, "api", "GEMINI.md", "x", ""); code(err) != "is_symlink" {
		t.Errorf("through a link: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("the write went through the link")
	}
}

func TestWriteChangedOnDisk(t *testing.T) {
	store, ws := filesWorld(t, false)
	p := filepath.Join(ws, "CLAUDE.md")
	os.WriteFile(p, []byte("theirs\n"), 0o644)
	if _, err := Write(store, "api", "CLAUDE.md", "mine\n", sum("before\n")); code(err) != "changed_on_disk" {
		t.Fatalf("stale base: %v", err)
	}
	if _, err := Write(store, "api", "CLAUDE.md", "mine\n", ""); code(err) != "changed_on_disk" {
		t.Fatalf("new over an existing file: %v", err)
	}
	if raw, _ := os.ReadFile(p); string(raw) != "theirs\n" {
		t.Errorf("disk changed: %q", raw)
	}
	os.Remove(p)
	if _, err := Write(store, "api", "CLAUDE.md", "mine\n", sum("theirs\n")); code(err) != "changed_on_disk" {
		t.Errorf("deleted meanwhile: %v", err)
	}
}

func TestMerge(t *testing.T) {
	store, ws := filesWorld(t, true)
	os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte("# Shared"), 0o644)
	os.WriteFile(filepath.Join(ws, "CLAUDE.md"), []byte("# Claude\nrules\n"), 0o644)
	if err := Merge(store, "api", "CLAUDE.md"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(ws, "AGENTS.md")); string(raw) != "# Shared\n\n# Claude\nrules\n" {
		t.Errorf("AGENTS.md = %q", raw)
	}
	if readlink(filepath.Join(ws, "CLAUDE.md")) != Shared {
		t.Error("CLAUDE.md is not a link")
	}
	st, _ := store.Load("api")
	if len(st.Instructions.Links) != 1 || st.Instructions.Links[0] != "CLAUDE.md" {
		t.Errorf("state = %+v", st.Instructions)
	}
	if !strings.Contains(exclude(t, ws), "/CLAUDE.md") {
		t.Error("link not excluded")
	}
	// A missing AGENTS.md is created from the file.
	store2, ws2 := filesWorld(t, true)
	os.WriteFile(filepath.Join(ws2, "GEMINI.md"), []byte("g\n"), 0o644)
	if err := Merge(store2, "api", "GEMINI.md"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(ws2, "AGENTS.md")); string(raw) != "g\n" {
		t.Errorf("new AGENTS.md = %q", raw)
	}
}

func TestMergeRefuses(t *testing.T) {
	off, ws := filesWorld(t, false)
	os.WriteFile(filepath.Join(ws, "CLAUDE.md"), []byte("c"), 0o644)
	if err := Merge(off, "api", "CLAUDE.md"); code(err) != "not_shared" {
		t.Errorf("not shared: %v", err)
	}
	store, ws := filesWorld(t, true)
	if err := Merge(store, "api", "AGENTS.md"); code(err) != "not_allowed" {
		t.Errorf("AGENTS.md into itself: %v", err)
	}
	if err := Merge(store, "api", "CLAUDE.md"); code(err) != "not_a_file" {
		t.Errorf("missing file: %v", err)
	}
	os.WriteFile(filepath.Join(ws, "CLAUDE.md"), []byte("c"), 0o644)
	os.Symlink("CLAUDE.md", filepath.Join(ws, "AGENTS.md"))
	if err := Merge(store, "api", "CLAUDE.md"); code(err) != "agents_md_symlink" {
		t.Errorf("symlinked AGENTS.md: %v", err)
	}
	os.Remove(filepath.Join(ws, "AGENTS.md"))
	os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte(strings.Repeat("a", MaxSize-1)), 0o644)
	if err := Merge(store, "api", "CLAUDE.md"); code(err) != "too_large" {
		t.Errorf("too large: %v", err)
	}
	if raw, _ := os.ReadFile(filepath.Join(ws, "CLAUDE.md")); string(raw) != "c" {
		t.Error("a refused merge changed CLAUDE.md")
	}
}
