package broker

import (
	"bytes"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

var setupLine = regexp.MustCompile(`berth: setup: open http://127\.0\.0\.1:7690/setup#token=([0-9a-f]{32})\n`)

func setupBroker(t *testing.T) (*testBroker, *bytes.Buffer) {
	t.Helper()
	tb := newTestBroker(t)
	tb.logins.Delete()
	log := &bytes.Buffer{}
	tb.b.Log = log
	tb.b.Host = HostFromEnv(func(string) string { return "" })
	tb.b.Load()
	tb.b.Reconcile()
	return tb, log
}

func tokenFrom(t *testing.T, log *bytes.Buffer) string {
	t.Helper()
	m := setupLine.FindStringSubmatch(log.String())
	if m == nil {
		t.Fatalf("no setup line in log:\n%s", log)
	}
	return m[1]
}

func TestSetupMode(t *testing.T) {
	tb, log := setupBroker(t)
	token := tokenFrom(t, log)
	if got := tb.svc.lastNames(); !slices.Equal(got, []string{"panel"}) {
		t.Errorf("setup runs %v", got)
	}
	if code, out := tb.call(t, "GET", "/mode", ""); code != 200 || out["mode"] != "setup" {
		t.Errorf("mode = %d %v", code, out)
	}
	if code, out := tb.verb(t, "auth.verify", `{"user":"me","password":"x"}`); code != 409 || errCode(out) != "setup_required" {
		t.Errorf("verify in setup = %d %v", code, out)
	}

	for body, want := range map[string]string{
		`{"token":"` + strings.Repeat("0", 32) + `","user":"me","password":"correct horse battery"}`:              "invalid_token",
		`{"token":"` + token + `","user":"two words","password":"correct horse battery"}`:                         "invalid_login",
		`{"token":"` + token + `","user":"me","password":"short"}`:                                                "invalid_login",
		`{"token":"` + token + `","user":"me","password":"correct horse battery","patch":{"agents":{"keep":99}}}`: "invalid_config",
	} {
		if code, out := tb.verb(t, "setup.complete", body); errCode(out) != want {
			t.Errorf("%.60s: %d %v", body, code, out)
		}
	}
	if ok, _ := tb.logins.Exists(); ok {
		t.Fatal("a rejected setup wrote a login")
	}

	code, out := tb.verb(t, "setup.complete",
		`{"token":"`+token+`","user":"me","password":"correct horse battery","patch":{"frontends":{"ide":{"enabled":true}}}}`)
	if code != 200 || out["user"] != "me" {
		t.Fatalf("setup = %d %v", code, out)
	}
	if got := tb.svc.lastNames(); !slices.Equal(got, []string{"panel", "ttyd", "code_server"}) {
		t.Errorf("after setup runs %v", got)
	}
	if code, out := tb.verb(t, "setup.complete", `{"token":"`+token+`","user":"me","password":"correct horse battery"}`); code != 409 || errCode(out) != "not_in_setup" {
		t.Errorf("token reuse = %d %v", code, out)
	}
	if code, out := tb.verb(t, "auth.verify", `{"user":"me","password":"correct horse battery"}`); code != 200 || out["valid"] != true {
		t.Errorf("verify = %d %v", code, out)
	}
}

func TestSetupWithoutPatchKeepsDefaults(t *testing.T) {
	tb, log := setupBroker(t)
	code, out := tb.verb(t, "setup.complete", `{"token":"`+tokenFrom(t, log)+`","user":"me","password":"correct horse battery"}`)
	if code != 200 || out["config"].(map[string]any)["revision"] != float64(0) {
		t.Fatalf("setup = %d %v", code, out)
	}
}

func TestSetupRepairsCorruptConfig(t *testing.T) {
	tb := newTestBroker(t)
	tb.logins.Delete()
	writeCorrupt(t, tb.dir)
	log := &bytes.Buffer{}
	tb.b.Log = log
	tb.b.Load()
	if _, mode, _ := tb.b.snapshot(); mode != ModeSetup {
		t.Fatalf("mode = %s; setup must win over recovery", mode)
	}
	code, out := tb.verb(t, "setup.complete", `{"token":"`+tokenFrom(t, log)+`","user":"me","password":"correct horse battery","patch":{"agents":{"keep":5}}}`)
	if code != 200 {
		t.Fatalf("setup = %d %v", code, out)
	}
	if _, mode, _ := tb.b.snapshot(); mode != ModeNormal {
		t.Errorf("mode = %s", mode)
	}
}

func TestAuthVerify(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.Load()
	for body, want := range map[string]bool{
		`{"user":"me","password":"correct horse battery"}`:  true,
		`{"user":"me","password":"wrong"}`:                  false,
		`{"user":"you","password":"correct horse battery"}`: false,
	} {
		if code, out := tb.verb(t, "auth.verify", body); code != 200 || out["valid"] != want {
			t.Errorf("%s = %d %v", body, code, out)
		}
	}
}

func TestAuthVerifyIsRateLimited(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.Load()
	start := time.Now()
	for range 3 {
		tb.verb(t, "auth.verify", `{"user":"me","password":"wrong"}`)
	}
	if took := time.Since(start); took < 500*time.Millisecond {
		t.Errorf("3 verifications took %s; want at least 2 × %s", took, verifyInterval)
	}
}

func TestAuthChange(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.Load()
	if code, out := tb.verb(t, "auth.change", `{"current_password":"wrong","user":"you","password":"another long one"}`); code != 403 || errCode(out) != "wrong_password" {
		t.Errorf("wrong current = %d %v", code, out)
	}
	if code, out := tb.verb(t, "auth.change", `{"current_password":"correct horse battery","user":"you","password":"short"}`); code != 400 || errCode(out) != "invalid_login" {
		t.Errorf("short = %d %v", code, out)
	}
	code, out := tb.verb(t, "auth.change", `{"current_password":"correct horse battery","user":"you","password":"another long one"}`)
	if code != 200 || out["user"] != "you" {
		t.Fatalf("change = %d %v", code, out)
	}
	if ok, _ := tb.logins.Verify("you", "another long one"); !ok {
		t.Error("new login not stored")
	}
}

func TestAuthResetReturnsToSetup(t *testing.T) {
	tb := newTestBroker(t)
	log := &bytes.Buffer{}
	tb.b.Log = log
	tb.b.Host = HostFromEnv(func(string) string { return "" })
	tb.b.Load()
	tb.b.PeerUID = func(*http.Request) (int, bool) { return 0, true }
	code, out := tb.verb(t, "auth.reset", `{}`)
	if code != 200 || !strings.HasPrefix(out["setup_url"].(string), "http://127.0.0.1:7690/setup#token=") {
		t.Fatalf("reset = %d %v", code, out)
	}
	if ok, _ := tb.logins.Exists(); ok {
		t.Error("login survives reset")
	}
	if got := tb.svc.lastNames(); !slices.Equal(got, []string{"panel"}) {
		t.Errorf("services = %v", got)
	}
	tokenFrom(t, log)
}

func TestHost(t *testing.T) {
	env := map[string]string{"BERTH_HOST_BIND_ADDRESS": "0.0.0.0", "BERTH_HOST_PANEL_PORT": "8443",
		"BERTH_HOST_PUID": "1001", "BERTH_HOST_PGID": "100", "BERTH_HOST_WORKSPACES_DIR": "/srv/ws"}
	h := HostFromEnv(func(k string) string { return env[k] })
	if h != (Host{"0.0.0.0", "8443", "6768", "1001", "100", "/srv/ws"}) {
		t.Errorf("host = %+v", h)
	}
	if got := h.SetupURL("abc"); got != "http://localhost:8443/setup#token=abc" {
		t.Errorf("url = %s", got)
	}
	d := HostFromEnv(func(string) string { return "" })
	if d != (Host{"127.0.0.1", "7690", "6768", "1000", "1000", "./workspaces"}) {
		t.Errorf("defaults = %+v", d)
	}
	if got := (Host{BindAddress: "::1", PanelPort: "7690"}).SetupURL("t"); got != "http://[::1]:7690/setup#token=t" {
		t.Errorf("ipv6 url = %s", got)
	}
	tb := newTestBroker(t)
	tb.b.Host = h
	if tb.b.Status().Host.PanelPort != "8443" {
		t.Error("status lacks host")
	}
}
