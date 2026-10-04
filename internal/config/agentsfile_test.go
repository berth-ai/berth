package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestAgentsFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if got, err := ReadAgentsFile(path); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("missing file = %v, %v", got, err)
	}
	if err := WriteAgentsFile(path, []string{"@github/copilot"}, -1, -1); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadAgentsFile(path); err != nil || !slices.Equal(got, []string{"@github/copilot"}) {
		t.Errorf("read = %v, %v", got, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", fi.Mode())
	}
	for _, bad := range []string{`{"schema":2,"added":[]}`, `{"schema":1,"added":["Bad Name"]}`, `{`} {
		os.WriteFile(path, []byte(bad), 0o640)
		if _, err := ReadAgentsFile(path); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}
