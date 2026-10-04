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

	"github.com/berth-ai/berth/internal/agentlayer"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/runner"
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

// mcpTokens maps the token name of every bearer server in m to its id.
func mcpTokens(m config.MCP) map[string]string {
	out := map[string]string{}
	for id, s := range m.Servers {
		if s.Auth == config.AuthBearer {
			out[config.MCPTokenName(id)] = id
		}
	}
	return out
}

// checkSecrets validates a secrets change against the current set's key
// names and the bearer servers of next, the config the change lands on. It
// also refuses a bearer server, added or changed since cur, whose token
// name is a key of the set.
func (b *Broker) checkSecrets(secrets map[string]*string, cur, next config.Config) error {
	var known []string
	if b.Layer != nil {
		var err error
		if known, err = b.Layer.KeyNames(next.Agents.Added); err != nil {
			return err
		}
	}
	tokens := mcpTokens(next.MCP)
	var fields []config.FieldError
	for _, name := range slices.Sorted(maps.Keys(tokens)) {
		id := tokens[name]
		if old, ok := cur.MCP.Servers[id]; slices.Contains(known, name) && (!ok || old != next.MCP.Servers[id]) {
			fields = append(fields, config.FieldError{Field: "mcp.servers." + id,
				Message: "its token name " + name + " clashes with a key of the agent set"})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(secrets)) {
		field := "secrets." + name
		_, isToken := tokens[name]
		switch v := secrets[name]; {
		case !slices.Contains(known, name) && !isToken:
			fields = append(fields, config.FieldError{Field: field, Message: "no enabled agent set or bearer MCP server uses this key"})
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

// staleTokens lists, as deletions, the tokens old needed and next does not.
func staleTokens(old, next config.MCP) map[string]*string {
	keep, gone := mcpTokens(next), map[string]*string{}
	for name := range mcpTokens(old) {
		if _, ok := keep[name]; !ok {
			gone[name] = nil
		}
	}
	return gone
}

// touchesTokens reports whether secrets sets or clears an MCP token of old or next.
func touchesTokens(secrets map[string]*string, old, next config.MCP) bool {
	a, b := mcpTokens(old), mcpTokens(next)
	for name := range secrets {
		if _, ok := a[name]; ok {
			return true
		}
		if _, ok := b[name]; ok {
			return true
		}
	}
	return false
}

func sameMCP(a, b config.MCP) bool {
	return maps.Equal(a.Servers, b.Servers) && maps.EqualFunc(a.Available, b.Available, slices.Equal[[]string])
}

// writeMCPSettings publishes m to MCPSettings, readable by the agent group.
func (b *Broker) writeMCPSettings(m config.MCP) error {
	if b.MCPSettings == "" {
		return nil
	}
	uid, gid := -1, -1
	if b.Agent != nil {
		uid, gid = 0, int(b.Agent.GID)
	}
	return config.WriteMCPFile(b.MCPSettings, m, uid, gid)
}

// publishedStale reports whether MCPSettings differs from m. Leaving
// recovery mode needs it: the config before the change was the defaults,
// not what was published.
func (b *Broker) publishedStale(m config.MCP) bool {
	if b.MCPSettings == "" {
		return false
	}
	pub, err := config.ReadMCPFile(b.MCPSettings)
	return err != nil || !sameMCP(pub, m)
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

// refreshAgents publishes the agents, MCP and skills settings that changed.
// When agents.added changed it runs the converger, whose job then rebuilds
// the layer and re-runs MCP and skills sync for the new set. Otherwise it
// rebuilds the layer when the enabled set changed, re-registers MCP servers
// when the set, the MCP settings or an MCP token changed, and re-links
// skills when the set or the skills changed.
func (b *Broker) refreshAgents(old, next config.Config, tokens bool) {
	added := !slices.Equal(old.Agents.Added, next.Agents.Added) || b.agentsStale(next.Agents.Added)
	if added {
		if err := b.writeAgentsSettings(next.Agents.Added); err != nil {
			b.logf("WARNING: agent settings: %v", err)
		}
	}
	disabled := b.Layer != nil && !slices.Equal(old.Agents.Disabled, next.Agents.Disabled)
	settings := !sameMCP(old.MCP, next.MCP) || b.publishedStale(next.MCP)
	syncSkills := disabled
	if !sameSkills(old.Skills, next.Skills) || b.skillsStale(next.Skills) {
		if err := b.writeSkillsSettings(next.Skills); err != nil {
			b.logf("WARNING: skills settings: %v", err)
		} else {
			syncSkills = true
		}
	}
	if settings {
		if err := b.writeMCPSettings(next.MCP); err != nil {
			b.logf("WARNING: MCP settings: %v", err)
			return
		}
	}
	if added && b.Layer != nil && len(b.AgentsCmd) > 0 {
		b.startAgentsSync()
		return
	}
	if disabled {
		if _, err := b.Layer.Build(next.Agents.Disabled, next.Agents.Added); err != nil {
			b.logf("WARNING: agent layer: %v", err)
			return
		}
	}
	if syncSkills {
		b.startSkillsSync()
	}
	if b.Layer == nil || len(b.MCPSetup) == 0 || !disabled && !settings && !tokens {
		return
	}
	b.startMCPSetup(time.Duration(next.Agents.MCPTimeout) * time.Second)
}

// startMCPSetup runs the MCP converger as a job.
func (b *Broker) startMCPSetup(timeout time.Duration) {
	work := b.agentWork(b.MCPSetup[0], b.MCPSetup[1:], timeout)
	if err := b.mcpRuns.start(b.Jobs, "mcp.setup", mcpGroup, work, "MCP settings changed during the run; running again."); err != nil {
		b.logf("WARNING: MCP setup not started: %v", err)
	}
}

// secretStates reports which known keys and MCP tokens have a value.
func (b *Broker) secretStates(cfg config.Config) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	if b.Layer == nil {
		return out
	}
	if pkgs, err := b.Layer.Packages(nil, cfg.Agents.Added); err == nil {
		for _, p := range pkgs {
			for _, k := range p.Keys {
				out[k.Name] = map[string]bool{"set": k.Set}
			}
		}
	}
	if keys, err := agentlayer.ReadKeys(b.Layer.KeysPath); err == nil {
		for name := range mcpTokens(cfg.MCP) {
			out[name] = map[string]bool{"set": keys[name] != ""}
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
