package config

import (
	"cmp"
	"maps"
	"path"
	"slices"
	"strings"
)

// MigrateSkills turns a schema 2 to 5 skills map, keyed by skill name,
// into sources: one per repository and ref, split by commit when the
// group's skills drifted apart, so no skill moves to another commit. The
// first commit, by hash, keeps the derived id; the others get -2, -3.
func MigrateSkills(old map[string]Skill) map[string]Source {
	type key struct{ repo, ref, commit string }
	groups := map[key][]string{}
	for _, name := range slices.Sorted(maps.Keys(old)) {
		s := old[name]
		k := key{s.Repo, s.Ref, s.Commit}
		groups[k] = append(groups[k], name)
	}
	keys := slices.SortedFunc(maps.Keys(groups), func(a, b key) int {
		return cmp.Or(cmp.Compare(a.repo, b.repo), cmp.Compare(a.ref, b.ref), cmp.Compare(a.commit, b.commit))
	})
	out := map[string]Source{}
	taken := func(id string) bool { _, ok := out[id]; return ok }
	var first string
	for i, k := range keys {
		var id string
		if i > 0 && keys[i-1].repo == k.repo && keys[i-1].ref == k.ref {
			for n := 2; ; n++ {
				if id = Suffixed(first, n); !taken(id) {
					break
				}
			}
		} else {
			id = SourceID(k.repo, k.ref, taken)
			first = id
		}
		src := Source{Repo: k.repo, Ref: k.ref, Commit: k.commit, Found: map[string]Found{}, Attached: map[string]Attachment{}}
		var paths []string
		for _, name := range groups[k] {
			s := old[name]
			paths = append(paths, s.Path)
			src.Found[name] = Found{Path: s.Path, Description: s.Description}
			src.Attached[name] = Attachment{Enabled: s.Enabled, Agents: s.Agents, Workspaces: map[string]string{}}
		}
		src.Path = commonParent(paths)
		out[id] = src
	}
	return out
}

// commonParent is the deepest folder holding every one of paths; a skill
// at the repository root, or in a top-level folder, makes it the root.
func commonParent(paths []string) string {
	var common []string
	for i, p := range paths {
		var dir []string
		if d := path.Dir(p); p != "" && d != "." {
			dir = strings.Split(d, "/")
		}
		if i == 0 {
			common = dir
			continue
		}
		n := 0
		for n < len(common) && n < len(dir) && common[n] == dir[n] {
			n++
		}
		common = common[:n]
	}
	return strings.Join(common, "/")
}
