package broker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/instructions"
)

// instructionStatus is the answer status of an agent-side refusal.
var instructionStatus = map[string]int{
	"changed_on_disk": http.StatusConflict, "is_symlink": http.StatusConflict, "not_shared": http.StatusConflict,
	"agents_md_symlink": http.StatusConflict, "workspace_missing": http.StatusConflict,
	"too_large": http.StatusBadRequest, "not_text": http.StatusBadRequest, "not_allowed": http.StatusBadRequest,
	"not_a_file": http.StatusConflict, "unknown_workspace": http.StatusNotFound,
}

// instructionFiles lists the files the panel may read and write in
// workspace w: every agent's instruction file, enabled or not, and
// AGENTS.md while w shares instructions.
func (b *Broker) instructionFiles(cur config.Config, w config.Workspace) []string {
	var out []string
	if b.Layer != nil {
		if pkgs, err := b.Layer.Packages(cur.Agents.Disabled, cur.Agents.Added); err == nil {
			for _, p := range pkgs {
				if p.Kind == "agent" && p.Instructions != "" {
					out = append(out, p.Instructions)
				}
			}
		}
	}
	if w.Instructions.Shared {
		out = append(out, instructions.Shared)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

type instructionsRequest struct {
	ID         string  `json:"id"`
	File       string  `json:"file"`
	Content    *string `json:"content"`
	BaseSHA256 *string `json:"base_sha256"`
}

// instructionsCall checks a request and runs the agent-side command for
// action; it returns the parsed result, or the answer to give.
func (b *Broker) instructionsCall(ctx context.Context, body []byte, action string) (int, any) {
	var in instructionsRequest
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	cur, _, _ := b.snapshot()
	w, ok := cur.Workspaces[in.ID]
	switch {
	case !ok:
		return fail(http.StatusNotFound, "unknown_workspace", "No workspace named %s", in.ID)
	case !instructions.ValidName(in.File) || !slices.Contains(b.instructionFiles(cur, w), in.File):
		return fail(http.StatusBadRequest, "not_allowed", "%s is not an instruction file of this workspace's agents", in.File)
	case action == "write" && (in.Content == nil || in.BaseSHA256 == nil):
		return fail(http.StatusBadRequest, "invalid_request", "send content and base_sha256")
	case action == "write" && len(*in.Content) > instructions.MaxSize:
		return fail(http.StatusBadRequest, "too_large", "%s would be larger than 256 KiB", in.File)
	case action == "write" && !utf8.ValidString(*in.Content):
		return fail(http.StatusBadRequest, "not_text", "The content must be UTF-8 text")
	case action != "write" && (in.Content != nil || in.BaseSHA256 != nil):
		return badRequest(errors.New("content and base_sha256 go with write"))
	}
	args := []string{"workspaces", "instructions", "--json", "--action", action, "--id", in.ID, "--file", in.File}
	var stdin io.Reader // nil reads the null device
	if action == "write" {
		args = append(args, "--base-sha256="+*in.BaseSHA256)
		stdin = strings.NewReader(*in.Content)
	}
	stdout, stderr, code, err := b.agentRunInput(ctx, b.syncTimeout(), stdin, b.Home.Berth, args...)
	if err != nil {
		return fail(http.StatusInternalServerError, "instructions_failed", "%v", err)
	}
	if code != 0 {
		fe := cliError([]byte(stdout), args)
		if st, ok := instructionStatus[fe.Code]; ok {
			return fail(st, fe.Code, "%s", fe.Message)
		}
		return fail(http.StatusInternalServerError, "instructions_failed", "%s %s", fe.Message, strings.TrimSpace(stderr))
	}
	var res map[string]any
	if json.Unmarshal([]byte(stdout), &res) != nil {
		return fail(http.StatusInternalServerError, "bad_output", "berth workspaces instructions printed no result")
	}
	delete(res, "schema")
	return http.StatusOK, res
}

func (b *Broker) instructionsRead(ctx context.Context, body []byte) (int, any) {
	code, v := b.instructionsCall(ctx, body, "read")
	if code != http.StatusOK {
		return code, v
	}
	return code, map[string]any{"file": v}
}

func (b *Broker) instructionsWrite(ctx context.Context, body []byte) (int, any) {
	return b.instructionsCall(ctx, body, "write")
}

func (b *Broker) instructionsMerge(ctx context.Context, body []byte) (int, any) {
	code, v := b.instructionsCall(ctx, body, "merge")
	if code == http.StatusOK {
		b.startWorkspacesSync() // the converger re-reads what merge changed
	}
	return code, v
}
