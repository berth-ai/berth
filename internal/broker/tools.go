package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/berth-ai/berth/internal/tools"
)

// toolsGroup serializes jobs that edit the user track.
const toolsGroup = "tools"

const (
	toolJobTimeout = 30 * time.Minute
	maxVersions    = 500
)

func (b *Broker) toolsList(ctx context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	warnings := []string{}
	base, err := tools.LoadBase(b.BaseTools)
	if err != nil {
		warnings = append(warnings, "base track: "+err.Error())
	}
	presets, err := tools.LoadPresets(b.PresetsDir)
	if err != nil {
		warnings = append(warnings, "presets: "+err.Error())
	}
	// The user track lives in agent's home: read it as agent, through berth
	// status, never as root.
	user := []tools.Tool{}
	stdout, stderr, exit, err := b.agentRun(ctx, b.syncTimeout(), b.Home.Berth, "status", "--json")
	var rep struct {
		UserTools []tools.Tool `json:"user_tools"`
		Warnings  []string     `json:"warnings"`
	}
	switch {
	case err != nil:
		warnings = append(warnings, "user track: "+err.Error())
	case exit != 0 || json.Unmarshal([]byte(stdout), &rep) != nil:
		warnings = append(warnings, "user track: "+strings.TrimSpace(stderr))
	default:
		user = rep.UserTools
		for _, w := range rep.Warnings {
			if strings.HasPrefix(w, "user tools:") {
				warnings = append(warnings, w)
			}
		}
	}
	for i := range presets {
		presets[i].State = tools.PresetState(presets[i], user)
	}
	if base == nil {
		base = []tools.Tool{}
	}
	return http.StatusOK, map[string]any{"base": base, "user": user, "presets": presets, "warnings": warnings}
}

func (b *Broker) toolsVersions(ctx context.Context, body []byte) (int, any) {
	var in struct {
		Tool string `json:"tool"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if !tools.ValidTool(in.Tool) {
		return badRequest(fmt.Errorf("invalid tool %q", in.Tool))
	}
	stdout, stderr, exit, err := b.agentRun(ctx, b.syncTimeout(), b.Home.Mise, "ls-remote", in.Tool)
	if code, v, failed := syncFailure(err, exit, stderr); failed {
		return code, v
	}
	versions := strings.Fields(stdout)
	if len(versions) > maxVersions {
		versions = versions[len(versions)-maxVersions:]
	}
	return http.StatusOK, map[string]any{"tool": in.Tool, "versions": versions}
}

func (b *Broker) toolsRegistry(ctx context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	stdout, stderr, exit, err := b.agentRun(ctx, b.syncTimeout(), b.Home.Mise, "registry")
	if code, v, failed := syncFailure(err, exit, stderr); failed {
		return code, v
	}
	names := []string{}
	for _, line := range strings.Split(stdout, "\n") {
		if f := strings.Fields(line); len(f) > 0 && tools.ValidTool(f[0]) {
			names = append(names, f[0])
		}
	}
	return http.StatusOK, map[string]any{"tools": names}
}

// syncFailure maps a failed synchronous agent command to a response.
func syncFailure(err error, exit int, stderr string) (int, any, bool) {
	switch {
	case errors.Is(err, errTimeout):
		code, v := fail(http.StatusGatewayTimeout, "timeout", "the command did not answer in time; try again")
		return code, v, true
	case err != nil:
		code, v := fail(http.StatusBadGateway, "exec_failed", "%v", err)
		return code, v, true
	case exit != 0:
		code, v := fail(http.StatusBadGateway, "command_failed", "%s", lastLine(stderr))
		return code, v, true
	}
	return 0, nil, false
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func (b *Broker) toolsAdd(_ context.Context, body []byte) (int, any) {
	var in struct {
		Tool    string `json:"tool"`
		Version string `json:"version"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if !tools.ValidTool(in.Tool) || !tools.ValidVersion(in.Version) {
		return badRequest(fmt.Errorf("invalid tool or version %q@%q", in.Tool, in.Version))
	}
	return b.homeJob("tools.add", toolsGroup, b.Home.Mise, []string{"use", "-g", in.Tool + "@" + in.Version}, toolJobTimeout)
}

func (b *Broker) toolsRemove(_ context.Context, body []byte) (int, any) {
	var in struct {
		Tool string `json:"tool"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if !tools.ValidTool(in.Tool) {
		return badRequest(fmt.Errorf("invalid tool %q", in.Tool))
	}
	return b.homeJob("tools.remove", toolsGroup, b.Home.Mise, []string{"unuse", "-g", in.Tool}, toolJobTimeout)
}

func (b *Broker) presetInstall(_ context.Context, body []byte) (int, any) {
	var in struct {
		Preset string `json:"preset"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if !tools.ValidPresetID(in.Preset) {
		return badRequest(fmt.Errorf("invalid preset %q", in.Preset))
	}
	presets, err := tools.LoadPresets(b.PresetsDir)
	if err != nil {
		return fail(http.StatusInternalServerError, "internal", "%v", err)
	}
	i := slices.IndexFunc(presets, func(p tools.Preset) bool { return p.ID == in.Preset })
	if i < 0 {
		return fail(http.StatusNotFound, "unknown_preset", "no preset named %q", in.Preset)
	}
	args := []string{"use", "-g"}
	for _, t := range slices.Sorted(maps.Keys(presets[i].Tools)) {
		args = append(args, t+"@"+presets[i].Tools[t])
	}
	return b.homeJob("preset.install", toolsGroup, b.Home.Mise, args, toolJobTimeout)
}

// homeJob starts an agent-side job; its Args show the command for the log.
func (b *Broker) homeJob(verb, group, path string, args []string, timeout time.Duration) (int, any) {
	return b.jobAccepted(verb, group, append([]string{path}, args...), b.homeWork(path, args, timeout))
}

// jobAccepted starts work as a job and answers the verb with it.
func (b *Broker) jobAccepted(verb, group string, shown []string, work Work) (int, any) {
	job, err := b.Jobs.Start(verb, group, shown, work)
	if errors.Is(err, ErrJobRunning) {
		return fail(http.StatusConflict, "job_running", "%v", err)
	}
	if err != nil {
		return fail(http.StatusInternalServerError, "internal", "%v", err)
	}
	return http.StatusAccepted, map[string]any{"job": job}
}
