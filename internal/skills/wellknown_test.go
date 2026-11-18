package skills

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/berth-ai/berth/internal/config"
)

// site is a TLS test server publishing files by path; set swaps one.
type site struct {
	srv   *httptest.Server
	mu    sync.Mutex
	files map[string][]byte
	hits  map[string]int
}

func newSite(t *testing.T) *site {
	t.Helper()
	s := &site{files: map[string][]byte{}, hits: map[string]int{}}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.hits[r.URL.Path]++
		body, ok := s.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *site) set(p string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[p] = body
}

func (s *site) web() WellKnown { return WellKnown{Client: s.srv.Client()} }

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// index publishes a 0.2.0 index at dir listing entries.
func (s *site) index(t *testing.T, dir string, entries ...map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"$schema": wellKnownSchema, "skills": entries})
	s.set(dir+"/index.json", raw)
}

func indexEntry(name, typ, url string, body []byte) map[string]any {
	return map[string]any{"name": name, "type": typ, "description": name + " tools", "url": url, "digest": digestOf(body)}
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	zw.Close()
	return buf.Bytes()
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func TestScanWellKnown(t *testing.T) {
	s := newSite(t)
	pdf := []byte(pdfSkill)
	s.index(t, "/.well-known/agent-skills",
		indexEntry("pdf", "skill-md", "pdf/SKILL.md", pdf),
		indexEntry("docs", "archive", "https://cdn.invalid/docs.tar.gz", []byte("x")),
		map[string]any{"name": "Bad Name", "type": "skill-md", "description": "d", "url": "x", "digest": digestOf(pdf)},
		map[string]any{"name": "nodigest", "type": "skill-md", "description": "d", "url": "x"},
		indexEntry("plain", "skill-md", "http://example.org/SKILL.md", pdf),
	)
	ctx := context.Background()
	res, err := s.web().ScanWellKnown(ctx, s.srv.URL+"/docs", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Index != s.srv.URL+"/.well-known/agent-skills/index.json" {
		t.Errorf("index = %s", res.Index)
	}
	got := map[string]ScanRow{}
	for _, r := range res.Skills {
		got[r.Path] = r
	}
	if r := got["pdf"]; r.Name != "pdf" || r.URL != s.srv.URL+"/.well-known/agent-skills/pdf/SKILL.md" || r.Digest != digestOf(pdf) || r.Type != "skill-md" {
		t.Errorf("pdf = %+v", r)
	}
	if r := got["docs"]; r.Name != "docs" || r.Type != "archive" {
		t.Errorf("docs = %+v", r)
	}
	for p, want := range map[string]string{"entry-3": "not a skill name", "nodigest": "has no digest", "plain": "must start with https://"} {
		if r := got[p]; !strings.Contains(r.Error, want) || r.Name != "" {
			t.Errorf("%s = %+v, want error %q", p, r, want)
		}
	}
	if !config.ValidDigest(res.Commit) || res.Unchanged {
		t.Errorf("commit = %s, unchanged %v", res.Commit, res.Unchanged)
	}
	again, _ := s.web().ScanWellKnown(ctx, s.srv.URL, res.Commit)
	if again.Commit != res.Commit || !again.Unchanged {
		t.Errorf("a second scan = %s %v", again.Commit, again.Unchanged)
	}
	s.index(t, "/.well-known/agent-skills", indexEntry("pdf", "skill-md", "pdf/SKILL.md", []byte(skill("pdf"))))
	if moved, _ := s.web().ScanWellKnown(ctx, s.srv.URL, res.Commit); moved.Commit == res.Commit || moved.Unchanged {
		t.Error("republishing a skill did not change the index digest")
	}
}

func TestScanWellKnownFailures(t *testing.T) {
	s := newSite(t)
	ctx := context.Background()
	var se *Error
	if _, err := s.web().ScanWellKnown(ctx, s.srv.URL, ""); !errors.As(err, &se) || se.Code != "no_index" {
		t.Errorf("no index = %v", err)
	}
	s.set("/.well-known/skills/index.json", []byte(`{"skills":[{"name":"a","description":"d","files":["SKILL.md"]}]}`))
	if _, err := s.web().ScanWellKnown(ctx, s.srv.URL, ""); !errors.As(err, &se) || se.Code != "unsupported_index" {
		t.Errorf("legacy index = %v", err)
	}
	if _, err := s.web().ScanWellKnown(ctx, "http://example.org", ""); !errors.As(err, &se) || se.Field != "repo" {
		t.Errorf("http site = %v", err)
	}
	// The older folder is read when it holds a 0.2.0 index.
	pdf := []byte(pdfSkill)
	s.index(t, "/.well-known/skills", indexEntry("pdf", "skill-md", "pdf/SKILL.md", pdf))
	if res, err := s.web().ScanWellKnown(ctx, s.srv.URL, ""); err != nil || len(res.Skills) != 1 {
		t.Errorf("0.2.0 in the older folder = %v %v", res, err)
	}
}

// exportTo exports skill name of found row f into a fresh folder.
func exportTo(t *testing.T, w WellKnown, home, name string, f config.Found) (string, error) {
	t.Helper()
	out := t.TempDir()
	err := w.Export(context.Background(), home, name, f, func(r io.Reader) error {
		return extract(r, out, func(string, ...any) {})
	})
	return out, err
}

func TestWellKnownExport(t *testing.T) {
	s := newSite(t)
	home := t.TempDir()
	pdf := []byte(pdfSkill)
	wrapped := tarGz(t, map[string]string{"docx/SKILL.md": skill("docx"), "docx/ref/a.md": "a"})
	zipped := zipOf(t, map[string]string{"SKILL.md": skill("xlsx"), "x.md": "x", "__MACOSX/._x.md": "junk"})
	s.set("/pdf.md", pdf)
	s.set("/docx.tgz", wrapped)
	s.set("/xlsx.zip", zipped)
	for name, tc := range map[string]struct {
		f    config.Found
		want []string
	}{
		"pdf":  {config.Found{URL: s.srv.URL + "/pdf.md", Digest: digestOf(pdf), Type: "skill-md"}, []string{"SKILL.md"}},
		"docx": {config.Found{URL: s.srv.URL + "/docx.tgz", Digest: digestOf(wrapped), Type: "archive"}, []string{"SKILL.md", "ref/a.md"}},
		"xlsx": {config.Found{URL: s.srv.URL + "/xlsx.zip", Digest: digestOf(zipped), Type: "archive"}, []string{"SKILL.md", "x.md"}},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := exportTo(t, s.web(), home, name, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range tc.want {
				if _, err := os.Stat(filepath.Join(out, p)); err != nil {
					t.Errorf("missing %s: %v", p, err)
				}
			}
			if _, err := os.Stat(filepath.Join(out, "__MACOSX")); err == nil {
				t.Error("kept __MACOSX")
			}
		})
	}
	// The site moves on: the cached download still installs, a new one is checked.
	s.set("/pdf.md", []byte(skill("pdf")))
	if _, err := exportTo(t, s.web(), home, "pdf", config.Found{URL: s.srv.URL + "/pdf.md", Digest: digestOf(pdf), Type: "skill-md"}); err != nil {
		t.Errorf("cached download = %v", err)
	}
	os.RemoveAll(ArtifactDir(home))
	if _, err := exportTo(t, s.web(), home, "pdf", config.Found{URL: s.srv.URL + "/pdf.md", Digest: digestOf(pdf), Type: "skill-md"}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("changed download = %v", err)
	}
	// A SKILL.md named differently from its index entry is refused.
	if _, err := exportTo(t, s.web(), home, "word", config.Found{URL: s.srv.URL + "/docx.tgz", Digest: digestOf(wrapped), Type: "archive"}); err == nil || !strings.Contains(err.Error(), "named docx") {
		t.Errorf("renamed skill = %v", err)
	}
	// An archive entry that climbs out of the skill is refused by extract.
	evil := tarGz(t, map[string]string{"SKILL.md": skill("evil"), "../escape": "x"})
	s.set("/evil.tgz", evil)
	if _, err := exportTo(t, s.web(), home, "evil", config.Found{URL: s.srv.URL + "/evil.tgz", Digest: digestOf(evil), Type: "archive"}); err == nil || !strings.Contains(err.Error(), "leaves the skill") {
		t.Errorf("escaping archive = %v", err)
	}
}
