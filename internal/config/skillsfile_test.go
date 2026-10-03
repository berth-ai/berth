package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills.json")
	in := map[string]Skill{"pdf": goodSkill()}
	if err := WriteSkillsFile(path, in, -1, -1); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", fi.Mode())
	}
	out, err := ReadSkillsFile(path)
	if err != nil || out["pdf"].Commit != goodCommit {
		t.Errorf("read = %+v, %v", out, err)
	}
}

func TestSkillsFileMissingAndNewer(t *testing.T) {
	dir := t.TempDir()
	if s, err := ReadSkillsFile(filepath.Join(dir, "none.json")); err != nil || s == nil || len(s) != 0 {
		t.Errorf("missing = %v, %v", s, err)
	}
	newer := filepath.Join(dir, "newer.json")
	os.WriteFile(newer, []byte(`{"schema":2,"skills":{}}`), 0o640)
	if _, err := ReadSkillsFile(newer); err == nil || !strings.Contains(err.Error(), "schema 2") {
		t.Errorf("newer = %v", err)
	}
}
