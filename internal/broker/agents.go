package broker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"os/exec"
	"slices"
	"time"

	"github.com/berth-ai/bert/internal/agentlayer"
	"github.com/berth-ai/bert/internal/config"
	"github.com/berth-ai/bert/internal/runner"
)

// mcpGroup serializes MCP registration runs.
const mcpGroup = "mcp"

// agentWork runs path as the agent user, with its output as the job log.
func (b *Broker) agentWork(path string, args []string, timeout time.Duration) Work {
	return func(ctx context.Context, out io.Writer) (int, json.RawMessage, error) {
		r := runner.Exec{Agent: b.Agent, Stdout: out, Stderr: out}
		err := r.Run(ctx, runner.Cmd{Path: path, Args: args, AsAgent: true, Timeout: timeout})
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil, nil
		}
		if err != nil {
			return -1, nil, err
		}
		return 0, nil, nil
	}
}

// checkSecrets validates a secrets change against the current set's key names.
func (b *Broker) checkSecrets(secrets map[string]*string) error {
	if len(secrets) == 0 {
		return nil
	}
	var known []string
	if b.Layer != nil {
		var err error
		if known, err = b.Layer.KeyNames(); err != nil {
			return err
		}
	}
	var fields []config.FieldError
	for _, name := range slices.Sorted(maps.Keys(secrets)) {
		field := "secrets." + name
		switch v := secrets[name]; {
		case !slices.Contains(known, name):
			fields = append(fields, config.FieldError{Field: field, Message: "no enabled agent set declares this key"})
		case v != nil && *v != "":
			if err := agentlayer.ValidKeyValue(*v); err != nil {
				fields = append(fields, config.FieldError{Field: field, Message: err.Error()})
			}
		}
	}
	if fields != nil {
		return &config.ValidationError{Fields: fields}
	}
	return nil
}

// writeSecrets applies a validated secrets change to agents.env.
func (b *Broker) writeSecrets(secrets map[string]*string) error {
	keys, err := agentlayer.ReadKeys(b.Layer.KeysPath)
	if err != nil {
		return err
	}
	for name, v := range secrets {
		if v == nil || *v == "" {
			delete(keys, name)
		} else {
			keys[name] = *v
		}
	}
	gid := -1
	if b.Agent != nil {
		gid = int(b.Agent.GID)
	}
	uid := -1
	if gid != -1 {
		uid = 0
	}
	if err := agentlayer.WriteKeys(b.Layer.KeysPath, keys, uid, gid); err != nil {
		return err
	}
	b.logf("API keys changed: %v", slices.Sorted(maps.Keys(secrets)))
	return nil
}

// refreshAgents rebuilds the layer and re-registers MCP servers when the
// enabled set changed between old and next.
func (b *Broker) refreshAgents(old, next config.Config) {
	if b.Layer == nil || slices.Equal(old.Agents.Disabled, next.Agents.Disabled) {
		return
	}
	if _, err := b.Layer.Build(next.Agents.Disabled); err != nil {
		b.logf("WARNING: agent layer: %v", err)
		return
	}
	if b.MCPSetup == "" {
		return
	}
	timeout := time.Duration(next.Agents.MCPTimeout) * time.Second
	if _, err := b.Jobs.Start("mcp.setup", mcpGroup, nil, b.agentWork(b.MCPSetup, nil, timeout)); err != nil {
		b.logf("WARNING: MCP setup not started: %v", err)
	}
}

// secretStates reports which known keys have a value.
func (b *Broker) secretStates() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	if b.Layer == nil {
		return out
	}
	pkgs, err := b.Layer.Packages(nil)
	if err != nil {
		return out
	}
	for _, p := range pkgs {
		for _, k := range p.Keys {
			out[k.Name] = map[string]bool{"set": k.Set}
		}
	}
	return out
}

func validationResponse(err error) (int, any) {
	var ve *config.ValidationError
	if errors.As(err, &ve) {
		return http.StatusBadRequest, map[string]any{"error": map[string]any{
			"code": "invalid_config", "message": ve.Error(), "fields": ve.Fields}}
	}
	return fail(http.StatusInternalServerError, "internal", "%v", err)
}
