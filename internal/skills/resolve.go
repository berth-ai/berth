package skills

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/berth-ai/berth/internal/config"
)

// Request names a skill in a repository.
type Request struct {
	Repo string
	Path string // folder in the repository; empty is the root
	Ref  string
}

// Resolved is a request pinned to a commit, with what its SKILL.md says.
type Resolved struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Commit      string `json:"commit"`
}

// Error is a resolve failure the panel shows next to one field.
type Error struct {
	Code    string // invalid_request, ref_not_found, no_skill, bad_skill, auth_required, fetch_failed
	Field   string // repo, path or ref
	Message string
}

func (e *Error) Error() string { return e.Message }

// Resolve fetches r.Ref, pins it to a commit and checks the SKILL.md at
// r.Path. Every failure is an *Error.
func (g Git) Resolve(ctx context.Context, home string, r Request) (Resolved, error) {
	if err := g.check(r); err != nil {
		return Resolved{}, err
	}
	dir := CacheDir(home, r.Repo)
	commit, err := g.Fetch(ctx, dir, r.Repo, r.Ref)
	if err != nil {
		return Resolved{}, fetchError(r, err)
	}
	file := "SKILL.md"
	if r.Path != "" {
		file = r.Path + "/SKILL.md"
	}
	data, err := g.Show(ctx, dir, commit, file)
	if err != nil {
		return Resolved{}, &Error{Code: "no_skill", Field: "path",
			Message: fmt.Sprintf("No SKILL.md at %s in that commit", cmp.Or(r.Path, "the repository root"))}
	}
	m, err := ParseFrontmatter(data)
	if err != nil {
		return Resolved{}, &Error{Code: "bad_skill", Field: "path", Message: err.Error()}
	}
	return Resolved{Name: m.Name, Description: m.Description, Commit: commit}, nil
}

func (g Git) check(r Request) error {
	if !g.AllowFile || !strings.HasPrefix(r.Repo, "file://") {
		if msg := config.CheckSkillRepo(r.Repo); msg != "" {
			return &Error{Code: "invalid_request", Field: "repo", Message: "The repository URL " + msg}
		}
	}
	if !config.ValidSkillPath(r.Path) {
		return &Error{Code: "invalid_request", Field: "path", Message: "The path must be a folder inside the repository, such as skills/pdf"}
	}
	if !config.ValidGitRef(r.Ref) {
		return &Error{Code: "invalid_request", Field: "ref", Message: "The ref must be a branch, tag or commit"}
	}
	return nil
}

// fetchError turns git's complaint about a fetch into a sentence.
func fetchError(r Request, err error) *Error {
	said := err.Error()
	var ge *gitError
	if errors.As(err, &ge) && ge.stderr != "" {
		said = ge.stderr
	}
	low := strings.ToLower(said)
	switch {
	case strings.Contains(low, "couldn't find remote ref") || strings.Contains(low, "not our ref") ||
		strings.Contains(low, "unadvertised object"):
		return &Error{Code: "ref_not_found", Field: "ref", Message: fmt.Sprintf("Ref %s not found in that repository", r.Ref)}
	case strings.Contains(low, "authentication failed") || strings.Contains(low, "could not read username") ||
		strings.Contains(low, "terminal prompts disabled") || strings.Contains(low, "repository not found"):
		msg := "Repository not found, or it is private."
		if u, perr := url.Parse(r.Repo); perr == nil && (u.Host == "github.com" || u.Host == "gitlab.com") {
			msg += fmt.Sprintf(" To use a private repository, sign in to %s on the Accounts page.", u.Host)
		}
		return &Error{Code: "auth_required", Field: "repo", Message: msg}
	}
	return &Error{Code: "fetch_failed", Field: "repo", Message: "Could not fetch the repository: " + err.Error()}
}
