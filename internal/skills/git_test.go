package skills

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// cacheHolds reports whether the cache holds the file at commit:file, or
// the commit itself when file is empty, without fetching it.
func cacheHolds(t *testing.T, dir, commit, file string) bool {
	t.Helper()
	g, ctx := testGit(), context.Background()
	if file == "" {
		return g.has(ctx, dir, commit)
	}
	oid, err := g.output(ctx, dir, "rev-parse", "--verify", "-q", commit+":"+file)
	if err != nil {
		t.Fatalf("%s:%s: %v", commit, file, err)
	}
	return g.run(ctx, dir, io.Discard, "cat-file", "-e", oid) == nil
}

func TestExportFetchesOnlyItsFolder(t *testing.T) {
	f := newFixture(t, map[string]string{"skills/pdf/SKILL.md": pdfSkill, "skills/pdf/ref.md": "ref", "app/big.bin": strings.Repeat("x", 1<<16)})
	commit := f.git(t, "rev-parse", "HEAD")
	dir := CacheDir(t.TempDir(), f.url)
	var tarball bytes.Buffer
	if err := testGit().Export(context.Background(), dir, f.url, commit, "skills/pdf", func(r io.Reader) error {
		_, err := io.Copy(&tarball, r)
		return err
	}); err != nil || tarball.Len() == 0 {
		t.Fatalf("Export = %d bytes, %v", tarball.Len(), err)
	}
	if !cacheHolds(t, dir, commit, "skills/pdf/ref.md") || cacheHolds(t, dir, commit, "app/big.bin") {
		t.Error("the cache should hold the skill folder's files and nothing else")
	}
}

func TestHasNeverFetches(t *testing.T) {
	f := newFixture(t, map[string]string{"SKILL.md": pdfSkill})
	first := f.git(t, "rev-parse", "HEAD")
	f.commit(t, map[string]string{"SKILL.md": skill("pdf2")})
	home := t.TempDir()
	dir := CacheDir(home, f.url)
	g, ctx := testGit(), context.Background()
	if _, err := g.Scan(ctx, home, ScanRequest{Repo: f.url, Ref: "main"}); err != nil {
		t.Fatal(err)
	}
	if g.has(ctx, dir, first) || g.has(ctx, dir, first) {
		t.Error("has fetched a commit the cache lacked")
	}
	if !g.has(ctx, dir, f.git(t, "rev-parse", "HEAD")) {
		t.Error("has misses the scanned commit")
	}
}

func TestConcurrentScansShareTheCache(t *testing.T) {
	f := newFixture(t, map[string]string{"SKILL.md": pdfSkill})
	home := t.TempDir()
	want := f.git(t, "rev-parse", "HEAD")
	var wg sync.WaitGroup
	errs := make([]error, 8)
	got := make([]string, 8)
	for i := range 8 {
		wg.Go(func() {
			s, err := testGit().Scan(context.Background(), home, ScanRequest{Repo: f.url, Ref: "main"})
			got[i], errs[i] = s.Commit, err
		})
	}
	wg.Wait()
	for i := range 8 {
		if errs[i] != nil || got[i] != want {
			t.Errorf("scan %d = %q, %v", i, got[i], errs[i])
		}
	}
}

// A tar reader stops at the end-of-archive marker, before git's record
// padding; Export must still succeed.
func TestExportIntoExtract(t *testing.T) {
	f := newFixture(t, map[string]string{"skills/pdf/SKILL.md": pdfSkill})
	dir := CacheDir(t.TempDir(), f.url)
	out := t.TempDir()
	if err := testGit().Export(context.Background(), dir, f.url, f.git(t, "rev-parse", "HEAD"), "skills/pdf", func(r io.Reader) error {
		return extract(r, out, func(string, ...any) {})
	}); err != nil {
		t.Fatalf("Export = %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(out, "SKILL.md")); err != nil || string(b) != pdfSkill {
		t.Errorf("SKILL.md = %q, %v", b, err)
	}
}

func TestKeepDropsUnpinnedCommits(t *testing.T) {
	f := newFixture(t, map[string]string{"skills/pdf/SKILL.md": pdfSkill})
	var commits []string
	commits = append(commits, f.git(t, "rev-parse", "HEAD"))
	for _, d := range []string{"two", "three", "four"} {
		commits = append(commits, f.commit(t, map[string]string{"skills/pdf/SKILL.md": skill(d), "other/" + d: d}))
	}
	home := t.TempDir()
	dir := CacheDir(home, f.url)
	g, ctx := testGit(), context.Background()
	// Each scan drops what the one before fetched, but keeps since.
	for _, c := range commits {
		if _, err := g.Scan(ctx, home, ScanRequest{Repo: f.url, Ref: c, Since: commits[0]}); err != nil {
			t.Fatal(err)
		}
	}
	held := func() []string {
		var out []string
		for i, c := range commits {
			if cacheHolds(t, dir, c, "") {
				out = append(out, []string{"one", "two", "three", "four"}[i])
			}
		}
		return out
	}
	if got := held(); !slices.Equal(got, []string{"one", "four"}) {
		t.Errorf("after the scans the cache holds %v, want since and the newest", got)
	}
	// A sync pins two; the newest scan stays too.
	if err := g.Export(ctx, dir, f.url, commits[1], "skills/pdf", func(r io.Reader) error { _, err := io.Copy(io.Discard, r); return err }); err != nil {
		t.Fatal(err)
	}
	if err := g.Keep(ctx, dir, []string{commits[1]}); err != nil {
		t.Fatal(err)
	}
	if got := held(); !slices.Equal(got, []string{"two", "four"}) {
		t.Errorf("after Keep the cache holds %v", got)
	}
	shallow, _ := os.ReadFile(filepath.Join(dir, "shallow"))
	if lines := strings.Fields(string(shallow)); len(lines) != 2 {
		t.Errorf("shallow = %q", lines)
	}
	if !cacheHolds(t, dir, commits[1], "skills/pdf/SKILL.md") {
		t.Error("Keep dropped a pinned commit's files")
	}
	if out, err := g.output(ctx, dir, "fsck", "--connectivity-only", "--no-dangling"); err != nil {
		t.Errorf("fsck: %v %s", err, out)
	}
	// The pinned commit still exports with no network.
	if err := g.Export(ctx, dir, "file:///nonexistent", commits[1], "skills/pdf", func(r io.Reader) error { _, err := io.Copy(io.Discard, r); return err }); err != nil {
		t.Errorf("Export after Keep = %v", err)
	}
}

func TestKeepStartsOverAnOldCache(t *testing.T) {
	f := newFixture(t, map[string]string{"SKILL.md": pdfSkill})
	dir := CacheDir(t.TempDir(), f.url)
	g, ctx := testGit(), context.Background()
	if err := g.run(ctx, "", io.Discard, "init", "--bare", "-q", dir); err != nil {
		t.Fatal(err)
	}
	if err := g.Keep(ctx, dir, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("an old cache survived Keep: %v", err)
	}
}
