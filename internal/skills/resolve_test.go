package skills

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	f := newFixture(t, map[string]string{"skills/pdf/SKILL.md": pdfSkill, "SKILL.md": "---\nname: root\ndescription: r\n---\n"})
	first := f.git(t, "rev-parse", "HEAD")
	f.git(t, "tag", "v1")
	second := f.commit(t, map[string]string{"skills/pdf/extra.md": "x"})
	home := t.TempDir()
	ctx := context.Background()
	for name, tc := range map[string]struct {
		path, ref, want, skill string
	}{
		"branch": {"skills/pdf", "main", second, "pdf"},
		"tag":    {"skills/pdf", "v1", first, "pdf"},
		"sha":    {"skills/pdf", first, first, "pdf"},
		"root":   {"", "main", second, "root"},
	} {
		t.Run(name, func(t *testing.T) {
			res, err := testGit().Resolve(ctx, home, Request{Repo: f.url, Path: tc.path, Ref: tc.ref})
			if err != nil {
				t.Fatal(err)
			}
			if res.Commit != tc.want || res.Name != tc.skill {
				t.Errorf("got %+v, want %s at %s", res, tc.skill, tc.want)
			}
		})
	}
}

func TestResolveErrors(t *testing.T) {
	f := newFixture(t, map[string]string{"skills/pdf/SKILL.md": pdfSkill, "skills/bad/SKILL.md": "# no frontmatter\n"})
	home := t.TempDir()
	for name, tc := range map[string]struct {
		req         Request
		code, field string
	}{
		"missing ref":  {Request{Repo: f.url, Path: "skills/pdf", Ref: "nope"}, "ref_not_found", "ref"},
		"missing path": {Request{Repo: f.url, Path: "skills/none", Ref: "main"}, "no_skill", "path"},
		"bad skill":    {Request{Repo: f.url, Path: "skills/bad", Ref: "main"}, "bad_skill", "path"},
		"bad ref":      {Request{Repo: f.url, Path: "skills/pdf", Ref: "-x"}, "invalid_request", "ref"},
		"bad path":     {Request{Repo: f.url, Path: "../x", Ref: "main"}, "invalid_request", "path"},
		"no repo":      {Request{Repo: "file:///nonexistent/repo", Path: "", Ref: "main"}, "fetch_failed", "repo"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := testGit().Resolve(context.Background(), home, tc.req)
			var e *Error
			if !errors.As(err, &e) || e.Code != tc.code || e.Field != tc.field {
				t.Errorf("err = %#v, want %s on %s", err, tc.code, tc.field)
			}
		})
	}
}

func TestResolveRefusesFileWithoutSeam(t *testing.T) {
	f := newFixture(t, map[string]string{"SKILL.md": pdfSkill})
	_, err := Git{}.Resolve(context.Background(), t.TempDir(), Request{Repo: f.url, Ref: "main"})
	var e *Error
	if !errors.As(err, &e) || e.Field != "repo" {
		t.Errorf("err = %v", err)
	}
}

func TestFetchErrorClassification(t *testing.T) {
	r := Request{Repo: "https://github.com/o/r", Ref: "main"}
	for stderr, code := range map[string]string{
		"fatal: couldn't find remote ref main":                                                  "ref_not_found",
		"fatal: remote error: upload-pack: not our ref 0123":                                    "ref_not_found",
		"fatal: could not read Username for 'https://github.com': terminal prompts disabled":    "auth_required",
		"remote: Repository not found.\nfatal: repository 'https://github.com/o/r/' not found":  "auth_required",
		"fatal: unable to access 'https://github.com/o/r/': Could not resolve host: github.com": "fetch_failed",
	} {
		e := fetchError(r, &gitError{stderr: stderr})
		if e.Code != code {
			t.Errorf("%q → %s, want %s", stderr, e.Code, code)
		}
		if code == "auth_required" && !strings.Contains(e.Message, "Accounts page") {
			t.Errorf("auth message %q does not point to the Accounts page", e.Message)
		}
	}
}
