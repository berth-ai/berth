package panel

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
)

// fixtureAssets is a minimal build: the four pages and two hashed assets.
func fixtureAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html":             {Data: []byte(`<!doctype html><title>app</title><script type="module" src="/static/assets/app-1a2b.js"></script>`)},
		"login.html":             {Data: []byte(`<!doctype html><title>login</title>`)},
		"setup.html":             {Data: []byte(`<!doctype html><title>setup</title>`)},
		"unavailable.html":       {Data: []byte(`<h1>The %[1]s is not running</h1><p>100% sure</p>`)},
		"assets/app-1a2b.js":     {Data: []byte(`console.log(1)`)},
		"assets/app-1a2b.css":    {Data: []byte(`body{}`)},
		"assets/app-1a2b.js.map": {Data: []byte(`{}`)},
	}
}

func TestServesInjectedAssets(t *testing.T) {
	e := newEnv(t, "normal")
	e.s.UseAssets(fixtureAssets())
	c := e.client()
	e.login(c)
	resp, body := e.do(c, "GET", "/", "", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "<title>app</title>") {
		t.Fatalf("GET / = %d %q", resp.StatusCode, body)
	}
	if resp.Header.Get("Content-Security-Policy") != panelCSP || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") ||
		resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("page headers = %v", resp.Header)
	}
	for p, ct := range map[string]string{"/static/assets/app-1a2b.js": "text/javascript", "/static/assets/app-1a2b.css": "text/css"} {
		resp, body := e.do(c, "GET", p, "", nil)
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), ct) || body == "" {
			t.Errorf("GET %s = %d %q", p, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		if resp.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Errorf("GET %s cache = %q", p, resp.Header.Get("Cache-Control"))
		}
	}
	for _, p := range []string{"/static/assets/app-1a2b.js.map", "/static/index.html", "/static/assets/missing.js", "/static/"} {
		if resp, _ := e.do(c, "GET", p, "", nil); resp.StatusCode != 404 {
			t.Errorf("GET %s = %d", p, resp.StatusCode)
		}
	}
}

func TestUnbuiltPanelSaysSo(t *testing.T) {
	e := newEnv(t, "normal")
	e.s.UseAssets(fstest.MapFS{})
	resp, body := e.do(e.client(), "GET", "/login", "", nil)
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(body, "not built") {
		t.Errorf("GET /login unbuilt = %d %q", resp.StatusCode, body)
	}
}

func TestUnavailablePageFromAssets(t *testing.T) {
	e, term, _ := proxyEnv(t)
	e.s.UseAssets(fixtureAssets())
	term.srv.Close()
	c := e.client()
	e.login(c)
	resp, body := e.do(c, "GET", "/terminal/", "", nil)
	if resp.StatusCode != 502 || !strings.Contains(body, "The terminal is not running") || !strings.Contains(body, "100% sure") {
		t.Errorf("down = %d %q", resp.StatusCode, body)
	}
}
