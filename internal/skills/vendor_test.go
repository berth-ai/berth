package skills

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/wsstate"
)

func TestVendorCopiesAndLinks(t *testing.T) {
	w := newWorld(t)
	ws := repoFolder(t)
	w.publish(map[string]string{"api": ws})
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": inWS(config.ModeVendor, "api")})})
	w.mustRun()
	copyDir := filepath.Join(ws, ".agents", "skills", "pdf")
	if body, _ := os.ReadFile(filepath.Join(copyDir, "SKILL.md")); string(body) != pdfSkill {
		t.Fatalf("vendored SKILL.md = %q", body)
	}
	if fi, err := os.Lstat(copyDir); err != nil || !fi.IsDir() {
		t.Fatalf("the copy is not a folder: %v", err)
	}
	if got := readlink(filepath.Join(ws, ".c", "skills", "pdf")); got != "../../.agents/skills/pdf" {
		t.Errorf("c link = %q", got)
	}
	// Vendored files are meant for git: no exclude lines.
	if strings.Contains(exclude(t, ws), "pdf") {
		t.Errorf("exclude = %q", exclude(t, ws))
	}
	out, _ := exec.Command("git", "-C", ws, "status", "--porcelain", "--untracked-files=all").Output()
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if !strings.HasPrefix(l, "?? .agents/skills/pdf/") && l != "?? .c/skills/pdf" {
			t.Errorf("unexpected git status line %q", l)
		}
	}
	e := w.state("api").Skills.Entries["fx/pdf"]
	if e.Mode != config.ModeVendor || e.Dir != ".agents/skills/pdf" || !strings.HasPrefix(e.Files["SKILL.md"], "sha256:") ||
		!slices.Equal(e.Links, []string{".c/skills/pdf"}) {
		t.Errorf("state = %+v", e)
	}

	// An update rewrites an unedited copy.
	src := w.source(map[string]config.Attachment{"pdf": inWS(config.ModeVendor, "api")})
	src.Commit = w.repo.commit(t, map[string]string{"skills/pdf/new.md": "n"})
	w.write(map[string]config.Source{"fx": src})
	w.mustRun()
	if !exists(filepath.Join(copyDir, "new.md")) {
		t.Error("the copy was not updated")
	}

	// Detaching an unedited copy removes it and its links.
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{})})
	w.mustRun()
	if exists(filepath.Join(ws, ".agents")) || exists(filepath.Join(ws, ".c")) {
		t.Error("detach left the copy or its links")
	}
}

func TestVendorLocalEdits(t *testing.T) {
	w := newWorld(t)
	ws := repoFolder(t)
	w.publish(map[string]string{"api": ws})
	src := w.source(map[string]config.Attachment{"pdf": inWS(config.ModeVendor, "api")})
	w.write(map[string]config.Source{"fx": src})
	w.mustRun()
	edited := filepath.Join(ws, ".agents", "skills", "pdf", "SKILL.md")
	os.WriteFile(edited, []byte("my notes"), 0o644)

	src.Commit = w.repo.commit(t, map[string]string{"skills/pdf/new.md": "n"})
	w.write(map[string]config.Source{"fx": src})
	_, err := w.run()
	if err == nil || !strings.Contains(err.Error(), "pdf in api has local edits") {
		t.Fatalf("err = %v", err)
	}
	if body, _ := os.ReadFile(edited); string(body) != "my notes" {
		t.Error("the edit was overwritten")
	}
	if !w.state("api").Skills.Entries["fx/pdf"].Edited {
		t.Error("the entry is not marked edited")
	}

	// An added file counts as an edit too, for detach.
	os.WriteFile(edited, []byte(pdfSkill), 0o644)
	os.WriteFile(filepath.Join(ws, ".agents", "skills", "pdf", "extra.md"), []byte("x"), 0o644)
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{})})
	if _, err := w.run(); err == nil || !strings.Contains(err.Error(), "has local edits") {
		t.Errorf("detach with an added file = %v", err)
	}
	if !exists(edited) || readlink(filepath.Join(ws, ".c", "skills", "pdf")) == "" {
		t.Error("a stopped detach removed files or links")
	}

	// Overwrite lets the next sync discard the edits and finish the detach.
	store := wsstate.Store{Home: w.home, Published: w.wsFile}
	if err := Resolve(store, "api", "fx/pdf", ResolveOverwrite); err != nil {
		t.Fatal(err)
	}
	w.mustRun()
	if exists(filepath.Join(ws, ".agents")) {
		t.Error("overwrite did not remove the copy")
	}
}

func TestVendorRelease(t *testing.T) {
	w := newWorld(t)
	ws := repoFolder(t)
	w.publish(map[string]string{"api": ws})
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": inWS(config.ModeVendor, "api")})})
	w.mustRun()
	os.WriteFile(filepath.Join(ws, ".agents", "skills", "pdf", "SKILL.md"), []byte("mine now"), 0o644)
	store := wsstate.Store{Home: w.home, Published: w.wsFile}
	if err := Resolve(store, "api", "fx/pdf", ResolveRelease); err != nil {
		t.Fatal(err)
	}
	// The broker then drops the workspace from the attachment.
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{})})
	w.mustRun()
	if body, _ := os.ReadFile(filepath.Join(ws, ".agents", "skills", "pdf", "SKILL.md")); string(body) != "mine now" {
		t.Error("released files were touched")
	}
	if readlink(filepath.Join(ws, ".c", "skills", "pdf")) == "" {
		t.Error("released links were removed")
	}
	var se *Error
	if err := Resolve(store, "api", "fx/pdf", ResolveRelease); !errors.As(err, &se) || se.Code != "not_vendored" {
		t.Errorf("second release = %v", err)
	}
	if err := Resolve(store, "api", "fx/pdf", "bogus"); !errors.As(err, &se) || se.Code != "invalid_request" {
		t.Errorf("bad action = %v", err)
	}
}

func TestVendorModeSwitch(t *testing.T) {
	w := newWorld(t)
	ws := repoFolder(t)
	w.publish(map[string]string{"api": ws})
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": inWS(config.ModeLink, "api")})})
	w.mustRun()
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": inWS(config.ModeVendor, "api")})})
	w.mustRun()
	if fi, err := os.Lstat(filepath.Join(ws, ".agents", "skills", "pdf")); err != nil || !fi.IsDir() {
		t.Fatalf("link → vendor: %v", err)
	}
	if strings.Contains(exclude(t, ws), "pdf") {
		t.Error("vendor mode kept the exclude line")
	}
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": inWS(config.ModeLink, "api")})})
	w.mustRun()
	if got := readlink(filepath.Join(ws, ".c", "skills", "pdf")); got != w.export("fx", "pdf") {
		t.Errorf("vendor → link: c link = %q", got)
	}
	if exists(filepath.Join(ws, ".agents")) {
		t.Error("vendor → link kept the copy")
	}
}

func TestVendorLeavesUserFolder(t *testing.T) {
	w := newWorld(t)
	ws := repoFolder(t)
	own := filepath.Join(ws, ".agents", "skills", "pdf")
	os.MkdirAll(own, 0o755)
	os.WriteFile(filepath.Join(own, "SKILL.md"), []byte("theirs"), 0o644)
	w.publish(map[string]string{"api": ws})
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": inWS(config.ModeVendor, "api")})})
	w.mustRun()
	if body, _ := os.ReadFile(filepath.Join(own, "SKILL.md")); string(body) != "theirs" {
		t.Error("vendoring replaced the user's folder")
	}
	if w.state("api").Skills.Skipped[".agents/skills/pdf"] == "" {
		t.Error("the skipped folder is not reported")
	}
}

func TestTreeFiles(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "sub", "run.sh"), []byte("x"), 0o755)
	os.Symlink("a", filepath.Join(dir, "l"))
	dst := filepath.Join(t.TempDir(), "copy")
	os.Mkdir(dst, 0o755)
	files, err := copyTree(dir, dst)
	if err != nil || len(files) != 3 || files["l"] != "link:a" || !strings.HasPrefix(files["sub/run.sh"], "sha256:") {
		t.Fatalf("files = %v, %v", files, err)
	}
	if fi, _ := os.Stat(filepath.Join(dst, "sub", "run.sh")); fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v", fi.Mode())
	}
	if ok, _ := unedited(dst, files); !ok {
		t.Error("a fresh copy reads as edited")
	}
	if ok, _ := unedited(filepath.Join(dst, "missing"), files); !ok {
		t.Error("a deleted copy has nothing to protect")
	}
	os.Remove(filepath.Join(dst, "a"))
	if ok, _ := unedited(dst, files); ok {
		t.Error("a removed file reads as unedited")
	}
}
