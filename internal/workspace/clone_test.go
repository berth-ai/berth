package workspace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestClone(t *testing.T) {
	r := newRepo(t, map[string]string{"README.md": "hello"})
	first := gitIn(t, r.dir, "rev-parse", "HEAD")
	gitIn(t, r.dir, "tag", "v1")
	gitIn(t, r.dir, "checkout", "-q", "-b", "dev")
	dev := r.commit(t, map[string]string{"dev.txt": "dev"})
	gitIn(t, r.dir, "checkout", "-q", "main")
	for name, tc := range map[string]struct{ ref, commit, branch string }{
		"default branch": {"", first, "main"},
		"branch":         {"dev", dev, "dev"},
		"tag":            {"v1", first, ""},
		"commit":         {dev, dev, ""},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			var log bytes.Buffer
			got, err := testGit().Clone(context.Background(), root, CloneRequest{Repo: r.url, Ref: tc.ref, Path: "acme/api"}, &log)
			if err != nil {
				t.Fatalf("Clone = %v\n%s", err, log.String())
			}
			if want := (Cloned{Path: "acme/api", Commit: tc.commit, Branch: tc.branch}); got != want {
				t.Errorf("Clone = %+v, want %+v", got, want)
			}
			if _, err := os.Stat(filepath.Join(root, "acme", "api", "README.md")); err != nil {
				t.Error(err)
			}
			if names := entries(t, filepath.Join(root, "acme")); !slices.Equal(names, []string{"api"}) {
				t.Errorf("acme holds %v; the temporary folder must be gone", names)
			}
		})
	}
}

func TestCloneFailureCleansUp(t *testing.T) {
	r := newRepo(t, map[string]string{"README.md": "hello"})
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "kept"), 0o755)
	for name, tc := range map[string]struct{ repo, ref, path, code, field string }{
		"unknown branch": {r.url, "nope", "new/deep/api", "ref_not_found", "ref"},
		"unknown commit": {r.url, strings.Repeat("a", 40), "new/deep/api", "ref_not_found", "ref"},
		"no repository":  {"file://" + filepath.Join(root, "none"), "", "kept/api", "fetch_failed", "repo"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := testGit().Clone(context.Background(), root, CloneRequest{Repo: tc.repo, Ref: tc.ref, Path: tc.path}, &bytes.Buffer{})
			var we *Error
			if !errors.As(err, &we) || we.Code != tc.code || we.Field != tc.field {
				t.Fatalf("Clone = %v, want %s on %s", err, tc.code, tc.field)
			}
		})
	}
	if names := entries(t, root); !slices.Equal(names, []string{"kept"}) {
		t.Errorf("root holds %v; folders a failed clone created must be gone", names)
	}
	if names := entries(t, filepath.Join(root, "kept")); len(names) != 0 {
		t.Errorf("kept holds %v", names)
	}
}

func TestCloneRefusals(t *testing.T) {
	r := newRepo(t, map[string]string{"README.md": "hello"})
	root, outside := t.TempDir(), t.TempDir()
	os.Symlink(outside, filepath.Join(root, "link"))
	os.MkdirAll(filepath.Join(root, "taken"), 0o755)
	os.WriteFile(filepath.Join(root, "file"), nil, 0o644)
	for name, tc := range map[string]struct {
		g           Git
		req         CloneRequest
		code, field string
	}{
		"exists":            {testGit(), CloneRequest{Repo: r.url, Path: "taken"}, "path_exists", "path"},
		"escape":            {testGit(), CloneRequest{Repo: r.url, Path: "link/api"}, "invalid_request", "path"},
		"under a file":      {testGit(), CloneRequest{Repo: r.url, Path: "file/api"}, "invalid_request", "path"},
		"temp folder":       {testGit(), CloneRequest{Repo: r.url, Path: ".berth-tmp-1/api"}, "invalid_request", "path"},
		"empty path":        {testGit(), CloneRequest{Repo: r.url}, "invalid_request", "path"},
		"http":              {testGit(), CloneRequest{Repo: "http://example.com/a/b", Path: "x"}, "invalid_request", "repo"},
		"file without seam": {Git{}, CloneRequest{Repo: r.url, Path: "x"}, "invalid_request", "repo"},
		"bad ref":           {testGit(), CloneRequest{Repo: r.url, Ref: "-x", Path: "x"}, "invalid_request", "ref"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tc.g.Clone(context.Background(), root, tc.req, &bytes.Buffer{})
			var we *Error
			if !errors.As(err, &we) || we.Code != tc.code || we.Field != tc.field {
				t.Fatalf("Clone = %v, want %s on %s", err, tc.code, tc.field)
			}
		})
	}
	if names := entries(t, outside); len(names) != 0 {
		t.Errorf("outside holds %v", names)
	}
	if names := entries(t, root); !slices.Equal(names, []string{"file", "link", "taken"}) {
		t.Errorf("root holds %v", names)
	}
}
