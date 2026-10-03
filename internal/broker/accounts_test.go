package broker

import (
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
	code, out := tb.verb(t, "forge.status", `{}`)
	if code != 200 {
		t.Fatalf("status = %d %v", code, out)
	}
	forges := out["forges"].(map[string]any)
	gh, glab := forges["gh"].(map[string]any), forges["glab"].(map[string]any)
	if gh["signed_in"] != true || gh["user"] != "octo" || gh["host"] != "github.com" || glab["signed_in"] != false {
		t.Errorf("forges = %v", forges)
	}
	git := out["git"].(map[string]any)
	if git["name"] != "Ada" || git["email"] != "" {
		t.Errorf("git = %v", git)
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
