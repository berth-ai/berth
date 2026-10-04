package skills

import (
	"context"
	"errors"
	"fmt"
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
}

// ScanRow is one SKILL.md a scan found; a row with Error has no name.
type ScanRow struct {
	Path        string `json:"path"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Scan is a ref pinned to a commit and the skills found there, by path.
type Scan struct {
	Commit string    `json:"commit"`
	Skills []ScanRow `json:"skills"`
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
// of the scan itself is an *Error.
func (g Git) Scan(ctx context.Context, home string, r ScanRequest) (Scan, error) {
	if err := g.check(r); err != nil {
		return Scan{}, err
	}
	dir := CacheDir(home, r.Repo)
	commit, err := g.Fetch(ctx, dir, r.Repo, r.Ref)
	if err != nil {
		return Scan{}, fetchError(r.Repo, r.Ref, err)
	}
	files, err := g.ListFiles(ctx, dir, commit, r.Paths)
	if err != nil {
		return Scan{}, &Error{Code: "fetch_failed", Field: "repo", Message: "Could not list the repository: " + err.Error()}
	}
	var found []string
	for _, f := range files {
		if path.Base(f) == "SKILL.md" && within(f, r.Paths) {
			found = append(found, strings.TrimSuffix(strings.TrimSuffix(f, "SKILL.md"), "/"))
		}
	}
	slices.Sort(found)
	found = slices.Compact(found)
	switch {
	case len(found) == 0:
		return Scan{}, &Error{Code: "no_skill", Field: "path",
			Message: fmt.Sprintf("No SKILL.md at or below %s in that commit", where(r.Paths))}
	case len(found) > MaxScan:
		return Scan{}, &Error{Code: "too_many", Field: "path",
			Message: fmt.Sprintf("Narrow the path; this folder holds more than %d skills", MaxScan)}
	}
	seen := map[string]bool{}
	rows := make([]ScanRow, 0, len(found))
	for _, p := range found {
		row := ScanRow{Path: p}
		m, err := g.meta(ctx, dir, commit, p)
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
	return Scan{Commit: commit, Skills: rows}, nil
}

func (g Git) meta(ctx context.Context, dir, commit, folder string) (Meta, error) {
	data, err := g.Show(ctx, dir, commit, path.Join(folder, "SKILL.md"))
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
	return nil
}

// fetchError turns git's complaint about a fetch into a sentence.
func fetchError(repo, ref string, err error) *Error {
	said := err.Error()
	var ge *gitError
	if errors.As(err, &ge) && ge.stderr != "" {
		said = ge.stderr
	}
	low := strings.ToLower(said)
	switch {
	case strings.Contains(low, "couldn't find remote ref") || strings.Contains(low, "not our ref") ||
		strings.Contains(low, "unadvertised object"):
		return &Error{Code: "ref_not_found", Field: "ref", Message: fmt.Sprintf("Ref %s not found in that repository", ref)}
	case strings.Contains(low, "authentication failed") || strings.Contains(low, "could not read username") ||
		strings.Contains(low, "terminal prompts disabled") || strings.Contains(low, "repository not found"):
		msg := "Repository not found, or it is private."
		if u, perr := url.Parse(repo); perr == nil && (u.Host == "github.com" || u.Host == "gitlab.com") {
			msg += fmt.Sprintf(" To use a private repository, sign in to %s on the Accounts page.", u.Host)
		}
		return &Error{Code: "auth_required", Field: "repo", Message: msg}
	}
	return &Error{Code: "fetch_failed", Field: "repo", Message: "Could not fetch the repository: " + err.Error()}
}
