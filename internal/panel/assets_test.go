package panel

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
)

func staticFiles(t *testing.T, ext ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	fs.WalkDir(static, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		for _, e := range ext {
			if strings.HasSuffix(p, e) {
				raw, _ := static.ReadFile(p)
				out[p] = string(raw)
			}
		}
		return nil
	})
	return out
}

func TestStaticRoute(t *testing.T) {
	e := newEnv(t, "normal")
	c := e.client()
	for p, ct := range map[string]string{"/static/panel.css": "text/css", "/static/js/app.js": "text/javascript", "/static/js/pages/overview.js": "text/javascript"} {
		resp, body := e.do(c, "GET", p, "", nil)
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), ct) || body == "" {
			t.Errorf("GET %s = %d %q", p, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}
	for _, p := range []string{"/static/index.html", "/static/js/%2e%2e/panel.css", "/static/missing.js", "/static/"} {
		if resp, _ := e.do(c, "GET", p, "", nil); resp.StatusCode != 404 {
			t.Errorf("GET %s = %d", p, resp.StatusCode)
		}
	}
	// The mux cleans dot segments with a redirect before any handler runs.
	if resp, body := e.do(c, "GET", "/static/../panel.go", "", nil); resp.StatusCode == 200 || strings.Contains(body, "package panel") {
		t.Errorf("GET /static/../panel.go = %d", resp.StatusCode)
	}
}

var hexColour = regexp.MustCompile(`#[0-9a-fA-F]{3}(?:[0-9a-fA-F]{3})?(?:[0-9a-fA-F]{2})?\b`)

func TestNoHexOutsideTokens(t *testing.T) {
	for p, src := range staticFiles(t, ".js", ".html") {
		if m := hexColour.FindString(src); m != "" {
			t.Errorf("%s hard-codes %s; use a token", p, m)
		}
	}
	css := staticFiles(t, ".css")["static/panel.css"]
	start, end := strings.Index(css, "/* tokens:start */"), strings.Index(css, "/* tokens:end */")
	if start < 0 || end < start {
		t.Fatal("panel.css lacks the tokens markers")
	}
	if m := hexColour.FindString(css[:start] + css[end:]); m != "" {
		t.Errorf("panel.css uses %s outside the token block", m)
	}
}

func TestDesignTokensDefined(t *testing.T) {
	design, err := os.ReadFile("DESIGN.md")
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("DESIGN.md is not in this checkout")
	}
	if err != nil {
		t.Fatal(err)
	}
	css := staticFiles(t, ".css")["static/panel.css"]
	for _, m := range regexp.MustCompile("`(--[a-z0-9-]+)`").FindAllStringSubmatch(string(design), -1) {
		if !strings.Contains(css, m[1]+":") {
			t.Errorf("DESIGN.md token %s is not defined in panel.css", m[1])
		}
	}
}

func TestNoExternalReferences(t *testing.T) {
	allowed := map[string]bool{"http://www.w3.org/2000/svg": true}
	url := regexp.MustCompile("https?://[^\\s\"'`)<>]+")
	for p, src := range staticFiles(t, ".js", ".css", ".html") {
		for _, u := range url.FindAllString(src, -1) {
			if !allowed[u] {
				t.Errorf("%s references %s", p, u)
			}
		}
	}
}

func TestNoInnerHTML(t *testing.T) {
	for p, src := range staticFiles(t, ".js") {
		for _, bad := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval("} {
			if strings.Contains(src, bad) {
				t.Errorf("%s uses %s", p, bad)
			}
		}
	}
}

func TestImportsResolve(t *testing.T) {
	imp := regexp.MustCompile(`(?m)^import .* from "(\./[^"]+|\.\./[^"]+)";$`)
	for p, src := range staticFiles(t, ".js") {
		for _, m := range imp.FindAllStringSubmatch(src, -1) {
			target := path.Join(path.Dir(p), m[1])
			if _, err := static.ReadFile(target); err != nil {
				t.Errorf("%s imports missing %s", p, m[1])
			}
		}
	}
}

func TestShellsAreStrict(t *testing.T) {
	for p, src := range staticFiles(t, ".html") {
		if strings.Contains(src, "<script>") || strings.Contains(src, " style=") || regexp.MustCompile(` on[a-z]+=`).MatchString(src) {
			t.Errorf("%s has inline script or style", p)
		}
		if p != "static/unavailable.html" && !regexp.MustCompile(`<script type="module" src="/static/js/[a-z]+\.js"></script>`).MatchString(src) {
			t.Errorf("%s does not load a module", p)
		}
	}
}
