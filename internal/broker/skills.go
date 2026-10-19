package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/runner"
	"github.com/berth-ai/berth/internal/skills"
)

// skillsSyncGroup serializes skills.sync runs; the skills.sources verbs
// use skillsGroup, so a scan can run during a sync.
const skillsSyncGroup = "skills-sync"

func sameSkills(a, b config.Skills) bool { return reflect.DeepEqual(a, b) }

// writeSkillsSettings publishes the sources to SkillsSettings, readable by
// the agent group.
func (b *Broker) writeSkillsSettings(s config.Skills) error {
	if b.SkillsSettings == "" {
		return nil
	}
	uid, gid := -1, -1
	if b.Agent != nil {
		uid, gid = 0, int(b.Agent.GID)
	}
	return config.WriteSkillsFile(b.SkillsSettings, s.Sources, uid, gid)
}

// skillsStale reports whether SkillsSettings differs from s.
func (b *Broker) skillsStale(s config.Skills) bool {
	if b.SkillsSettings == "" {
		return false
	}
	pub, err := config.ReadSkillsFile(b.SkillsSettings)
	return err != nil || !reflect.DeepEqual(pub, s.Sources)
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
	// skillsGroup serializes the skills.sources verbs.
	skillsGroup      = "skills"
	skillScanTimeout = 5 * time.Minute
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

// scanRow and skillScan are berth skills scan's answer.
type scanRow struct {
	Path        string `json:"path"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
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

// skillsVendorResolve settles a vendored skill that local edits stopped,
// through berth skills vendor as agent. Overwrite then syncs, which
// replaces or removes the copy; release then drops the workspace from the
// attachment, leaving the files as the user's.
func (b *Broker) skillsVendorResolve(_ context.Context, body []byte) (int, any) {
	var in struct {
		Source    string `json:"source"`
		Name      string `json:"name"`
		Workspace string `json:"workspace"`
		Action    string `json:"action"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	switch {
	case in.Action != "overwrite" && in.Action != "release":
		return rejectJob(&fieldError{Code: "invalid_request", Field: "action", Message: "The action must be overwrite or release"})
	case !config.WorkspaceIDPattern.MatchString(in.Source) || !config.SkillIDPattern.MatchString(in.Name):
		return rejectJob(&fieldError{Code: "invalid_request", Field: "name", Message: "Name a source and one of its skills"})
	}
	if cur, _, _ := b.snapshot(); cur.Workspaces[in.Workspace].Path == "" {
		return fail(http.StatusNotFound, "unknown_workspace", "No workspace named %s", in.Workspace)
	}
	args := []string{"skills", "vendor", "--json", "--workspace", in.Workspace, "--key", in.Source + "/" + in.Name, "--action", in.Action}
	return b.cliJob("skills.vendor.resolve", skillsGroup, args, b.syncTimeout(), func(context.Context, io.Writer, []byte) (json.RawMessage, error) {
		res := map[string]any{"action": in.Action, "workspace": in.Workspace, "name": in.Name, "changed": false}
		if in.Action == "overwrite" {
			b.startSkillsSync()
			return json.Marshal(res)
		}
		next, changed, err := b.applyPatch(func(cur config.Config) (map[string]any, error) {
			a, ok := cur.Skills.Sources[in.Source].Attached[in.Name]
			if _, on := a.Workspaces[in.Workspace]; !ok || !on {
				return nil, nil
			}
			var entry any = map[string]any{"workspaces": map[string]any{in.Workspace: nil}}
			if a.Agents != nil && len(a.Agents) == 0 && len(a.Workspaces) == 1 {
				entry = nil // it reached nothing else
			}
			return map[string]any{"skills": map[string]any{"sources": map[string]any{
				in.Source: map[string]any{"attached": map[string]any{in.Name: entry}}}}}, nil
		})
		if err != nil {
			return nil, err
		}
		res["changed"], res["revision"] = changed, next.Revision
		return json.Marshal(res)
	})
}
