package broker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// realGit points tb's broker at the real git, with a fresh home and no
// system config, and returns that home.
func realGit(t *testing.T, tb *testBroker) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	fakeHome(t, tb)
	tb.b.Home.Git = git
	home := filepath.Join(tb.dir, "home")
	os.MkdirAll(home, 0o755)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	return home
}

// userIn is the identity git uses in a fresh repository with remote url.
func userIn(t *testing.T, home, url string) string {
	t.Helper()
	repo := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		out, err := cmd.Output()
		if err != nil && !strings.Contains(strings.Join(args, " "), "config") {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	run("remote", "add", "origin", url)
	return run("config", "user.name") + " <" + run("config", "user.email") + ">"
}

func TestHostIdentityWithGit(t *testing.T) {
	tb := newTestBroker(t)
	home := realGit(t, tb)
	for _, body := range []string{
		`{"name":"Lab","email":"lab@example.org","host":"GitLab.com"}`,
		// The global identity, written after the include, must not win over it.
		`{"name":"Ada","email":"ada@example.org"}`,
		`{"name":"Tea","email":"tea@example.org","host":"git.example.org:3000"}`,
	} {
		if code, out := tb.verb(t, "git.identity", body); code != 200 {
			t.Fatalf("%s = %d %v", body, code, out)
		}
	}
	for url, want := range map[string]string{
		"https://gitlab.com/group/sub/repo.git":  "Lab <lab@example.org>",
		"git@gitlab.com:group/repo.git":          "Lab <lab@example.org>",
		"ssh://git@gitlab.com/group/repo.git":    "Lab <lab@example.org>",
		"https://github.com/octo/repo.git":       "Ada <ada@example.org>",
		"https://gitlab.com.example.org/a/b.git": "Ada <ada@example.org>",
		"https://git.example.org:3000/a/b.git":   "Tea <tea@example.org>",
		"https://git.example.org/a/b.git":        "Ada <ada@example.org>",
	} {
		if got := userIn(t, home, url); got != want {
			t.Errorf("%s: user = %s, want %s", url, got, want)
		}
	}

	_, out := tb.verb(t, "forge.status", `{}`)
	ids, _ := out["identities"].([]any)
	if len(ids) != 2 || ids[0].(map[string]any)["host"] != "git.example.org:3000" ||
		ids[1].(map[string]any)["host"] != "gitlab.com" || ids[1].(map[string]any)["email"] != "lab@example.org" {
		t.Errorf("identities = %v", out["identities"])
	}

	// Setting it again rewrites the includes instead of adding more.
	if code, out := tb.verb(t, "git.identity", `{"name":"Lab 2","email":"lab@example.org","host":"gitlab.com"}`); code != 200 {
		t.Fatalf("again = %d %v", code, out)
	}
	cfg, _ := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if n := strings.Count(string(cfg), "gitlab.com.gitconfig"); n != 3 {
		t.Errorf("%d gitlab.com includes:\n%s", n, cfg)
	}

	if code, out := tb.verb(t, "git.identity.remove", `{"host":"gitlab.com"}`); code != 200 {
		t.Fatalf("remove = %d %v", code, out)
	}
	if got := userIn(t, home, "https://gitlab.com/group/repo.git"); got != "Ada <ada@example.org>" {
		t.Errorf("after remove: user = %s", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".config/berth/git/gitlab.com.gitconfig")); !os.IsNotExist(err) {
		t.Errorf("identity file left behind: %v", err)
	}
	cfg, _ = os.ReadFile(filepath.Join(home, ".gitconfig"))
	if strings.Contains(string(cfg), "gitlab.com") {
		t.Errorf("includes left behind:\n%s", cfg)
	}
	if code, _ := tb.verb(t, "git.identity.remove", `{"host":"gitlab.com"}`); code != 404 {
		t.Errorf("second remove = %d", code)
	}
}

func TestHostIdentityKeepsForeignInclude(t *testing.T) {
	tb := newTestBroker(t)
	home := realGit(t, tb)
	mine := "[includeIf \"hasconfig:remote.*.url:https://github.com/**\"]\n\tpath = ~/.gitconfig-work\n"
	os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(mine), 0o644)
	code, out := tb.verb(t, "git.identity", `{"name":"Ada","email":"ada@example.org","host":"github.com"}`)
	if code != 409 || out["error"].(map[string]any)["code"] != "include_exists" {
		t.Errorf("identity = %d %v", code, out)
	}
	if cfg, _ := os.ReadFile(filepath.Join(home, ".gitconfig")); string(cfg) != mine {
		t.Errorf("config changed:\n%s", cfg)
	}
	if code, _ := tb.verb(t, "git.identity.remove", `{"host":"github.com"}`); code != 404 {
		t.Errorf("remove of a foreign include = %d", code)
	}
}

func TestHostIdentityValidation(t *testing.T) {
	tb := newTestBroker(t)
	dir := fakeHome(t, tb)
	for _, host := range []string{"-x", "a b", "a/b", "github.com/**", "a]b", `a"b`, "https://github.com", "a:b", "a..b"} {
		body := `{"name":"Ada","email":"a@b.cd","host":` + quote(host) + `}`
		if code, out := tb.verb(t, "git.identity", body); code != 400 {
			t.Errorf("%s = %d %v", body, code, out)
		}
		if code, out := tb.verb(t, "git.identity.remove", `{"host":`+quote(host)+`}`); code != 400 {
			t.Errorf("remove %q = %d %v", host, code, out)
		}
	}
	if callLog(dir, "git") != "" {
		t.Error("git ran for rejected input")
	}
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }
