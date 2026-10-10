package skills

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// rowsOf renders rows as "path=name" or "path!error" for compact comparison.
func rowsOf(rows []ScanRow) string {
	var out []string
	for _, r := range rows {
		if r.Error != "" {
			out = append(out, r.Path+"!"+r.Error)
		} else {
			out = append(out, r.Path+"="+r.Name)
		}
	}
	return strings.Join(out, " ")
}

func skill(name string) string {
	return "---\nname: " + name + "\ndescription: " + name + " tools\n---\n"
}

func TestScan(t *testing.T) {
	f := newFixture(t, map[string]string{
		"SKILL.md":                    skill("root"),
		"skills/pdf/SKILL.md":         pdfSkill,
		"skills/pdf/README.md":        "noise",
		"skills/nested/deep/SKILL.md": skill("deep"),
		"skills/bad/SKILL.md":         "# no frontmatter\n",
		"skills/zdup/SKILL.md":        skill("pdf"),
		"skills-extra/x/SKILL.md":     skill("extra"),
	})
	first := f.git(t, "rev-parse", "HEAD")
	f.commit(t, map[string]string{"skills/new/SKILL.md": skill("new")})
	home := t.TempDir()
	dup := "skills/zdup!Another SKILL.md in this repository is also named pdf"
	bad := "skills/bad!SKILL.md does not start with a --- frontmatter block"
	for name, tc := range map[string]struct {
		ref   string
		paths []string
		want  string
	}{
		"whole tree":    {first, nil, "=root skills-extra/x=extra " + bad + " skills/nested/deep=deep skills/pdf=pdf " + dup},
		"empty path":    {first, []string{""}, "=root skills-extra/x=extra " + bad + " skills/nested/deep=deep skills/pdf=pdf " + dup},
		"folder":        {first, []string{"skills"}, bad + " skills/nested/deep=deep skills/pdf=pdf " + dup},
		"one skill":     {first, []string{"skills/pdf"}, "skills/pdf=pdf"},
		"the file":      {first, []string{"skills/pdf/SKILL.md"}, "skills/pdf=pdf"},
		"several":       {first, []string{"skills/pdf", "skills/nested"}, "skills/nested/deep=deep skills/pdf=pdf"},
		"overlapping":   {first, []string{"skills/nested", "skills/nested/deep"}, "skills/nested/deep=deep"},
		"branch tip":    {"main", []string{"skills/new"}, "skills/new=new"},
		"sha pins tree": {first, []string{"skills/pdf", "skills/new"}, "skills/pdf=pdf"},
	} {
		t.Run(name, func(t *testing.T) {
			res, err := testGit().Scan(context.Background(), home, ScanRequest{Repo: f.url, Ref: tc.ref, Paths: tc.paths})
			if err != nil {
				t.Fatal(err)
			}
			if got := rowsOf(res.Skills); got != tc.want {
				t.Errorf("rows = %s\nwant   %s", got, tc.want)
			}
			if tc.ref == first && res.Commit != first {
				t.Errorf("commit = %s, want %s", res.Commit, first)
			}
		})
	}
	res, _ := testGit().Scan(context.Background(), home, ScanRequest{Repo: f.url, Ref: first, Paths: []string{"skills/pdf"}})
	if res.Skills[0].Description != "PDF tools" {
		t.Errorf("description = %q", res.Skills[0].Description)
	}
}

func TestScanCap(t *testing.T) {
	files := map[string]string{"many/b/s200/SKILL.md": skill("s200")}
	for i := range MaxScan {
		files[fmt.Sprintf("many/a/s%03d/SKILL.md", i)] = skill(fmt.Sprintf("s%03d", i))
	}
	f := newFixture(t, files)
	home := t.TempDir()
	res, err := testGit().Scan(context.Background(), home, ScanRequest{Repo: f.url, Ref: "main", Paths: []string{"many/a"}})
	if err != nil || len(res.Skills) != MaxScan {
		t.Fatalf("200 skills: %d rows, %v", len(res.Skills), err)
	}
	_, err = testGit().Scan(context.Background(), home, ScanRequest{Repo: f.url, Ref: "main", Paths: []string{"many"}})
	var e *Error
	if !errors.As(err, &e) || e.Code != "too_many" || e.Field != "path" || !strings.Contains(e.Message, "more than 200 skills") {
		t.Errorf("201 skills: %#v", err)
	}
}

func TestScanErrors(t *testing.T) {
	f := newFixture(t, map[string]string{"skills/pdf/SKILL.md": pdfSkill, "docs/README.md": "x"})
	home := t.TempDir()
	tooMany := make([]string, MaxScan+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("s%d", i)
	}
	for name, tc := range map[string]struct {
		req               ScanRequest
		code, field, text string
	}{
		"no skill":    {ScanRequest{Repo: f.url, Ref: "main", Paths: []string{"docs"}}, "no_skill", "path", "No SKILL.md at or below docs in that commit"},
		"no folder":   {ScanRequest{Repo: f.url, Ref: "main", Paths: []string{"none"}}, "no_skill", "path", "at or below none"},
		"missing ref": {ScanRequest{Repo: f.url, Ref: "nope"}, "ref_not_found", "ref", "Ref nope not found"},
		"bad ref":     {ScanRequest{Repo: f.url, Ref: "-x"}, "invalid_request", "ref", "branch, tag or commit"},
		"bad path":    {ScanRequest{Repo: f.url, Ref: "main", Paths: []string{"../x"}}, "invalid_request", "path", "folder inside"},
		"many paths":  {ScanRequest{Repo: f.url, Ref: "main", Paths: tooMany}, "invalid_request", "path", "at most 200"},
		"no repo":     {ScanRequest{Repo: "file:///nonexistent/repo", Ref: "main"}, "fetch_failed", "repo", "Could not fetch"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := testGit().Scan(context.Background(), home, tc.req)
			var e *Error
			if !errors.As(err, &e) || e.Code != tc.code || e.Field != tc.field || !strings.Contains(e.Message, tc.text) {
				t.Errorf("err = %#v, want %s on %s containing %q", err, tc.code, tc.field, tc.text)
			}
		})
	}
}

func TestScanRefusesFileWithoutSeam(t *testing.T) {
	f := newFixture(t, map[string]string{"SKILL.md": pdfSkill})
	_, err := Git{}.Scan(context.Background(), t.TempDir(), ScanRequest{Repo: f.url, Ref: "main"})
	var e *Error
	if !errors.As(err, &e) || e.Field != "repo" {
		t.Errorf("err = %v", err)
	}
}

func TestFetchErrorClassification(t *testing.T) {
	for stderr, code := range map[string]string{
		"fatal: couldn't find remote ref main":                                                  "ref_not_found",
		"fatal: remote error: upload-pack: not our ref 0123":                                    "ref_not_found",
		"fatal: could not read Username for 'https://github.com': terminal prompts disabled":    "auth_required",
		"remote: Repository not found.\nfatal: repository 'https://github.com/o/r/' not found":  "auth_required",
		"fatal: unable to access 'https://github.com/o/r/': Could not resolve host: github.com": "fetch_failed",
	} {
		e := fetchError("https://github.com/o/r", "main", &gitError{stderr: stderr})
		if e.Code != code {
			t.Errorf("%q → %s, want %s", stderr, e.Code, code)
		}
		if code == "auth_required" && !strings.Contains(e.Message, "Accounts page") {
			t.Errorf("auth message %q does not point to the Accounts page", e.Message)
		}
	}
}

func TestClassifyFetch(t *testing.T) {
	for said, code := range map[string]string{
		"fatal: couldn't find remote ref nope":                       "ref_not_found",
		"warning: Could not find remote branch nope to clone.\nfatal: Remote branch nope not found in upstream origin": "ref_not_found",
		"remote: Repository not found.\nfatal: repository 'https://github.com/a/b/' not found": "auth_required",
		"fatal: could not read Username for 'https://github.com': terminal prompts disabled":   "auth_required",
		"fatal: unable to access 'https://example.com/': Could not resolve host: example.com":  "fetch_failed",
	} {
		e := ClassifyFetch("https://github.com/a/b", "nope", said)
		if e.Code != code {
			t.Errorf("%q: code = %s, want %s", said, e.Code, code)
		}
	}
	if e := ClassifyFetch("https://example.com/a", "main", "fatal: one\nfatal: the last line"); e.Message != "Could not fetch the repository: fatal: the last line" {
		t.Errorf("message = %q", e.Message)
	}
}
