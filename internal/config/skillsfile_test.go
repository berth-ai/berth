package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills.json")
	in := map[string]Source{"anthropics-skills": goodSource()}
	if err := WriteSkillsFile(path, in, -1, -1); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", fi.Mode())
	}
	if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), `"schema": 2`) {
		t.Errorf("file = %s", raw)
	}
	out, err := ReadSkillsFile(path)
	if err != nil || out["anthropics-skills"].Commit != goodCommit || out["anthropics-skills"].Found["pdf"].Path != "skills/pdf" {
		t.Errorf("read = %+v, %v", out, err)
	}
}

func TestSkillsFileMissingAndOtherSchemas(t *testing.T) {
	dir := t.TempDir()
	if s, err := ReadSkillsFile(filepath.Join(dir, "none.json")); err != nil || s == nil || len(s) != 0 {
		t.Errorf("missing = %v, %v", s, err)
	}
	for _, schema := range []string{"1", "3"} {
		p := filepath.Join(dir, schema+".json")
		os.WriteFile(p, []byte(`{"schema":`+schema+`,"skills":{}}`), 0o640)
		if _, err := ReadSkillsFile(p); err == nil || !strings.Contains(err.Error(), "schema "+schema) {
			t.Errorf("schema %s = %v", schema, err)
		}
	}
}
