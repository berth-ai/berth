package config

import (
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Skill is a skill installed from a git repository: the folder holding its
// SKILL.md, pinned to a commit. Its id is the name in that SKILL.md.
type Skill struct {
	Repo        string   `json:"repo"`
	Path        string   `json:"path"` // folder in the repository; empty is the root
	Ref         string   `json:"ref"`  // what the user asked for; Commit is what it resolved to
	Commit      string   `json:"commit"`
	Description string   `json:"description"`
	Agents      []string `json:"agents"` // nil means all agents
	Enabled     bool     `json:"enabled"`
}

var (
	// SkillIDPattern is the Agent Skills name rule.
	SkillIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	commitPattern  = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
)

// CheckSkillRepo returns why raw cannot be a skill repository URL, or "".
func CheckSkillRepo(raw string) string {
	if raw == "" {
		return "is required"
	}
	if len(raw) > 2048 {
		return "must be at most 2048 characters"
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" || u.Opaque != "" {
		return "must be a repository URL such as https://github.com/owner/repo"
	}
	if u.Scheme != "https" {
		return "must start with https://"
	}
	if u.User != nil {
		return "must not contain a user name or password; sign in on the Accounts page instead"
	}
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "must not contain ? or #"
	}
	return ""
}

// ValidSkillPath reports whether p names a folder inside a repository.
func ValidSkillPath(p string) bool {
	if p == "" {
		return true
	}
	if len(p) > 1024 || strings.HasPrefix(p, "/") || strings.ContainsAny(p, `\:`) ||
		strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// ValidGitRef reports whether ref is a branch, tag or commit git accepts
// (git check-ref-format --allow-onelevel, plus no leading dash).
func ValidGitRef(ref string) bool {
	if ref == "" || len(ref) > 255 || ref == "@" ||
		strings.HasPrefix(ref, "-") || strings.HasPrefix(ref, "/") ||
		strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") ||
		strings.Contains(ref, "..") || strings.Contains(ref, "//") || strings.Contains(ref, "@{") {
		return false
	}
	if strings.ContainsFunc(ref, func(r rune) bool {
		return r <= ' ' || r == 0x7f || strings.ContainsRune("~^:?*[\\", r)
	}) {
		return false
	}
	for _, seg := range strings.Split(ref, "/") {
		if strings.HasPrefix(seg, ".") || strings.HasSuffix(seg, ".lock") {
			return false
		}
	}
	return true
}

// ValidCommit reports whether c is a full SHA-1 or SHA-256 commit id.
func ValidCommit(c string) bool { return commitPattern.MatchString(c) }

func validateSkills(v *validator, skills map[string]Skill) {
	for _, id := range slices.Sorted(maps.Keys(skills)) {
		s, field := skills[id], "skills."+id
		if !SkillIDPattern.MatchString(id) {
			v.add(field, "%q is not a skill name: use 1 to 64 lowercase letters, digits and dashes", id)
			continue
		}
		if msg := CheckSkillRepo(s.Repo); msg != "" {
			v.add(field+".repo", "%s", msg)
		}
		if !ValidSkillPath(s.Path) {
			v.add(field+".path", "must be a folder inside the repository, such as skills/pdf")
		}
		if !ValidGitRef(s.Ref) {
			v.add(field+".ref", "must be a branch, tag or commit")
		}
		if !ValidCommit(s.Commit) {
			v.add(field+".commit", "must be a full commit hash")
		}
		if n := utf8.RuneCountInString(s.Description); n == 0 || n > 1024 {
			v.add(field+".description", "must be 1 to 1024 characters")
		}
		if s.Agents != nil && len(s.Agents) == 0 {
			v.add(field+".agents", "must name at least one agent; use null to make the skill available to all agents")
		}
		for i, p := range s.Agents {
			if !packagePattern.MatchString(p) {
				v.add(fmt.Sprintf("%s.agents[%d]", field, i), "%q is not an npm package name", p)
			}
		}
	}
}
