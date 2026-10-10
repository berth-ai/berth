package config

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
)

// MigrateSkills turns a schema 2 to 5 skills map, keyed by skill name,
// into sources: one per repository and ref, split by commit when the
// group's skills drifted apart, so no skill moves to another commit. The
// first commit, by hash, keeps the derived id; the others get -2, -3.
func MigrateSkills(old map[string]LegacySkill) map[string]Source {
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

// sourcesSchema is the first schema whose skills section holds sources.
const sourcesSchema = 6

// Upgrade decodes a config file or export of schema 1 to Schema into the
// current shape: settings it lacks take their defaults, a schema 2 to 5
// skills map becomes sources, and the schema becomes Schema. Unknown keys
// and a newer schema are refused. The result is not validated.
func Upgrade(raw []byte) (Config, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Config{}, err
	}
	if doc == nil {
		return Config{}, errors.New("a config must be a JSON object")
	}
	var schema int
	if s, ok := doc["schema"]; ok {
		if err := json.Unmarshal(s, &schema); err != nil {
			return Config{}, fmt.Errorf("schema: %w", err)
		}
	}
	if schema > Schema {
		return Config{}, fmt.Errorf("schema %d is newer than this version of Berth understands (%d)", schema, Schema)
	}
	if old, ok := doc["skills"]; ok && schema >= 1 && schema < sourcesSchema {
		legacy := map[string]LegacySkill{}
		dec := json.NewDecoder(bytes.NewReader(old))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&legacy); err != nil {
			return Config{}, fmt.Errorf("skills: %w", err)
		}
		migrated, err := json.Marshal(Skills{Sources: MigrateSkills(legacy)})
		if err != nil {
			return Config{}, err
		}
		doc["skills"] = migrated
		if raw, err = json.Marshal(doc); err != nil {
			return Config{}, err
		}
	}
	c := Defaults()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, err
	}
	if c.Schema >= 1 && c.Schema < Schema {
		c.Schema = Schema // older schemas lack only settings whose defaults apply
	}
	return c, nil
}
