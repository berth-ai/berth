package skills

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixture is a local repository served over file://.
type fixture struct {
	dir string
	url string
}

// newFixture creates a repository whose first commit holds files; the
// global and system git configs are ignored for the whole test.
func newFixture(t *testing.T, files map[string]string) *fixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := &fixture{dir: t.TempDir()}
	f.url = "file://" + f.dir
	f.git(t, "init", "-q", "-b", "main")
	f.git(t, "config", "uploadpack.allowAnySHA1InWant", "true")
	f.commit(t, files)
	return f
}

func (f *fixture) git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.org", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = f.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes files (a "->" prefix makes a symlink) and returns the commit.
func (f *fixture) commit(t *testing.T, files map[string]string) string {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(f.dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if target, ok := strings.CutPrefix(body, "->"); ok {
			os.Symlink(target, p)
			continue
		}
		os.WriteFile(p, []byte(body), 0o644)
	}
	f.git(t, "add", "-A")
	f.git(t, "commit", "-q", "-m", "c")
	return f.git(t, "rev-parse", "HEAD")
}

const pdfSkill = "---\nname: pdf\ndescription: PDF tools\n---\n# PDF\n"

func testGit() Git { return Git{AllowFile: true} }
