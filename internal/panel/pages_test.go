package panel

import (
	"strings"
	"testing"
)

func TestPagesAndAssets(t *testing.T) {
	e := newEnv(t, "normal")
	c := e.client()
	resp, body := e.do(c, "GET", "/login", "", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, `id="login-form"`) || !strings.Contains(body, `src="/static/auth.js"`) {
		t.Errorf("login page = %d", resp.StatusCode)
	}
	for _, path := range []string{"/static/auth.js", "/static/panel.js", "/static/panel.css"} {
		if resp, body := e.do(c, "GET", path, "", nil); resp.StatusCode != 200 || body == "" {
			t.Errorf("%s = %d", path, resp.StatusCode)
		}
	}
	e.login(c)
	if _, body := e.do(c, "GET", "/", "", nil); !strings.Contains(body, `id="logout"`) {
		t.Error("index lacks a sign-out button")
	}
	se := newEnv(t, "setup")
	_, body = se.do(se.client(), "GET", "/setup", "", nil)
	for _, id := range []string{"setup-form", "password2", "terminal", "ide", "no-token"} {
		if !strings.Contains(body, `id="`+id+`"`) {
			t.Errorf("setup page lacks #%s", id)
		}
	}
}

func TestPagesHaveNoInlineScript(t *testing.T) {
	for _, name := range []string{"index.html", "login.html", "setup.html"} {
		raw, _ := static.ReadFile("static/" + name)
		if strings.Contains(string(raw), "<script>") || strings.Contains(string(raw), "onclick=") {
			t.Errorf("%s has inline script; the CSP forbids it", name)
		}
	}
}

func TestSetupPageNeverQueriesToken(t *testing.T) {
	raw, _ := static.ReadFile("static/auth.js")
	js := string(raw)
	if strings.Contains(js, "?token") || !strings.Contains(js, "location.hash") {
		t.Error("auth.js must read the token from the fragment only")
	}
	if !strings.Contains(js, "history.replaceState") {
		t.Error("auth.js must drop the token from the address bar")
	}
}
