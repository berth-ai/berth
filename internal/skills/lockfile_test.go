package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if target, ok := strings.CutPrefix(body, "->"); ok {
			if err := os.Symlink(target, p); err != nil {
				t.Fatal(err)
			}
			continue
		}
		os.WriteFile(p, []byte(body), 0o644)
	}
}

// The test vectors the skills CLI publishes in its README.
func TestComputedHashVectors(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  string
	}{
		"none":      {nil, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		"one file":  {map[string]string{"SKILL.md": "# hi\n"}, "93c36952d8843b33e68ae970ebabcc03e1687c9bb0dd1ecaf357060318952f55"},
		"two files": {map[string]string{"SKILL.md": "a\n", "docs/b.md": "b\n"}, "b22c4d22aa319d2924312fa18fcbfedc88fa89c74dbd8f298c683e1caf7b7f24"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, tc.files)
			if got, err := ComputedHash(dir); err != nil || got != tc.want {
				t.Errorf("hash = %s, %v; want %s", got, err, tc.want)
			}
		})
	}
}

// Line endings, left-out names and symlinks follow the skills CLI.
func TestComputedHashRules(t *testing.T) {
	want := func(files map[string]string) string {
		dir := t.TempDir()
		writeTree(t, dir, files)
		h, err := ComputedHash(dir)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	base := want(map[string]string{"SKILL.md": "a\n", "docs/b.md": "b\n"})
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"secret": "s"})
	for name, files := range map[string]map[string]string{
		"crlf":        {"SKILL.md": "a\r\n", "docs/b.md": "b\r\n"},
		"left out":    {"SKILL.md": "a\n", "docs/b.md": "b\n", "metadata.json": "{}", ".git/HEAD": "x", "docs/__pycache__/c.pyc": "x"},
		"broken link": {"SKILL.md": "a\n", "docs/b.md": "b\n", "gone": "->nowhere"},
		"link out":    {"SKILL.md": "a\n", "docs/b.md": "b\n", "leak": "->" + filepath.Join(outside, "secret")},
		"loop":        {"SKILL.md": "a\n", "docs/b.md": "b\n", "docs/self": "->.."},
	} {
		if got := want(files); got != base {
			t.Errorf("%s: hash %s, want %s", name, got, base)
		}
	}
	if got := want(map[string]string{"SKILL.md": "a\n", "docs/b.md": "->../real.md", "real.md": "b\n"}); got == base {
		t.Error("a link inside the folder should count as its target, under its own path")
	}
	if got := want(map[string]string{"SKILL.md": "a\x00\r\n", "docs/b.md": "b\n"}); got == want(map[string]string{"SKILL.md": "a\x00\n", "docs/b.md": "b\n"}) {
		t.Error("CRLF in a binary file must stay")
	}
}

func TestReadLock(t *testing.T) {
	ws := t.TempDir()
	if l, err := ReadLock(ws); err != nil || l.Found {
		t.Fatalf("no lock = %+v %v", l, err)
	}
	writeTree(t, ws, map[string]string{
		".agents/skills/pdf/SKILL.md":  pdfSkill,
		".agents/skills/docx/SKILL.md": "edited",
	})
	pdfHash, _ := ComputedHash(filepath.Join(ws, ".agents/skills/pdf"))
	writeTree(t, ws, map[string]string{LockFileName: `{"version":2,"skills":{
		"pdf":{"source":"anthropics/skills","sourceType":"github","ref":"main","commit":"0123456789abcdef0123456789abcdef01234567","skillPath":"skills/pdf/SKILL.md","computedHash":"` + pdfHash + `"},
		"docx":{"source":"acme/skills","sourceUrl":"https://gitlab.example.org/acme/skills.git","sourceType":"git","skillPath":"docx/SKILL.md","computedHash":"` + pdfHash + `"},
		"xlsx":{"source":"acme/skills","sourceType":"gitlab","skillPath":"SKILL.md","computedHash":""},
		"guide":{"source":"docs.example.org","sourceUrl":"https://docs.example.org","sourceType":"well-known","computedHash":""},
		"local":{"source":"./skills/local","sourceType":"local","computedHash":""},
		"npm":{"source":"agent-browser","sourceType":"node_modules","computedHash":""},
		"alias":{"source":"acme/skills","sourceType":"github","skillName":"original","computedHash":""},
		"plain":{"source":"http://example.org/repo","sourceUrl":"http://example.org/repo","sourceType":"git","computedHash":""}
	}}`})
	l, err := ReadLock(ws)
	if err != nil || !l.Found || l.Version != 2 {
		t.Fatalf("lock = %+v %v", l, err)
	}
	got := map[string]LockSkill{}
	for _, s := range l.Skills {
		got[s.Name] = s
	}
	if s := got["pdf"]; s.Repo != "https://github.com/anthropics/skills" || s.Ref != "main" || s.Folder != "skills/pdf" ||
		s.Commit == "" || s.Installed != LockMatch || s.Reason != "" {
		t.Errorf("pdf = %+v", s)
	}
	if s := got["docx"]; s.Repo != "https://gitlab.example.org/acme/skills" || s.Ref != "HEAD" || s.Folder != "docx" || s.Installed != LockDiffers {
		t.Errorf("docx = %+v", s)
	}
	if s := got["xlsx"]; s.Repo != "https://gitlab.com/acme/skills" || s.Folder != "" || s.Installed != LockAbsent {
		t.Errorf("xlsx = %+v", s)
	}
	if s := got["guide"]; s.Kind != "well-known" || s.Repo != "https://docs.example.org" {
		t.Errorf("guide = %+v", s)
	}
	for _, name := range []string{"local", "npm", "alias", "plain"} {
		if s := got[name]; s.Repo != "" || s.Reason == "" {
			t.Errorf("%s should not be addable: %+v", name, s)
		}
	}
	writeTree(t, ws, map[string]string{LockFileName: "<<<<<<< HEAD\n{}"})
	if _, err := ReadLock(ws); err == nil || !strings.Contains(err.Error(), "merge conflict") {
		t.Errorf("conflicted lock = %v", err)
	}
	writeTree(t, ws, map[string]string{LockFileName: `{"version":9,"skills":{}}`})
	if _, err := ReadLock(ws); err == nil || !strings.Contains(err.Error(), "version 9") {
		t.Errorf("future lock = %v", err)
	}
}
