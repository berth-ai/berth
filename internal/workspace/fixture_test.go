package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repo is a local repository served over file://.
type repo struct {
	dir string
	url string
}

// newRepo creates a repository on branch main whose first commit holds
// files; the global and system git configs are ignored for the whole test.
func newRepo(t *testing.T, files map[string]string) *repo {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	r := &repo{dir: t.TempDir()}
	r.url = "file://" + r.dir
	gitIn(t, r.dir, "init", "-q", "-b", "main")
	r.commit(t, files)
	return r
}

// gitIn runs git in dir as a test user and returns its trimmed output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.org", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes files, commits them and returns the commit.
func (r *repo) commit(t *testing.T, files map[string]string) string {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(r.dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	gitIn(t, r.dir, "add", "-A")
	gitIn(t, r.dir, "commit", "-q", "-m", "c")
	return gitIn(t, r.dir, "rev-parse", "HEAD")
}

func testGit() Git { return Git{AllowFile: true} }

// entries lists the names in dir.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}
