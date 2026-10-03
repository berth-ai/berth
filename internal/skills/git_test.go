package skills

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
)

func TestFetchHasCommitShowArchive(t *testing.T) {
	f := newFixture(t, map[string]string{"skills/pdf/SKILL.md": pdfSkill})
	home := t.TempDir()
	dir := CacheDir(home, f.url)
	g, ctx := testGit(), context.Background()
	commit, err := g.Fetch(ctx, dir, f.url, "main")
	if err != nil {
		t.Fatal(err)
	}
	if !g.HasCommit(ctx, dir, commit) || g.HasCommit(ctx, dir, "0123456789abcdef0123456789abcdef01234567") {
		t.Error("HasCommit is wrong")
	}
	body, err := g.Show(ctx, dir, commit, "skills/pdf/SKILL.md")
	if err != nil || string(body) != pdfSkill {
		t.Errorf("Show = %q, %v", body, err)
	}
	var tarball bytes.Buffer
	if err := g.Archive(ctx, dir, commit, "skills/pdf", func(r io.Reader) error { _, err := io.Copy(&tarball, r); return err }); err != nil || tarball.Len() == 0 {
		t.Errorf("Archive = %d bytes, %v", tarball.Len(), err)
	}
}

func TestConcurrentFetchesShareTheCache(t *testing.T) {
	f := newFixture(t, map[string]string{"SKILL.md": pdfSkill})
	home := t.TempDir()
	dir := CacheDir(home, f.url)
	want := f.git(t, "rev-parse", "HEAD")
	var wg sync.WaitGroup
	errs := make([]error, 8)
	got := make([]string, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], errs[i] = testGit().Fetch(context.Background(), dir, f.url, "main")
		}()
	}
	wg.Wait()
	for i := range 8 {
		if errs[i] != nil || got[i] != want {
			t.Errorf("fetch %d = %q, %v", i, got[i], errs[i])
		}
	}
}
