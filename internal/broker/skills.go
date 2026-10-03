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
	"time"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/runner"
	"github.com/berth-ai/berth/internal/skills"
)

// skillsSyncGroup serializes skills.sync runs; skills.add and
// skills.update use skillsGroup, so a resolve can run during a sync.
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
	// skillsGroup serializes skills.add and skills.update.
	skillsGroup         = "skills"
	skillResolveTimeout = 5 * time.Minute
)

// skillError is a failure the panel shows next to a field of its dialog.
type skillError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func (e *skillError) Error() string { return e.Message }

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

func (b *Broker) skillsAdd(_ context.Context, body []byte) (int, any) {
	var in struct {
		Repo   string   `json:"repo"`
		Path   string   `json:"path"`
		Ref    string   `json:"ref"`
		Agents []string `json:"agents"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if msg := config.CheckSkillRepo(in.Repo); msg != "" {
		return fieldInvalid("repo", "The repository URL "+msg)
	}
	if !config.ValidSkillPath(in.Path) {
		return fieldInvalid("path", "The path must be a folder inside the repository, such as skills/pdf")
	}
	if !config.ValidGitRef(in.Ref) {
		return fieldInvalid("ref", "The ref must be a branch, tag or commit")
	}
	if in.Agents != nil && len(in.Agents) == 0 {
		return fieldInvalid("agents", "Choose at least one agent")
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	return b.resolveJob("skills.add", in.Repo, in.Path, in.Ref, func(res skillResolved) (json.RawMessage, error) {
		return b.applySkill(res.Name, func(cur config.Config) (any, error) {
			if _, ok := cur.Skills[res.Name]; ok {
				return nil, &skillError{Code: "skill_exists", Field: "path",
					Message: fmt.Sprintf("A skill named %s is already installed", res.Name)}
			}
			return config.Skill{Repo: in.Repo, Path: in.Path, Ref: in.Ref, Commit: res.Commit,
				Description: res.Description, Agents: in.Agents, Enabled: true}, nil
		})
	})
}

func (b *Broker) skillsUpdate(_ context.Context, body []byte) (int, any) {
	var in struct {
		ID  string `json:"id"`
		Ref string `json:"ref"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	cur, _, _ := b.snapshot()
	s, ok := cur.Skills[in.ID]
	if !ok {
		return fail(http.StatusNotFound, "unknown_skill", "no skill named %q", in.ID)
	}
	ref := cmp.Or(in.Ref, s.Ref)
	if !config.ValidGitRef(ref) {
		return fieldInvalid("ref", "The ref must be a branch, tag or commit")
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	return b.resolveJob("skills.update", s.Repo, s.Path, ref, func(res skillResolved) (json.RawMessage, error) {
		return b.applySkill(in.ID, func(cur config.Config) (any, error) {
			now, ok := cur.Skills[in.ID]
			switch {
			case !ok:
				return nil, &skillError{Code: "unknown_skill", Message: fmt.Sprintf("The skill %s was removed", in.ID)}
			case res.Name != in.ID:
				return nil, &skillError{Code: "name_changed", Field: "ref",
					Message: fmt.Sprintf("That ref holds skill %s, not %s", res.Name, in.ID)}
			case now.Ref == ref && now.Commit == res.Commit && now.Description == res.Description:
				return nil, nil
			}
			return map[string]any{"ref": ref, "commit": res.Commit, "description": res.Description}, nil
		})
	})
}

// skillResolved is berth skills resolve's answer.
type skillResolved struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Commit      string `json:"commit"`
}

// resolveJob runs berth skills resolve as agent, then save with its answer.
// The job fails when either step does; its result then carries the field.
func (b *Broker) resolveJob(verb, repo, path, ref string, save func(skillResolved) (json.RawMessage, error)) (int, any) {
	args := []string{"skills", "resolve", "--json", "--repo", repo, "--ref", ref}
	if path != "" {
		args = append(args, "--path", path)
	}
	shown := append([]string{b.Home.Berth}, args...)
	job, err := b.Jobs.Start(verb, skillsGroup, shown, func(ctx context.Context, out io.Writer) (int, json.RawMessage, error) {
		var stdout bytes.Buffer
		r := runner.Exec{Agent: b.Agent, Stdout: &stdout, Stderr: out}
		err := r.Run(ctx, runner.Cmd{Path: b.Home.Berth, Args: args, AsAgent: true, Timeout: skillResolveTimeout})
		var ee *exec.ExitError
		switch {
		case errors.As(err, &ee):
			return ee.ExitCode(), resolveFailure(stdout.Bytes()), nil
		case err != nil:
			return -1, nil, err
		}
		var res skillResolved
		if err := json.Unmarshal(stdout.Bytes(), &res); err != nil || res.Name == "" {
			return 1, errorJSON("bad_output", "berth skills resolve printed no result"), nil
		}
		result, err := save(res)
		if err != nil {
			fmt.Fprintf(out, "error: %v\n", err)
			return 1, skillFailure(err), nil
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

// resolveFailure turns resolve's --json error, whose step is the field,
// into the job result.
func resolveFailure(stdout []byte) json.RawMessage {
	var doc struct {
		Error struct{ Code, Step, Message string } `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(stdout), &doc) != nil || doc.Error.Code == "" {
		return errorJSON("resolve_failed", "berth skills resolve failed; see the log")
	}
	return skillFailure(&skillError{Code: doc.Error.Code, Message: doc.Error.Message, Field: doc.Error.Step})
}

func skillFailure(err error) json.RawMessage {
	var se *skillError
	if !errors.As(err, &se) {
		se = &skillError{Code: "apply_failed", Message: err.Error()}
	}
	raw, _ := json.Marshal(map[string]any{"error": se})
	return raw
}

// applySkill patches skills.<id> with build's entry on top of the current
// revision, under the change lock, and refreshes like config.apply. A nil
// entry means nothing changed.
func (b *Broker) applySkill(id string, build func(cur config.Config) (any, error)) (json.RawMessage, error) {
	b.change.Lock()
	defer b.change.Unlock()
	if _, _, ok := b.normalMode(); !ok {
		return nil, &skillError{Code: "not_ready", Message: "Berth is in setup or recovery mode; finish setup or fix the config first"}
	}
	cur, _, _ := b.snapshot()
	entry, err := build(cur)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return json.Marshal(map[string]any{"skill": id, "changed": false, "revision": cur.Revision})
	}
	patch, err := json.Marshal(map[string]any{"skills": map[string]any{id: entry}})
	if err != nil {
		return nil, err
	}
	old, next, err := b.Store.Apply(patch, cur.Revision)
	if err != nil {
		return nil, err
	}
	b.changed(old, next, nil)
	b.refreshAgents(old, next, false)
	return json.Marshal(map[string]any{"skill": id, "changed": true, "revision": next.Revision})
}
