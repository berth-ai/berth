package skills

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/berth-ai/berth/internal/config"
)

// MaxScan is the most skills one scan lists, and the most paths it takes.
const MaxScan = 200

// ScanRequest names folders of a repository at a ref.
type ScanRequest struct {
	Repo  string
	Ref   string
	Paths []string // folders, or SKILL.md files, to look in; none, or "", is the whole tree
	Since string   // a commit to compare Paths with; empty compares nothing
}

// ScanRow is one SKILL.md a scan found; a row with Error has no name. A
// well-known scan's rows also name the artifact to download.
type ScanRow struct {
	Path        string `json:"path"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
	URL         string `json:"url,omitempty"`
	Digest      string `json:"digest,omitempty"`
	Type        string `json:"type,omitempty"`
}

// Scan is a ref pinned to a commit and the skills found there, by path.
// Unchanged is true when Since holds the same skill folders below Paths,
// each with the same files; other files may differ.
// Index is the URL a well-known scan read its index from.
type Scan struct {
	Commit    string    `json:"commit"`
	Index     string    `json:"index,omitempty"`
	Unchanged bool      `json:"unchanged,omitempty"`
	Skills    []ScanRow `json:"skills"`
}

// Error is a scan failure the panel shows next to one field.
type Error struct {
	Code    string // invalid_request, ref_not_found, no_skill, too_many, auth_required, fetch_failed
	Field   string // repo, path or ref
	Message string
}

func (e *Error) Error() string { return e.Message }

// Scan fetches r.Ref, pins it to a commit and reads every SKILL.md at or
// below r.Paths. A bad SKILL.md becomes a row with an error; every failure
// of the scan itself is an *Error. It fetches the trees of the commit and
// the SKILL.md files, never the other files.
func (g Git) Scan(ctx context.Context, home string, r ScanRequest) (Scan, error) {
	if err := g.check(r); err != nil {
		return Scan{}, err
	}
	dir := CacheDir(home, r.Repo)
	unlock, err := lock(dir, true)
	if err != nil {
		return Scan{}, &Error{Code: "fetch_failed", Field: "repo", Message: "Could not lock the cache: " + err.Error()}
	}
	defer unlock()
	if err := g.open(ctx, dir, r.Repo); err != nil {
		return Scan{}, &Error{Code: "fetch_failed", Field: "repo", Message: "Could not prepare the cache: " + err.Error()}
	}
	commit, err := g.fetch(ctx, dir, r.Ref)
	if err != nil {
		return Scan{}, fetchError(r.Repo, r.Ref, err)
	}
	listErr := func(err error) error {
		return &Error{Code: "fetch_failed", Field: "repo", Message: "Could not list the repository: " + err.Error()}
	}
	if err := g.run(ctx, dir, io.Discard, "update-ref", latestRef, commit); err != nil {
		return Scan{}, listErr(err)
	}
	// A since that cannot be fetched only means the scan compares nothing.
	sinceOK := r.Since != "" && g.ensure(ctx, dir, r.Since) == nil
	// Drop what older scans fetched; the commit this one compares with stays.
	if err := g.compact(ctx, dir, r.Since); err != nil {
		return Scan{}, listErr(err)
	}
	skillFiles, err := g.skillFiles(ctx, dir, commit, r.Paths)
	if err != nil {
		return Scan{}, listErr(err)
	}
	found := slices.Sorted(maps.Keys(skillFiles))
	switch {
	case len(found) == 0:
		return Scan{}, &Error{Code: "no_skill", Field: "path",
			Message: fmt.Sprintf("No SKILL.md at or below %s in that commit", where(r.Paths))}
	case len(found) > MaxScan:
		return Scan{}, &Error{Code: "too_many", Field: "path",
			Message: fmt.Sprintf("Narrow the path; this folder holds more than %d skills", MaxScan)}
	}
	if err := g.fetchSkillFiles(ctx, dir, commit, slices.Collect(maps.Values(skillFiles))); err != nil {
		return Scan{}, fetchError(r.Repo, r.Ref, err)
	}
	seen := map[string]bool{}
	rows := make([]ScanRow, 0, len(found))
	for _, p := range found {
		row := ScanRow{Path: p}
		m, err := g.meta(ctx, dir, skillFiles[p])
		switch {
		case err != nil:
			row.Error = err.Error()
		case seen[m.Name]:
			row.Error = "Another SKILL.md in this repository is also named " + m.Name
		default:
			seen[m.Name] = true
			row.Name, row.Description = m.Name, m.Description
		}
		rows = append(rows, row)
	}
	unchanged := sinceOK && g.sameSkills(ctx, dir, r.Since, commit, found, r.Paths)
	return Scan{Commit: commit, Unchanged: unchanged, Skills: rows}, nil
}

// skillFiles maps each folder at or below paths at commit that holds a
// SKILL.md to that file's object.
func (g Git) skillFiles(ctx context.Context, dir, commit string, paths []string) (map[string]string, error) {
	files, err := g.files(ctx, dir, commit, paths)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, f := range files {
		if path.Base(f.path) == "SKILL.md" && within(f.path, paths) {
			out[strings.TrimSuffix(strings.TrimSuffix(f.path, "SKILL.md"), "/")] = f.oid
		}
	}
	return out, nil
}

// sameSkills reports whether since holds exactly the skill folders found
// at commit below paths, each with the same tree. A file outside every
// skill folder, such as a README beside them, does not count.
func (g Git) sameSkills(ctx context.Context, dir, since, commit string, found, paths []string) bool {
	if since == commit {
		return true
	}
	before, err := g.skillFiles(ctx, dir, since, paths)
	if err != nil || !slices.Equal(slices.Sorted(maps.Keys(before)), found) {
		return false
	}
	for _, folder := range found {
		a, aerr := g.probe(ctx, dir, "rev-parse", "--verify", "-q", treeish(since, folder))
		b, berr := g.probe(ctx, dir, "rev-parse", "--verify", "-q", treeish(commit, folder))
		if aerr != nil || berr != nil || a != b {
			return false
		}
	}
	return true
}

// fetchSkillFiles fetches the SKILL.md objects oids that the cache lacks.
func (g Git) fetchSkillFiles(ctx context.Context, dir, commit string, oids []string) error {
	lacks, err := g.missing(ctx, dir, treeish(commit, ""))
	if err != nil {
		return err
	}
	var want []string
	for _, oid := range oids {
		if lacks[oid] {
			want = append(want, oid)
		}
	}
	return g.fetchBlobs(ctx, dir, want)
}

func (g Git) meta(ctx context.Context, dir, oid string) (Meta, error) {
	data, err := g.show(ctx, dir, oid)
	if err != nil {
		return Meta{}, errors.New("SKILL.md could not be read")
	}
	return ParseFrontmatter(data)
}

// within reports whether file is one of paths or below one; no paths, or
// "", means everywhere. Matching stops at folder boundaries.
func within(file string, paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	for _, p := range paths {
		if p == "" || file == p || strings.HasPrefix(file, p+"/") {
			return true
		}
	}
	return false
}

func where(paths []string) string {
	switch {
	case len(paths) == 0 || slices.Contains(paths, ""):
		return "the repository root"
	case len(paths) == 1:
		return paths[0]
	}
	return "the chosen folders"
}

func (g Git) check(r ScanRequest) error {
	if !g.AllowFile || !strings.HasPrefix(r.Repo, "file://") {
		if msg := config.CheckSkillRepo(r.Repo); msg != "" {
			return &Error{Code: "invalid_request", Field: "repo", Message: "The repository URL " + msg}
		}
	}
	if len(r.Paths) > MaxScan {
		return &Error{Code: "invalid_request", Field: "path", Message: fmt.Sprintf("Choose at most %d folders", MaxScan)}
	}
	for _, p := range r.Paths {
		if !config.ValidSkillPath(p) {
			return &Error{Code: "invalid_request", Field: "path", Message: "The path must be a folder inside the repository, such as skills/pdf"}
		}
	}
	if !config.ValidGitRef(r.Ref) {
		return &Error{Code: "invalid_request", Field: "ref", Message: "The ref must be a branch, tag or commit"}
	}
	if r.Since != "" && !config.ValidCommit(r.Since) {
		return &Error{Code: "invalid_request", Field: "since", Message: "Since must be a full commit hash"}
	}
	return nil
}

// fetchError turns git's complaint about a fetch into a sentence.
func fetchError(repo, ref string, err error) *Error {
	said := err.Error()
	var ge *gitError
	if errors.As(err, &ge) && ge.stderr != "" {
		said = ge.stderr
	}
	return ClassifyFetch(repo, ref, said)
}

// ClassifyFetch turns what git said when fetching or cloning ref from repo
// failed into an *Error: ref_not_found, auth_required or fetch_failed.
func ClassifyFetch(repo, ref, said string) *Error {
	low := strings.ToLower(said)
	switch {
	case strings.Contains(low, "couldn't find remote ref") || strings.Contains(low, "not our ref") ||
		strings.Contains(low, "unadvertised object") || strings.Contains(low, "not found in upstream"):
		return &Error{Code: "ref_not_found", Field: "ref", Message: fmt.Sprintf("Ref %s not found in that repository", ref)}
	case strings.Contains(low, "authentication failed") || strings.Contains(low, "could not read username") ||
		strings.Contains(low, "terminal prompts disabled") || strings.Contains(low, "repository not found"):
		msg := "Repository not found, or it is private."
		if u, perr := url.Parse(repo); perr == nil && (u.Host == "github.com" || u.Host == "gitlab.com") {
			msg += fmt.Sprintf(" To use a private repository, sign in to %s on the Accounts page.", u.Host)
		}
		return &Error{Code: "auth_required", Field: "repo", Message: msg}
	}
	last := strings.TrimSpace(said)
	last = last[strings.LastIndex(last, "\n")+1:]
	return &Error{Code: "fetch_failed", Field: "repo", Message: "Could not fetch the repository: " + last}
}
