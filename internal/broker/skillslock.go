package broker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/skills"
)

// lockSkill is one skill of a workspace's skills-lock.json, as berth
// workspaces skills-lock reports it, plus the source that already gives
// it to the workspace, if any.
type lockSkill struct {
	skills.LockSkill
	Provider string `json:"provider,omitempty"`
}

// readSkillsLock runs berth workspaces skills-lock as agent for workspace id.
func (b *Broker) readSkillsLock(ctx context.Context, log io.Writer, id string) (skills.Lock, error) {
	args := []string{"workspaces", "skills-lock", "--json", "--id", id}
	stdout, code, err := b.runCLI(ctx, log, args, b.syncTimeout())
	switch {
	case err != nil:
		return skills.Lock{}, &fieldError{Code: "lock_failed", Message: err.Error()}
	case code != 0:
		return skills.Lock{}, cliError(stdout, args)
	}
	var lock skills.Lock
	if json.Unmarshal(stdout, &lock) != nil {
		return skills.Lock{}, &fieldError{Code: "bad_output", Message: "berth workspaces skills-lock printed no result"}
	}
	return lock, nil
}

// providerFor names the source that attaches skill name to workspace ws.
func providerFor(cur config.Config, name, ws string) string {
	for _, sid := range slices.Sorted(maps.Keys(cur.Skills.Sources)) {
		if _, ok := cur.Skills.Sources[sid].Attached[name].Workspaces[ws]; ok {
			return sid
		}
	}
	return ""
}

// workspacesSkillsLock answers what workspace id's skills-lock.json asks
// for and which of those skills Berth already gives it. It changes nothing.
func (b *Broker) workspacesSkillsLock(ctx context.Context, body []byte) (int, any) {
	var in struct {
		ID string `json:"id"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	cur, _, _ := b.snapshot()
	if _, ok := cur.Workspaces[in.ID]; !ok {
		return fail(http.StatusNotFound, "unknown_workspace", "No workspace named %s", in.ID)
	}
	var log strings.Builder
	lock, err := b.readSkillsLock(ctx, &log, in.ID)
	if err != nil {
		var fe *fieldError
		if errors.As(err, &fe) && fe.Code == "bad_lock" {
			return fail(http.StatusConflict, fe.Code, "%s", fe.Message)
		}
		return fail(http.StatusInternalServerError, "lock_failed", "%v %s", err, strings.TrimSpace(log.String()))
	}
	rows := make([]lockSkill, 0, len(lock.Skills))
	for _, s := range lock.Skills {
		rows = append(rows, lockSkill{LockSkill: s, Provider: providerFor(cur, s.Name, in.ID)})
	}
	return http.StatusOK, map[string]any{"found": lock.Found, "version": lock.Version, "skills": rows}
}

// lockGroup is the skills of a lock that one new source gives: a
// repository at a ref and commit (Path the folder holding them all), or a
// website.
type lockGroup struct {
	kind, repo, ref, commit, path string
	names                         []string
}

func (g *lockGroup) target() scanTarget {
	if g.kind == config.KindWellKnown {
		return scanTarget{Kind: g.kind, Repo: g.repo}
	}
	ref := g.ref
	if g.commit != "" {
		ref = g.commit
	}
	return scanTarget{Repo: g.repo, Ref: ref, Paths: []string{g.path}}
}

// commonFolder is the deepest folder holding every one of folders; "" is
// the repository root.
func commonFolder(folders []string) string {
	if len(folders) == 0 {
		return ""
	}
	common := strings.Split(folders[0], "/")
	for _, f := range folders[1:] {
		segs := strings.Split(f, "/")
		n := 0
		for n < len(common) && n < len(segs) && common[n] == segs[n] {
			n++
		}
		common = common[:n]
	}
	return path.Join(common...)
}

// sameSite reports whether two website URLs share a host.
func sameSite(a, b string) bool {
	ua, ea := url.Parse(a)
	ub, eb := url.Parse(b)
	return ea == nil && eb == nil && ua.Host != "" && strings.EqualFold(ua.Host, ub.Host)
}

// reusable names the source in cur that already has skill s, valid, so the
// workspace can get it from there, or "".
func reusable(cur config.Config, s skills.LockSkill) string {
	for _, sid := range slices.Sorted(maps.Keys(cur.Skills.Sources)) {
		src := cur.Skills.Sources[sid]
		if f, ok := src.Found[s.Name]; !ok || !f.Valid() {
			continue
		}
		switch {
		case s.Kind == config.KindWellKnown && src.Kind == config.KindWellKnown && sameSite(src.Repo, s.Repo):
			return sid
		case s.Kind == config.KindGit && src.Kind == config.KindGit && src.Repo == s.Repo && src.Ref == s.Ref &&
			(src.Path == "" || s.Folder == src.Path || strings.HasPrefix(s.Folder, src.Path+"/")):
			return sid
		}
	}
	return ""
}

// lockSkip is a skill skills_lock.add left out, and why.
type lockSkip struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// workspacesSkillsLockAdd gives workspace id the skills names of its
// skills-lock.json, linked into the workspace and to no agent: each from a
// source that already has it, else from a new source pinned to the lock's
// commit (one per repository, ref and commit, or per website). A skill the
// workspace already holds or gets is left out.
func (b *Broker) workspacesSkillsLockAdd(_ context.Context, body []byte) (int, any) {
	var in struct {
		ID    string   `json:"id"`
		Names []string `json:"names"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	if len(in.Names) == 0 || len(in.Names) > skills.MaxScan {
		return rejectJob(&fieldError{Code: "invalid_request", Field: "names",
			Message: fmt.Sprintf("List 1 to %d skills to add", skills.MaxScan)})
	}
	cur, _, _ := b.snapshot()
	if _, ok := cur.Workspaces[in.ID]; !ok {
		return fail(http.StatusNotFound, "unknown_workspace", "No workspace named %s", in.ID)
	}
	shown := []string{b.Home.Berth, "workspaces", "skills-lock", "--json", "--id", in.ID}
	return b.bulkJob("workspaces.skills_lock.add", shown, func(ctx context.Context, out io.Writer) (int, json.RawMessage, error) {
		lock, err := b.readSkillsLock(ctx, out, in.ID)
		if err != nil {
			fmt.Fprintf(out, "error: %v\n", err)
			return 1, fieldFailure(err), nil
		}
		byName := map[string]skills.LockSkill{}
		for _, s := range lock.Skills {
			byName[s.Name] = s
		}
		skipped := []lockSkip{}
		skip := func(name, reason string) {
			fmt.Fprintf(out, "skip      %s: %s\n", name, reason)
			skipped = append(skipped, lockSkip{Name: name, Reason: reason})
		}
		// Plan from the config as it is now; the patch checks again.
		now, _, _ := b.snapshot()
		reuse := map[string]string{} // name → source
		groups := map[string]*lockGroup{}
		folders := map[string][]string{}
		names := slices.Clone(in.Names)
		slices.Sort(names)
		for _, name := range slices.Compact(names) {
			s, ok := byName[name]
			switch {
			case !ok:
				skip(name, "The lock file no longer lists it")
				continue
			case s.Repo == "":
				skip(name, s.Reason)
				continue
			case s.Installed == skills.LockMatch:
				skip(name, "The workspace already has it in .agents/skills")
				continue
			case s.Installed != skills.LockAbsent:
				skip(name, "The workspace has a folder of that name in .agents/skills; Berth leaves it alone")
				continue
			}
			if p := providerFor(now, name, in.ID); p != "" {
				skip(name, "The workspace already gets it from "+p)
				continue
			}
			if sid := reusable(now, s); sid != "" {
				reuse[name] = sid
				continue
			}
			key := strings.Join([]string{s.Kind, s.Repo, s.Ref, s.Commit}, "\x00")
			g := groups[key]
			if g == nil {
				g = &lockGroup{kind: s.Kind, repo: s.Repo, ref: s.Ref, commit: s.Commit}
				groups[key] = g
			}
			g.names = append(g.names, name)
			folders[key] = append(folders[key], s.Folder)
		}
		scans := map[string]skillScan{}
		for _, key := range slices.Sorted(maps.Keys(groups)) {
			g := groups[key]
			if g.kind != config.KindWellKnown {
				g.path = commonFolder(folders[key])
			}
			fmt.Fprintf(out, "scan      %s %s\n", g.repo, cmp.Or(g.commit, g.ref, "index"))
			s, err := b.scanNow(ctx, out, g.target())
			if err != nil {
				for _, name := range g.names {
					skip(name, asFieldError(err).Message)
				}
				delete(groups, key)
				continue
			}
			found := foundOf(s.Skills)
			var kept []string
			for _, name := range g.names {
				if f, ok := found[name]; ok && f.Valid() {
					kept = append(kept, name)
				} else {
					skip(name, "The source has no valid skill of that name")
				}
			}
			if g.names = kept; len(kept) == 0 {
				delete(groups, key)
				continue
			}
			scans[key] = s
		}
		type added struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		}
		addedRows, newSources, late := []added{}, []string{}, []lockSkip{}
		next, changed, err := b.applyPatch(func(cur config.Config) (map[string]any, error) {
			// This may run again on a conflicting write; start over each time.
			addedRows, newSources, late = []added{}, []string{}, []lockSkip{}
			skip := func(name, reason string) { late = append(late, lockSkip{Name: name, Reason: reason}) }
			sources := map[string]any{}
			taken := map[string]bool{}
			for sid := range cur.Skills.Sources {
				taken[sid] = true
			}
			attach := func(sid, name string, src config.Source, patch map[string]any) {
				if p := providerFor(cur, name, in.ID); p != "" {
					skip(name, "The workspace already gets it from "+p)
					return
				}
				att, _ := patch["attached"].(map[string]any)
				if att == nil {
					att = map[string]any{}
					patch["attached"] = att
				}
				ws := map[string]any{in.ID: config.ModeLink}
				if _, had := src.Attached[name]; had {
					att[name] = map[string]any{"workspaces": ws} // keep who else gets it
				} else {
					att[name] = map[string]any{"enabled": true, "agents": []string{}, "workspaces": ws}
				}
				addedRows = append(addedRows, added{Name: name, Source: sid})
			}
			for _, name := range slices.Sorted(maps.Keys(reuse)) {
				sid := reuse[name]
				src, ok := cur.Skills.Sources[sid]
				if !ok {
					skip(name, "Source "+sid+" was removed meanwhile; try again")
					continue
				}
				patch, _ := sources[sid].(map[string]any)
				if patch == nil {
					patch = map[string]any{}
					sources[sid] = patch
				}
				attach(sid, name, src, patch)
			}
			for _, key := range slices.Sorted(maps.Keys(groups)) {
				g, s := groups[key], scans[key]
				repo := cmp.Or(s.Index, g.repo)
				ref := g.ref
				if g.kind == config.KindWellKnown {
					ref = ""
				}
				if other := sourceTracking(cur, repo, ref, g.path); other != "" {
					for _, name := range g.names {
						skip(name, "Source "+other+" was added meanwhile; try again")
					}
					continue
				}
				sid := newSourceID(func(id string) bool { return taken[id] }, g.kind, repo, ref)
				taken[sid] = true
				patch := map[string]any{"repo": repo, "ref": ref, "path": g.path, "commit": s.Commit, "found": foundOf(s.Skills)}
				if g.kind != config.KindGit {
					patch["kind"] = g.kind
				}
				before := len(addedRows)
				for _, name := range g.names {
					attach(sid, name, config.Source{}, patch)
				}
				if len(addedRows) > before {
					sources[sid] = patch
					newSources = append(newSources, sid)
				}
			}
			if len(sources) == 0 {
				return nil, nil
			}
			return map[string]any{"skills": map[string]any{"sources": sources}}, nil
		})
		if err != nil {
			fmt.Fprintf(out, "error: %v\n", err)
			return 1, fieldFailure(err), nil
		}
		for _, s := range late {
			skip(s.Name, s.Reason)
		}
		for _, a := range addedRows {
			fmt.Fprintf(out, "add       %s from %s\n", a.Name, a.Source)
		}
		res := map[string]any{"added": addedRows, "sources": newSources, "skipped": skipped,
			"changed": changed, "revision": next.Revision}
		raw, err := json.Marshal(res)
		return 0, raw, err
	})
}
