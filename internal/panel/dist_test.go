package panel

import (
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
)

// builtDist is the embedded build, or skips (fails in CI) when web/dist
// holds only .gitkeep.
func builtDist(t *testing.T) fs.FS {
	t.Helper()
	d := builtAssets()
	if _, err := fs.Stat(d, "index.html"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("web/dist is not built; CI must build it before go test")
		}
		t.Skip("web/dist is not built; run pnpm build in web/")
	}
	return d
}

var pages = []string{"index.html", "login.html", "setup.html", "unavailable.html"}

func TestBuiltPagesAreStrict(t *testing.T) {
	d := builtDist(t)
	tag := regexp.MustCompile(`<script[^>]*>`)
	module := regexp.MustCompile(`<script type="module" crossorigin src="/static/assets/[A-Za-z0-9_.-]+\.js"></script>`)
	for _, name := range pages {
		raw, err := fs.ReadFile(d, name)
		if err != nil {
			t.Errorf("%s is missing from the build", name)
			continue
		}
		src := string(raw)
		for _, s := range tag.FindAllString(src, -1) {
			if !strings.Contains(s, "src=") {
				t.Errorf("%s has an inline script: %s", name, s)
			}
		}
		if strings.Contains(src, "<style") || strings.Contains(src, " style=") || regexp.MustCompile(` on[a-z]+=`).MatchString(src) {
			t.Errorf("%s has inline style or handlers", name)
		}
		if name == "unavailable.html" {
			if strings.Contains(src, "<script") || !strings.Contains(src, "%[1]s") || !strings.Contains(src, `href="/#settings"`) ||
				!strings.Contains(src, `href="/static/assets/`) {
				t.Errorf("unavailable.html must be script-free, keep %%[1]s and link its stylesheet: %s", src)
			}
			continue
		}
		if !module.MatchString(src) {
			t.Errorf("%s does not load a module from /static/assets/", name)
		}
	}
}

func TestBuiltAssetsHaveKnownTypes(t *testing.T) {
	d := builtDist(t)
	_ = fs.WalkDir(d, "assets", func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			if _, ok := assetTypes[path.Ext(p)]; !ok {
				t.Errorf("%s has no entry in assetTypes; add its MIME type", p)
			}
		}
		return err
	})
}

func TestBuiltPagesAndStylesStayLocal(t *testing.T) {
	d := builtDist(t)
	url := regexp.MustCompile("https?://[^\\s\"'`)<>]+")
	comment := regexp.MustCompile(`(?s)/\*.*?\*/|<!--.*?-->`)
	_ = fs.WalkDir(d, ".", func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || (path.Ext(p) != ".html" && path.Ext(p) != ".css") {
			return err
		}
		raw, _ := fs.ReadFile(d, p)
		// Comments load nothing; Tailwind's licence banner names its site.
		src := comment.ReplaceAllString(string(raw), "")
		for _, u := range url.FindAllString(src, -1) {
			if u != "http://www.w3.org/2000/svg" {
				t.Errorf("%s references %s", p, u)
			}
		}
		return nil
	})
}

func TestBuiltPanelServesItsAssets(t *testing.T) {
	builtDist(t)
	e := newEnv(t, "normal")
	c := e.client()
	resp, body := e.do(c, "GET", "/login", "", nil)
	asset := regexp.MustCompile(`/static/assets/[A-Za-z0-9_.-]+\.js`).FindString(body)
	if resp.StatusCode != 200 || asset == "" {
		t.Fatalf("GET /login = %d, asset %q", resp.StatusCode, asset)
	}
	resp, _ = e.do(c, "GET", asset, "", nil)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/javascript") ||
		!strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("GET %s = %d %v", asset, resp.StatusCode, resp.Header)
	}
}
