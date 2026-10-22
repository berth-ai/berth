package broker

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestGitIdentity(t *testing.T) {
	tb := newTestBroker(t)
	dir := fakeHome(t, tb)
	code, out := tb.verb(t, "git.identity", `{"name":"Ada Lovelace","email":"ada@example.org"}`)
	if code != 200 || out["name"] != "Ada Lovelace" {
		t.Fatalf("identity = %d %v", code, out)
	}
	if got := callLog(dir, "git"); got != "config --global user.name Ada Lovelace\nconfig --global user.email ada@example.org\n" {
		t.Errorf("git ran %q", got)
	}
}

func TestGitIdentityValidation(t *testing.T) {
	tb := newTestBroker(t)
	dir := fakeHome(t, tb)
	for _, body := range []string{
		`{"name":"-c x","email":"a@b.c"}`,
		`{"name":"two\nlines","email":"a@b.c"}`,
		`{"name":"","email":"a@b.c"}`,
		`{"name":"Ada","email":"not-an-email"}`,
		`{"name":"Ada","email":"-a@b.c"}`,
		`{"name":"Ada","email":"a b@c.d"}`,
	} {
		if code, out := tb.verb(t, "git.identity", body); code != 400 {
			t.Errorf("%s = %d %v", body, code, out)
		}
	}
	if callLog(dir, "git") != "" {
		t.Error("git ran for rejected input")
	}
}

func TestForgeStatus(t *testing.T) {
	tb := newTestBroker(t)
	fakeHome(t, tb)
	os.WriteFile(tb.b.Home.GH, []byte("#!/bin/sh\necho \"github.com\n  ✓ Logged in to github.com account octo (keyring)\" >&2\n"), 0o755)
	os.WriteFile(tb.b.Home.Glab, []byte("#!/bin/sh\necho 'gitlab.com: not logged in' >&2\nexit 1\n"), 0o755)
	os.WriteFile(tb.b.Home.Git, []byte("#!/bin/sh\ncase $4 in user.name) echo Ada ;; user.email) exit 1 ;; esac\n"), 0o755)
	withTea(t, tb, teaLogins)
	code, out := tb.verb(t, "forge.status", `{}`)
	if code != 200 {
		t.Fatalf("status = %d %v", code, out)
	}
	forges := out["forges"].(map[string]any)
	gh, glab := forges["gh"].(map[string]any), forges["glab"].(map[string]any)
	if gh["signed_in"] != true || gh["user"] != "octo" || gh["host"] != "github.com" || glab["signed_in"] != false {
		t.Errorf("forges = %v", forges)
	}
	tea := forges["tea"].(map[string]any)
	logins, _ := tea["logins"].([]any)
	if tea["signed_in"] != true || tea["host"] != "codeberg.org" || tea["user"] != "ada" || len(logins) != 2 ||
		logins[1].(map[string]any)["default"] != true || logins[0].(map[string]any)["default"] != false {
		t.Errorf("tea = %v", tea)
	}
	if _, ok := gh["logins"]; ok {
		t.Errorf("gh has logins: %v", gh)
	}
	git := out["git"].(map[string]any)
	if git["name"] != "Ada" || git["email"] != "" {
		t.Errorf("git = %v", git)
	}
}

func TestForgeStatusWithoutTea(t *testing.T) {
	tb := newTestBroker(t)
	fakeHome(t, tb)
	tb.b.Home.Tea = "/nonexistent/tea"
	code, out := tb.verb(t, "forge.status", `{}`)
	tea, _ := out["forges"].(map[string]any)["tea"].(map[string]any)
	if code != 200 || tea["signed_in"] != false || tea["logins"] != nil {
		t.Errorf("status = %d %v", code, out)
	}
}

const teaLogins = `[{"name":"git.example.org","url":"https://git.example.org","ssh_host":"git.example.org","user":"octo","default":"false"},` +
	`{"name":"codeberg.org","url":"https://codeberg.org","ssh_host":"codeberg.org","user":"ada","default":"true"}]`

// withTea makes tea list logins, record each call's argv and token, and fail
// a login add whose token is "bad" like the real one.
func withTea(t *testing.T, tb *testBroker, logins string) {
	t.Helper()
	os.WriteFile(tb.b.Home.Tea, []byte(`#!/bin/sh
echo "$*" >> "$0.log"
case $2 in
  list) printf '%s' '`+logins+`' ;;
  add) printf '%s' "$GITEA_SERVER_TOKEN" > "$0.token"
    if [ "$GITEA_SERVER_TOKEN" = bad ]; then
      printf 'Error: invalid username, password or token\naccess token does not exist [sha: bad]\n' >&2; exit 1
    fi ;;
esac
`), 0o755)
}

func TestForgeToken(t *testing.T) {
	tb := newTestBroker(t)
	fakeHome(t, tb)
	withTea(t, tb, teaLogins)
	code, out := tb.verb(t, "forge.token", `{"forge":"tea","url":" https://gitea.com/ ","token":"t0k3n"}`)
	if code != 200 || out["name"] != "gitea.com" || out["url"] != "https://gitea.com" {
		t.Fatalf("token = %d %v", code, out)
	}
	log, _ := os.ReadFile(tb.b.Home.Tea + ".log")
	if want := "login list --output json\nlogin add --name gitea.com --url https://gitea.com --git-credentials\n"; string(log) != want {
		t.Errorf("tea ran %q", log)
	}
	if strings.Contains(string(log), "t0k3n") {
		t.Error("the token reached argv")
	}
	if token, _ := os.ReadFile(tb.b.Home.Tea + ".token"); string(token) != "t0k3n" {
		t.Errorf("tea saw token %q", token)
	}

	code, out = tb.verb(t, "forge.token", `{"forge":"tea","url":"https://gitea.com","token":"bad"}`)
	if code != 502 || !strings.Contains(fmt.Sprint(out), "invalid username, password or token") ||
		strings.Contains(fmt.Sprint(out), "Error:") {
		t.Errorf("bad token = %d %v", code, out)
	}
	code, out = tb.verb(t, "forge.token", `{"forge":"tea","url":"https://codeberg.org/","token":"x"}`)
	if code != 409 || !strings.Contains(fmt.Sprint(out), "sign out first") {
		t.Errorf("existing login = %d %v", code, out)
	}
}

func TestForgeTokenValidation(t *testing.T) {
	tb := newTestBroker(t)
	fakeHome(t, tb)
	withTea(t, tb, "[]")
	for _, body := range []string{
		`{"forge":"gh","url":"https://github.com","token":"x"}`,
		`{"forge":"tea","url":"http://gitea.com","token":"x"}`,
		`{"forge":"tea","url":"gitea.com","token":"x"}`,
		`{"forge":"tea","url":"https://user:pw@gitea.com","token":"x"}`,
		`{"forge":"tea","url":"https://gitea.com?x=1","token":"x"}`,
		`{"forge":"tea","url":"https://-x","token":"x"}`,
		`{"forge":"tea","url":"https://gitea.com","token":""}`,
		`{"forge":"tea","url":"https://gitea.com","token":"a b"}`,
		`{"forge":"tea","url":"https://gitea.com","token":"x","extra":1}`,
	} {
		if code, out := tb.verb(t, "forge.token", body); code != 400 {
			t.Errorf("%s = %d %v", body, code, out)
		}
	}
	if log, _ := os.ReadFile(tb.b.Home.Tea + ".log"); strings.Contains(string(log), "add") {
		t.Errorf("tea ran for rejected input: %q", log)
	}
}

func TestForgeLogout(t *testing.T) {
	tb := newTestBroker(t)
	dir := fakeHome(t, tb)
	withTea(t, tb, teaLogins)
	if code, out := tb.verb(t, "forge.logout", `{"forge":"gh"}`); code != 200 {
		t.Errorf("gh = %d %v", code, out)
	}
	if got := callLog(dir, "gh"); got != "auth logout --hostname github.com\n" {
		t.Errorf("gh ran %q", got)
	}
	if code, out := tb.verb(t, "forge.logout", `{"forge":"tea","name":"codeberg.org"}`); code != 200 {
		t.Errorf("tea = %d %v", code, out)
	}
	if log, _ := os.ReadFile(tb.b.Home.Tea + ".log"); !strings.HasSuffix(string(log), "login delete codeberg.org\n") {
		t.Errorf("tea ran %q", log)
	}
	for body, want := range map[string]int{
		`{"forge":"tea","name":"gitea.com"}`: 404,
		`{"forge":"tea","name":"-h"}`:        400,
		`{"forge":"tea"}`:                    400,
		`{"forge":"glab","name":"x"}`:        400,
		`{"forge":"bitbucket"}`:              400,
	} {
		if code, out := tb.verb(t, "forge.logout", body); code != want {
			t.Errorf("%s = %d %v, want %d", body, code, out, want)
		}
	}
	os.WriteFile(tb.b.Home.Glab, []byte("#!/bin/sh\necho 'not logged in to gitlab.com' >&2\nexit 1\n"), 0o755)
	if code, out := tb.verb(t, "forge.logout", `{"forge":"glab"}`); code != 502 || !strings.Contains(fmt.Sprint(out), "not logged in") {
		t.Errorf("glab = %d %v", code, out)
	}
}

func TestForgeLogin(t *testing.T) {
	tb := newTestBroker(t)
	fakeHome(t, tb)
	os.WriteFile(tb.b.Home.GH, []byte("#!/bin/sh\necho \"$*\" > \"$0.args\"\necho '! First copy your one-time code: 1A2B-3C4D'\necho 'Open this URL to continue in your web browser: https://github.com/login/device'\n"), 0o755)
	code, out := tb.verb(t, "forge.login", `{"forge":"gh"}`)
	if code != 202 || out["job"].(map[string]any)["group"] != "forge" {
		t.Fatalf("login = %d %v", code, out)
	}
	tb.waitIdle(t)
	code, out = tb.call(t, "GET", "/jobs/1", "")
	device, _ := out["job"].(map[string]any)["device"].(map[string]any)
	if code != 200 || device["code"] != "1A2B-3C4D" || device["url"] != "https://github.com/login/device" {
		t.Errorf("job = %v", out)
	}
	args, _ := os.ReadFile(tb.b.Home.GH + ".args")
	if strings.TrimSpace(string(args)) != "auth login --hostname github.com --web --git-protocol https --skip-ssh-key" {
		t.Errorf("gh args = %q", args)
	}
	for _, body := range []string{`{"forge":"bitbucket"}`, `{"forge":"-gh"}`, `{}`} {
		if code, _ := tb.verb(t, "forge.login", body); code != 400 {
			t.Errorf("%s = %d", body, code)
		}
	}
}

func TestForgeLoginTimesOut(t *testing.T) {
	tb := newTestBroker(t)
	fakeHome(t, tb)
	tb.b.LoginTimeout = 300 * time.Millisecond
	os.WriteFile(tb.b.Home.Glab, []byte("#!/bin/sh\nsleep 30\n"), 0o755)
	tb.verb(t, "forge.login", `{"forge":"glab"}`)
	tb.waitIdle(t)
	job, _ := tb.b.Jobs.Get(1)
	if job.State != StateFailed {
		t.Errorf("state = %s", job.State)
	}
	if code, _ := tb.verb(t, "forge.login", `{"forge":"glab"}`); code != 202 {
		t.Errorf("forge group still busy: %d", code)
	}
}
