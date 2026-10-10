package config

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"
)

// Skills is the skills section: the repositories skills come from.
type Skills struct {
	Sources map[string]Source `json:"sources"`
}

// Source is a repository folder at a ref, pinned to a commit. Found is its
// last scan, by skill name; Attached says which of those skills reach whom.
type Source struct {
	Repo     string                `json:"repo"`
	Ref      string                `json:"ref"`  // what the user asked for; Commit is what it resolved to
	Path     string                `json:"path"` // the folder scanned; empty is the whole repository
	Commit   string                `json:"commit"`
	Found    map[string]Found      `json:"found"`
	Attached map[string]Attachment `json:"attached"`
}

// Found is one SKILL.md of a source's last scan. A row with Error has no
// description and cannot be attached.
type Found struct {
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Valid reports whether f can be attached.
func (f Found) Valid() bool { return f.Error == "" }

// Attachment gives one found skill to agents and workspaces. Agents nil
// means every agent, including ones added later; empty means none.
// Workspaces maps a workspace id to ModeLink or ModeVendor.
type Attachment struct {
	Enabled    bool              `json:"enabled"`
	Agents     []string          `json:"agents"`
	Workspaces map[string]string `json:"workspaces"`
}

// How a workspace gets a skill: linked and hidden from git, or copied in.
const (
	ModeLink   = "link"
	ModeVendor = "vendor"
)

// MaxFound is the most skills one source lists; berth skills scan stops
// there too.
const MaxFound = 200

// Missing lists, sorted, the attached names s has no valid found row for:
// an update dropped them or broke their SKILL.md.
func (s Source) Missing() []string {
	var out []string
	for _, name := range slices.Sorted(maps.Keys(s.Attached)) {
		if f, ok := s.Found[name]; !ok || !f.Valid() {
			out = append(out, name)
		}
	}
	return out
}

// IDFrom turns text into an id of lowercase letters, digits and dashes, at
// most 64 long; "" when nothing usable is left.
func IDFrom(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	return strings.TrimRight(cut(b.String(), 64), "-")
}

// Suffixed is id with -n appended, shortened so it stays a valid id.
func Suffixed(id string, n int) string {
	tail := fmt.Sprintf("-%d", n)
	return strings.TrimRight(cut(id, 64-len(tail)), "-") + tail
}

func cut(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// SourceID derives a source id from repo's last two path segments
// (owner-repo), then owner-repo-<ref> when that is taken, then -2, -3 and
// so on.
func SourceID(repo, ref string, taken func(string) bool) string {
	base := "source"
	if u, err := url.Parse(repo); err == nil {
		segs := strings.Split(strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"), "/")
		if len(segs) > 2 {
			segs = segs[len(segs)-2:]
		}
		if id := IDFrom(strings.Join(segs, "-")); id != "" {
			base = id
		}
	}
	if !taken(base) {
		return base
	}
	id := IDFrom(base + "-" + ref)
	if !taken(id) {
		return id
	}
	for n := 2; ; n++ {
		if c := Suffixed(id, n); !taken(c) {
			return c
		}
	}
}

// ProviderOf names the source, other than skip, that already gives skill
// name to an agent that agents also picks, and that agent ("every agent"
// when both pick every agent). source is "" when there is none.
func ProviderOf(sources map[string]Source, name string, agents []string, skip string) (source, who string) {
	for _, id := range slices.Sorted(maps.Keys(sources)) {
		a, ok := sources[id].Attached[name]
		if !ok || id == skip {
			continue
		}
		if who, ok := overlap(a.Agents, agents); ok {
			return id, who
		}
	}
	return "", ""
}

// overlap names an agent both lists pick; nil picks every agent.
func overlap(a, b []string) (string, bool) {
	switch {
	case a == nil && b == nil:
		return "every agent", true
	case a == nil && len(b) > 0:
		return b[0], true
	case b == nil && len(a) > 0:
		return a[0], true
	}
	for _, p := range b {
		if slices.Contains(a, p) {
			return p, true
		}
	}
	return "", false
}

func validateSources(v *validator, sources map[string]Source, workspaces map[string]Workspace) {
	valid := map[string]Source{}
	for _, id := range slices.Sorted(maps.Keys(sources)) {
		s, field := sources[id], "skills.sources."+id
		if !WorkspaceIDPattern.MatchString(id) {
			v.add(field, "%q is not a source id: use 1 to 64 lowercase letters, digits and dashes", id)
			continue
		}
		valid[id] = s
		if msg := CheckSkillRepo(s.Repo); msg != "" {
			v.add(field+".repo", "%s", msg)
		}
		if !ValidGitRef(s.Ref) {
			v.add(field+".ref", "must be a branch, tag or commit")
		}
		if !ValidSkillPath(s.Path) {
			v.add(field+".path", "must be a folder inside the repository, such as skills")
		}
		if !ValidCommit(s.Commit) {
			v.add(field+".commit", "must be a full commit hash")
		}
		validateFound(v, field+".found", s)
		for _, name := range slices.Sorted(maps.Keys(s.Attached)) {
			validateAttachment(v, field+".attached."+name, name, s.Attached[name], workspaces)
		}
	}
	validateProviders(v, valid)
}

func validateFound(v *validator, field string, s Source) {
	if len(s.Found) > MaxFound {
		v.add(field, "must list at most %d skills", MaxFound)
		return
	}
	for _, name := range slices.Sorted(maps.Keys(s.Found)) {
		f, ff := s.Found[name], field+"."+name
		if !SkillIDPattern.MatchString(name) {
			v.add(ff, "%q is not a skill name: use 1 to 64 lowercase letters, digits and dashes", name)
			continue
		}
		if !ValidSkillPath(f.Path) || !within(f.Path, s.Path) {
			v.add(ff+".path", "must be the source's folder or a folder inside it")
		}
		switch d, e := utf8.RuneCountInString(f.Description), utf8.RuneCountInString(f.Error); {
		case e > 1024:
			v.add(ff+".error", "must be at most 1024 characters")
		case e > 0 && d > 0:
			v.add(ff+".description", "must be empty when the skill has an error")
		case e == 0 && (d == 0 || d > 1024):
			v.add(ff+".description", "must be 1 to 1024 characters")
		}
	}
}

// within reports whether folder p is dir or inside it; "" is the whole
// repository.
func within(p, dir string) bool { return dir == "" || p == dir || strings.HasPrefix(p, dir+"/") }

func validateAttachment(v *validator, field, name string, a Attachment, workspaces map[string]Workspace) {
	if !SkillIDPattern.MatchString(name) {
		v.add(field, "%q is not a skill name: use 1 to 64 lowercase letters, digits and dashes", name)
		return
	}
	for i, p := range a.Agents {
		if !packagePattern.MatchString(p) {
			v.add(fmt.Sprintf("%s.agents[%d]", field, i), "%q is not an npm package name", p)
		}
	}
	for _, ws := range slices.Sorted(maps.Keys(a.Workspaces)) {
		switch _, ok := workspaces[ws]; {
		case !ok:
			v.add(field+".workspaces."+ws, "there is no workspace %s", ws)
		case a.Workspaces[ws] != ModeLink && a.Workspaces[ws] != ModeVendor:
			v.add(field+".workspaces."+ws, "must be link or vendor")
		}
	}
}

// validateProviders enforces one provider per skill name per target: for
// every agent and every workspace, at most one source attaches a name,
// enabled or not. The later source, in id order, carries the error.
func validateProviders(v *validator, sources map[string]Source) {
	earlier := map[string]Source{}
	for _, id := range slices.Sorted(maps.Keys(sources)) {
		s := sources[id]
		for _, name := range slices.Sorted(maps.Keys(s.Attached)) {
			a, field := s.Attached[name], "skills.sources."+id+".attached."+name
			if other, who := ProviderOf(earlier, name, a.Agents, ""); other != "" {
				v.add(field+".agents", "%s already gets %s from %s", who, name, other)
			}
			for _, ws := range slices.Sorted(maps.Keys(a.Workspaces)) {
				for _, oid := range slices.Sorted(maps.Keys(earlier)) {
					if _, ok := earlier[oid].Attached[name].Workspaces[ws]; ok {
						v.add(field+".workspaces."+ws, "workspace %s already gets %s from %s", ws, name, oid)
						break
					}
				}
			}
		}
		earlier[id] = s
	}
}

func validateMCPWorkspaces(v *validator, m map[string][]string, workspaces map[string]Workspace) {
	for _, id := range slices.Sorted(maps.Keys(m)) {
		field := "mcp.workspaces." + id
		if !serverIDPattern.MatchString(id) {
			v.add(field, "%q is not a server id", id)
			continue
		}
		if len(m[id]) == 0 {
			v.add(field, "must name at least one workspace; remove it to attach the server to none")
		}
		for i, ws := range m[id] {
			if _, ok := workspaces[ws]; !ok {
				v.add(fmt.Sprintf("%s[%d]", field, i), "there is no workspace %s", ws)
			}
		}
	}
}

// normalize fills empty maps and sorts agent lists, so equal sections
// compare and print the same.
func (s *Skills) normalize() {
	if s.Sources == nil {
		s.Sources = map[string]Source{}
	}
	for id, src := range s.Sources {
		if src.Found == nil {
			src.Found = map[string]Found{}
		}
		if src.Attached == nil {
			src.Attached = map[string]Attachment{}
		}
		for name, a := range src.Attached {
			if a.Agents != nil {
				slices.Sort(a.Agents)
				a.Agents = slices.Compact(a.Agents)
			}
			if a.Workspaces == nil {
				a.Workspaces = map[string]string{}
			}
			src.Attached[name] = a
		}
		s.Sources[id] = src
	}
}
