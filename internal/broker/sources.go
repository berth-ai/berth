package broker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path"
	"slices"
	"unicode/utf8"

	"github.com/berth-ai/berth/internal/config"
)

// foundOf turns a scan into found rows: valid skills by name, broken ones
// by their folder's name made into an id ("root" at the repository root),
// suffixed when that is taken.
func foundOf(rows []scanRow) map[string]config.Found {
	out := make(map[string]config.Found, len(rows))
	for _, r := range rows {
		if r.Error == "" && r.Name != "" {
			out[r.Name] = config.Found{Path: r.Path, Description: r.Description}
		}
	}
	for _, r := range rows {
		if r.Error == "" && r.Name != "" {
			continue
		}
		base := cmp.Or(config.IDFrom(path.Base(r.Path)), "root")
		key := base
		for n := 2; ; n++ {
			if _, taken := out[key]; !taken {
				break
			}
			key = config.Suffixed(base, n)
		}
		out[key] = config.Found{Path: r.Path, Error: clip(cmp.Or(r.Error, "SKILL.md has no name"), 1024)}
	}
	return out
}

// clip keeps at most n runes of s.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// sourceTracking names the source that already tracks repo at ref and
// path, or "".
func sourceTracking(cur config.Config, repo, ref, p string) string {
	for _, id := range slices.Sorted(maps.Keys(cur.Skills.Sources)) {
		if s := cur.Skills.Sources[id]; s.Repo == repo && s.Ref == ref && s.Path == p {
			return id
		}
	}
	return ""
}

// checkSourceFields checks a new source's repository, ref and path.
func checkSourceFields(repo, ref, p string) *fieldError {
	if field, msg := skillSourceError(repo, ref); field != "" {
		return &fieldError{Code: "invalid_request", Field: field, Message: msg}
	}
	if !config.ValidSkillPath(p) {
		return &fieldError{Code: "invalid_request", Field: "path", Message: skillPathMsg}
	}
	return nil
}

// checkNewSource says why a source tracking repo at ref and path, with id
// (empty means derived), cannot be added to cur.
func checkNewSource(cur config.Config, repo, ref, p, id string) *fieldError {
	if other := sourceTracking(cur, repo, ref, p); other != "" {
		return &fieldError{Code: "source_exists", Field: "repo",
			Message: fmt.Sprintf("Source %s already tracks this repository at that ref and path", other)}
	}
	switch _, taken := cur.Skills.Sources[id]; {
	case id == "":
	case !config.WorkspaceIDPattern.MatchString(id):
		return &fieldError{Code: "invalid_request", Field: "id", Message: "The id must be 1 to 64 lowercase letters, digits and dashes"}
	case taken:
		return &fieldError{Code: "id_exists", Field: "id", Message: fmt.Sprintf("A source named %s exists; choose another id", id)}
	}
	return nil
}

func sourceIDFor(cur config.Config, repo, ref string) string {
	return config.SourceID(repo, ref, func(id string) bool { _, ok := cur.Skills.Sources[id]; return ok })
}

// diffRow is one skill a preview reports; a moved row's From is the old path.
type diffRow struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	From        string `json:"from,omitempty"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
	Attached    bool   `json:"attached"`
}

// sourceDiff is skills.sources.preview's answer for an existing source.
type sourceDiff struct {
	ID        string    `json:"id"`
	Ref       string    `json:"ref"`
	From      string    `json:"from"`
	Commit    string    `json:"commit"`
	Changed   bool      `json:"changed"`
	Added     []diffRow `json:"added"`
	Removed   []diffRow `json:"removed"`
	Moved     []diffRow `json:"moved"`
	Described []diffRow `json:"described"`
}

// diffFound compares a source's found rows with a new scan's.
func diffFound(src config.Source, next map[string]config.Found) (added, removed, moved, described []diffRow) {
	added, removed, moved, described = []diffRow{}, []diffRow{}, []diffRow{}, []diffRow{}
	row := func(name string, f config.Found) diffRow {
		_, att := src.Attached[name]
		return diffRow{Name: name, Path: f.Path, Description: f.Description, Error: f.Error, Attached: att}
	}
	for _, name := range slices.Sorted(maps.Keys(next)) {
		n, r := next[name], row(name, next[name])
		switch o, had := src.Found[name]; {
		case !had:
			added = append(added, r)
		case o.Path != n.Path:
			r.From = o.Path
			moved = append(moved, r)
		case o.Description != n.Description || o.Error != n.Error:
			described = append(described, r)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(src.Found)) {
		if _, ok := next[name]; !ok {
			removed = append(removed, row(name, src.Found[name]))
		}
	}
	return added, removed, moved, described
}

func (b *Broker) skillsSourcesPreview(_ context.Context, body []byte) (int, any) {
	var in struct {
		ID   string `json:"id"`
		Repo string `json:"repo"`
		Ref  string `json:"ref"`
		Path string `json:"path"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	if in.ID == "" {
		if fe := checkSourceFields(in.Repo, in.Ref, in.Path); fe != nil {
			return rejectJob(fe)
		}
		return b.scanJob("skills.sources.preview", in.Repo, in.Ref, []string{in.Path}, func(s skillScan) (json.RawMessage, error) {
			now, _, _ := b.snapshot()
			return json.Marshal(map[string]any{"commit": s.Commit, "found": foundOf(s.Skills),
				"id": sourceIDFor(now, in.Repo, in.Ref), "exists": sourceTracking(now, in.Repo, in.Ref, in.Path)})
		})
	}
	cur, _, _ := b.snapshot()
	src, ok := cur.Skills.Sources[in.ID]
	switch {
	case !ok:
		return fail(http.StatusNotFound, "unknown_source", "No source named %s", in.ID)
	case in.Repo != "" || in.Path != "":
		return badRequest(errors.New("a preview of an existing source takes only id and ref"))
	}
	ref := cmp.Or(in.Ref, src.Ref)
	if !config.ValidGitRef(ref) {
		return rejectJob(&fieldError{Code: "invalid_request", Field: "ref", Message: "The ref must be a branch, tag or commit"})
	}
	return b.scanJob("skills.sources.preview", src.Repo, ref, []string{src.Path}, func(s skillScan) (json.RawMessage, error) {
		d := sourceDiff{ID: in.ID, Ref: ref, From: src.Commit, Commit: s.Commit, Changed: s.Commit != src.Commit || ref != src.Ref}
		next := foundOf(s.Skills)
		if s.Commit == src.Commit {
			next = src.Found
		}
		d.Added, d.Removed, d.Moved, d.Described = diffFound(src, next)
		return json.Marshal(d)
	})
}

func (b *Broker) skillsSourcesAdd(_ context.Context, body []byte) (int, any) {
	var in struct {
		Repo      string `json:"repo"`
		Ref       string `json:"ref"`
		Path      string `json:"path"`
		ID        string `json:"id"`
		Commit    string `json:"commit"`
		AttachAll bool   `json:"attach_all"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if fe := checkSourceFields(in.Repo, in.Ref, in.Path); fe != nil {
		return rejectJob(fe)
	}
	if in.Commit != "" && !config.ValidCommit(in.Commit) {
		return rejectJob(&fieldError{Code: "invalid_request", Field: "commit", Message: "Find the skills again; the scanned commit is missing"})
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	cur, _, _ := b.snapshot()
	if fe := checkNewSource(cur, in.Repo, in.Ref, in.Path, in.ID); fe != nil {
		return rejectJob(fe)
	}
	return b.scanJob("skills.sources.add", in.Repo, cmp.Or(in.Commit, in.Ref), []string{in.Path}, func(s skillScan) (json.RawMessage, error) {
		found := foundOf(s.Skills)
		id, attached, skipped := in.ID, []string{}, []string{}
		next, _, err := b.applyPatch(func(cur config.Config) (map[string]any, error) {
			if fe := checkNewSource(cur, in.Repo, in.Ref, in.Path, in.ID); fe != nil {
				return nil, fe
			}
			id = cmp.Or(in.ID, sourceIDFor(cur, in.Repo, in.Ref))
			att := map[string]any{}
			for _, name := range slices.Sorted(maps.Keys(found)) {
				switch other, _ := config.ProviderOf(cur.Skills.Sources, name, nil, ""); {
				case !in.AttachAll || !found[name].Valid():
				case other != "":
					skipped = append(skipped, name)
				default:
					att[name] = map[string]any{} // the template: enabled, every agent
					attached = append(attached, name)
				}
			}
			return map[string]any{"skills": map[string]any{"sources": map[string]any{id: map[string]any{
				"repo": in.Repo, "ref": in.Ref, "path": in.Path, "commit": s.Commit, "found": found, "attached": att}}}}, nil
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"id": id, "commit": s.Commit, "attached": attached, "skipped": skipped,
			"changed": true, "revision": next.Revision})
	})
}

// checkAttachments refuses a new attachment whose name is not a valid
// found row of its source. An existing one stays even after an update
// dropped its row; it is missing then.
func checkAttachments(cur, next config.Config) error {
	var fields []config.FieldError
	for _, id := range slices.Sorted(maps.Keys(next.Skills.Sources)) {
		src := next.Skills.Sources[id]
		for _, name := range slices.Sorted(maps.Keys(src.Attached)) {
			if _, had := cur.Skills.Sources[id].Attached[name]; had {
				continue
			}
			field := "skills.sources." + id + ".attached." + name
			switch f, ok := src.Found[name]; {
			case !ok:
				fields = append(fields, config.FieldError{Field: field, Message: fmt.Sprintf("%s has no skill named %s; update the source first", id, name)})
			case !f.Valid():
				fields = append(fields, config.FieldError{Field: field, Message: fmt.Sprintf("%s can't be attached: %s", name, f.Error)})
			}
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return &config.ValidationError{Fields: fields}
}
