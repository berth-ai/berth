package panel

import (
	"strings"
	"testing"
)

func TestPagesAndAssets(t *testing.T) {
	e := newEnv(t, "normal")
	c := e.client()
	for path, page := range map[string]string{"/login": "login"} {
		resp, body := e.do(c, "GET", path, "", nil)
		if resp.StatusCode != 200 || !strings.Contains(body, `data-page="`+page+`"`) || !strings.Contains(body, `src="/static/js/auth.js"`) {
			t.Errorf("GET %s = %d", path, resp.StatusCode)
		}
	}
	e.login(c)
	if _, body := e.do(c, "GET", "/", "", nil); !strings.Contains(body, `src="/static/js/app.js"`) {
		t.Error("index does not load the app")
	}
	se := newEnv(t, "setup")
	if _, body := se.do(se.client(), "GET", "/setup", "", nil); !strings.Contains(body, `data-page="setup"`) {
		t.Error("setup page")
	}
	js, _ := static.ReadFile("static/js/auth.js")
	for _, id := range []string{`"login-form"`, `"setup-form"`} {
		if !strings.Contains(string(js), id) {
			t.Errorf("auth.js lacks %s", id)
		}
	}
}

func TestSetupPageNeverQueriesToken(t *testing.T) {
	raw, _ := static.ReadFile("static/js/auth.js")
	js := string(raw)
	if strings.Contains(js, "?token") || !strings.Contains(js, "location.hash") || !strings.Contains(js, "history.replaceState") {
		t.Error("auth.js must read the token from the fragment and drop it from the address bar")
	}
}
