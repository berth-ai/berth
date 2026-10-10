package broker

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/runner"
	"github.com/berth-ai/berth/internal/skills"
)

// skillsSyncGroup serializes skills.sync runs; skills.scan, skills.add and
// skills.update use skillsGroup, so a scan can run during a sync.
const skillsSyncGroup = "skills-sync"

func sameSkills(a, b map[string]config.Skill) bool {
	return maps.EqualFunc(a, b, func(x, y config.Skill) bool {
		return x.Repo == y.Repo && x.Path == y.Path && x.Ref == y.Ref && x.Commit == y.Commit &&
			x.Description == y.Description && x.Enabled == y.Enabled &&
			(x.Agents == nil) == (y.Agents == nil) && slices.Equal(x.Agents, y.Agents)
	})
}

// writeSkillsSettings publishes s to SkillsSettings, readable by the agent group.
func (b *Broker) writeSkillsSettings(s map[string]config.Skill) error {
	if b.SkillsSettings == "" {
		return nil
	}
	uid, gid := -1, -1
	if b.Agent != nil {
		uid, gid = 0, int(b.Agent.GID)
	}
	return config.WriteSkillsFile(b.SkillsSettings, s, uid, gid)
}

// skillsStale reports whether SkillsSettings differs from s.
func (b *Broker) skillsStale(s map[string]config.Skill) bool {
	if b.SkillsSettings == "" {
		return false
	}
	pub, err := config.ReadSkillsFile(b.SkillsSettings)
	return err != nil || !sameSkills(pub, s)
}

// startSkillsSync runs the skills converger as a job.
func (b *Broker) startSkillsSync() {
	if b.Layer == nil || len(b.SkillsSync) == 0 {
		return
	}
	work := b.agentWork(b.SkillsSync[0], b.SkillsSync[1:], skills.SyncTimeout)
	if err := b.skillsRuns.start(b.Jobs, "skills.sync", skillsSyncGroup, work, "Skills changed during the run; running again."); err != nil {
		b.logf("WARNING: skills sync not started: %v", err)
	}
}

const (
	// skillsGroup serializes skills.scan, skills.add and skills.update.
	skillsGroup      = "skills"
	skillScanTimeout = 5 * time.Minute
	// maxSkillBatch caps the skills one add or update touches.
	maxSkillBatch = skills.MaxScan
)

const skillPathMsg = "The path must be a folder inside the repository, such as skills/pdf"

// fieldError is a failure the panel shows next to a field of its dialog.
type fieldError struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Field   string   `json:"field,omitempty"`
	Left    []string `json:"left,omitempty"` // what a folder delete could not remove
}

func (e *fieldError) Error() string { return e.Message }

func fieldInvalid(field, msg string) (int, any) {
	return validationResponse(&config.ValidationError{Fields: []config.FieldError{{Field: field, Message: msg}}})
}

// normalMode answers setup and recovery the way config.apply does.
func (b *Broker) normalMode() (int, any, bool) {
	switch _, mode, _ := b.snapshot(); mode {
	case ModeSetup:
		code, v := fail(http.StatusConflict, "setup_required", "no login exists yet; finish setup first")
		return code, v, false
	case ModeRecovery:
		code, v := fail(http.StatusConflict, "recovery", "the config is unusable; revert it or reset it to the defaults first")
		return code, v, false
	}
	return 0, nil, true
}

// skillSourceError checks a repository and ref; field is "" when both are fine.
func skillSourceError(repo, ref string) (field, msg string) {
	if msg := config.CheckSkillRepo(repo); msg != "" {
		return "repo", "The repository URL " + msg
	}
	if !config.ValidGitRef(ref) {
		return "ref", "The ref must be a branch, tag or commit"
	}
	return "", ""
}

// pathsError checks skills.add's paths: 1 to maxSkillBatch unique folders.
func pathsError(paths []string) string {
	if len(paths) == 0 || len(paths) > maxSkillBatch {
		return fmt.Sprintf("Choose 1 to %d skills", maxSkillBatch)
	}
	seen := map[string]bool{}
	for _, p := range paths {
		switch {
		case !config.ValidSkillPath(p):
			return skillPathMsg
		case seen[p]:
			return "Choose each skill once"
		}
		seen[p] = true
	}
	return ""
}

func rowsByPath(rows []scanRow) map[string]scanRow {
	m := make(map[string]scanRow, len(rows))
	for _, r := range rows {
		m[r.Path] = r
	}
	return m
}

func (b *Broker) skillsScan(_ context.Context, body []byte) (int, any) {
	var in struct {
		Repo string `json:"repo"`
		Ref  string `json:"ref"`
		Path string `json:"path"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if field, msg := skillSourceError(in.Repo, in.Ref); field != "" {
		return fieldInvalid(field, msg)
	}
	if !config.ValidSkillPath(in.Path) {
		return fieldInvalid("path", skillPathMsg)
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	return b.scanJob("skills.scan", in.Repo, in.Ref, []string{in.Path}, func(s skillScan) (json.RawMessage, error) {
		cur, _, _ := b.snapshot()
		for i, r := range s.Skills {
			_, ok := cur.Skills[r.Name]
			s.Skills[i].Installed = ok && r.Name != ""
		}
		return json.Marshal(s)
	})
}

func (b *Broker) skillsAdd(_ context.Context, body []byte) (int, any) {
	var in struct {
		Repo   string   `json:"repo"`
		Ref    string   `json:"ref"`
		Commit string   `json:"commit"`
		Paths  []string `json:"paths"`
		Agents []string `json:"agents"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if field, msg := skillSourceError(in.Repo, in.Ref); field != "" {
		return fieldInvalid(field, msg)
	}
	if !config.ValidCommit(in.Commit) {
		return fieldInvalid("commit", "Find the skills again; the scanned commit is missing")
	}
	if msg := pathsError(in.Paths); msg != "" {
		return fieldInvalid("paths", msg)
	}
	if in.Agents != nil && len(in.Agents) == 0 {
		return fieldInvalid("agents", "Choose at least one agent")
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	return b.scanJob("skills.add", in.Repo, in.Commit, in.Paths, func(s skillScan) (json.RawMessage, error) {
		byPath := rowsByPath(s.Skills)
		entries := make(map[string]any, len(in.Paths))
		for _, p := range in.Paths {
			row, ok := byPath[p]
			switch {
			case !ok:
				return nil, &fieldError{Code: "no_skill", Field: "path",
					Message: fmt.Sprintf("No SKILL.md at %s in that commit", cmp.Or(p, "the repository root"))}
			case row.Error != "":
				return nil, &fieldError{Code: "bad_skill", Field: "path",
					Message: fmt.Sprintf("%s: %s", cmp.Or(p, "The repository root"), row.Error)}
			}
			entries[row.Name] = config.Skill{Repo: in.Repo, Path: p, Ref: in.Ref, Commit: s.Commit,
				Description: row.Description, Agents: in.Agents, Enabled: true}
		}
		return b.applySkills(func(cur config.Config) (map[string]any, error) {
			for _, id := range slices.Sorted(maps.Keys(entries)) {
				if _, ok := cur.Skills[id]; ok {
					return nil, &fieldError{Code: "skill_exists", Field: "path",
						Message: fmt.Sprintf("A skill named %s is already installed", id)}
				}
			}
			return entries, nil
		})
	})
}

func (b *Broker) skillsUpdate(_ context.Context, body []byte) (int, any) {
	var in struct {
		IDs []string `json:"ids"`
		Ref string   `json:"ref"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if len(in.IDs) == 0 || len(in.IDs) > maxSkillBatch {
		return fieldInvalid("ids", fmt.Sprintf("Choose 1 to %d skills", maxSkillBatch))
	}
	cur, _, _ := b.snapshot()
	want := make(map[string]config.Skill, len(in.IDs))
	for _, id := range in.IDs {
		s, ok := cur.Skills[id]
		_, dup := want[id]
		switch {
		case !ok:
			return fieldInvalid("ids", fmt.Sprintf("No skill named %s is installed", id))
		case dup:
			return fieldInvalid("ids", "List each skill once")
		case s.Repo != cur.Skills[in.IDs[0]].Repo:
			return fieldInvalid("ids", "These skills come from different repositories; update one repository at a time")
		}
		want[id] = s
	}
	first := want[in.IDs[0]]
	ref := cmp.Or(in.Ref, first.Ref)
	if !config.ValidGitRef(ref) {
		return fieldInvalid("ref", "The ref must be a branch, tag or commit")
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	ids := slices.Sorted(maps.Keys(want))
	var paths []string
	for _, id := range ids {
		paths = append(paths, want[id].Path)
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	return b.scanJob("skills.update", first.Repo, ref, paths, func(s skillScan) (json.RawMessage, error) {
		byPath := rowsByPath(s.Skills)
		for _, id := range ids {
			p := want[id].Path
			row, ok := byPath[p]
			at := cmp.Or(p, "the repository root")
			switch {
			case !ok:
				return nil, &fieldError{Code: "no_skill", Field: "ref",
					Message: fmt.Sprintf("%s is no longer at %s in %s; remove it or update the others alone", id, at, ref)}
			case row.Error != "":
				return nil, &fieldError{Code: "bad_skill", Field: "ref", Message: fmt.Sprintf("%s at %s in %s: %s", id, at, ref, row.Error)}
			case row.Name != id:
				return nil, &fieldError{Code: "name_changed", Field: "ref",
					Message: fmt.Sprintf("%s in %s holds skill %s, not %s", at, ref, row.Name, id)}
			}
		}
		return b.applySkills(func(cur config.Config) (map[string]any, error) {
			changes := map[string]any{}
			for _, id := range ids {
				now, ok := cur.Skills[id]
				if !ok || now.Repo != want[id].Repo || now.Path != want[id].Path {
					return nil, &fieldError{Code: "unknown_skill",
						Message: fmt.Sprintf("The skill %s was removed or changed meanwhile; try again", id)}
				}
				row := byPath[now.Path]
				if now.Ref == ref && now.Commit == s.Commit && now.Description == row.Description {
					continue
				}
				changes[id] = map[string]any{"ref": ref, "commit": s.Commit, "description": row.Description}
			}
			return changes, nil
		})
	})
}

// scanRow and skillScan are berth skills scan's answer; skills.scan adds
// installed.
type scanRow struct {
	Path        string `json:"path"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
	Installed   bool   `json:"installed,omitempty"`
}

type skillScan struct {
	Commit string    `json:"commit"`
	Skills []scanRow `json:"skills"`
}

// scanJob runs berth skills scan as agent over paths (the whole tree when
// one is the root), then save with its answer.
func (b *Broker) scanJob(verb, repo, ref string, paths []string, save func(skillScan) (json.RawMessage, error)) (int, any) {
	args := []string{"skills", "scan", "--json", "--repo", repo, "--ref", ref}
	if !slices.Contains(paths, "") {
		for _, p := range paths {
			args = append(args, "--path", p)
		}
	}
	return b.cliJob(verb, skillsGroup, args, skillScanTimeout, func(_ context.Context, _ io.Writer, stdout []byte) (json.RawMessage, error) {
		var res skillScan
		if err := json.Unmarshal(stdout, &res); err != nil || res.Commit == "" {
			return nil, &fieldError{Code: "bad_output", Message: "berth skills scan printed no result"}
		}
		return save(res)
	})
}

// cliJob runs berth args as agent as job verb in group, then save with its
// stdout. The job fails when either step does; its result then carries
// the field.
func (b *Broker) cliJob(verb, group string, args []string, timeout time.Duration,
	save func(ctx context.Context, log io.Writer, stdout []byte) (json.RawMessage, error)) (int, any) {
	shown := append([]string{b.Home.Berth}, args...)
	job, err := b.Jobs.Start(verb, group, shown, func(ctx context.Context, out io.Writer) (int, json.RawMessage, error) {
		stdout, code, err := b.runCLI(ctx, out, args, timeout)
		switch {
		case err != nil:
			return -1, nil, err
		case code != 0:
			return code, fieldFailure(cliError(stdout, args)), nil
		}
		result, err := save(ctx, out, stdout)
		if err != nil {
			fmt.Fprintf(out, "error: %v\n", err)
			return 1, fieldFailure(err), nil
		}
		return 0, result, nil
	})
	if errors.Is(err, ErrJobRunning) {
		return fail(http.StatusConflict, "job_running", "%v", err)
	}
	if err != nil {
		return fail(http.StatusInternalServerError, "internal", "%v", err)
	}
	return http.StatusAccepted, map[string]any{"job": job}
}

// runCLI runs berth args as agent with its stderr in log, and returns its
// stdout and exit code; err means it could not run or timed out.
func (b *Broker) runCLI(ctx context.Context, log io.Writer, args []string, timeout time.Duration) ([]byte, int, error) {
	var stdout bytes.Buffer
	r := runner.Exec{Agent: b.Agent, Stdout: &stdout, Stderr: log}
	err := r.Run(ctx, runner.Cmd{Path: b.Home.Berth, Args: args, AsAgent: true, Timeout: timeout})
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		return stdout.Bytes(), ee.ExitCode(), nil
	case err != nil:
		return nil, -1, err
	}
	return bytes.TrimSpace(stdout.Bytes()), 0, nil
}

// cliError turns berth's --json error, whose step names the field, into a
// fieldError.
func cliError(stdout []byte, args []string) *fieldError {
	var doc struct {
		Error struct{ Code, Step, Message string } `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(stdout), &doc) != nil || doc.Error.Code == "" {
		name := strings.Join(args[:min(2, len(args))], " ")
		return &fieldError{Code: args[min(1, len(args)-1)] + "_failed", Message: "berth " + name + " failed; see the log"}
	}
	return &fieldError{Code: doc.Error.Code, Message: doc.Error.Message, Field: doc.Error.Step}
}

func fieldFailure(err error) json.RawMessage {
	var se *fieldError
	if !errors.As(err, &se) {
		se = &fieldError{Code: "apply_failed", Message: err.Error()}
	}
	raw, _ := json.Marshal(map[string]any{"error": se})
	return raw
}

// applySkills patches the entries build returns, keyed by skill id, and
// lists the ids it patched; no entries means nothing changed.
func (b *Broker) applySkills(build func(cur config.Config) (map[string]any, error)) (json.RawMessage, error) {
	var ids []string
	next, changed, err := b.applyPatch(func(cur config.Config) (map[string]any, error) {
		entries, err := build(cur)
		if err != nil || len(entries) == 0 {
			return nil, err
		}
		ids = slices.Sorted(maps.Keys(entries))
		return map[string]any{"skills": entries}, nil
	})
	if err != nil {
		return nil, err
	}
	if !changed {
		ids = []string{}
	}
	return json.Marshal(map[string]any{"skills": ids, "changed": changed, "revision": next.Revision})
}
