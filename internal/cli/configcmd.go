package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/salgnt/berth/internal/broker"
	"github.com/salgnt/berth/internal/config"
	"github.com/salgnt/berth/internal/output"
)

const maxImportBytes = 64 << 10

func runConfig(ctx context.Context, a *app, args []string) error {
	fs := newFlagSet("config")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	a.p.JSON = *jsonOut
	if len(pos) == 0 {
		return usageErr(errors.New("config needs a subcommand: get, set, revert, reset, export or import"))
	}
	if !a.cfg.IsRoot {
		return output.Errorf(output.ExitNotRoot, "not_root", "",
			"berth config must run as root (docker exec -u root <container> berth config ...)")
	}
	c := broker.NewClient(a.cfg.BrokerSocket)
	sub, rest := pos[0], pos[1:]
	switch sub {
	case "get", "export", "revert", "reset":
		if len(rest) > 0 {
			return usageErr(fmt.Errorf("config %s takes no arguments", sub))
		}
		return a.configCall(ctx, c, "config."+sub, nil)
	case "set":
		if len(rest) == 0 {
			return usageErr(errors.New("config set needs KEY=VALUE arguments, such as frontends.ide.enabled=true"))
		}
		patch, err := setPatch(rest)
		if err != nil {
			return usageErr(err)
		}
		return a.configApply(ctx, c, patch)
	case "import":
		if len(rest) != 1 {
			return usageErr(errors.New("config import takes one FILE"))
		}
		patch, err := importPatch(rest[0])
		if err != nil {
			return output.Errorf(output.ExitFailure, "invalid_import", "", "%v", err)
		}
		return a.configApply(ctx, c, patch)
	default:
		return usageErr(fmt.Errorf("unknown config subcommand %q", sub))
	}
}

// configApply applies patch on top of the revision the broker reports now.
func (a *app) configApply(ctx context.Context, c broker.Client, patch json.RawMessage) error {
	code, raw, err := c.Verb(ctx, "config.get", nil)
	if err != nil {
		return output.Errorf(output.ExitFailure, "unreachable", "", "%v", err)
	}
	if code != http.StatusOK {
		return brokerError(code, raw)
	}
	var cur struct {
		Config config.Config `json:"config"`
	}
	if err := json.Unmarshal(raw, &cur); err != nil {
		return output.Errorf(output.ExitFailure, "bad_output", "", "%v", err)
	}
	return a.configCall(ctx, c, "config.apply", map[string]any{"patch": patch, "base_revision": cur.Config.Revision})
}

func (a *app) configCall(ctx context.Context, c broker.Client, verb string, in any) error {
	code, raw, err := c.Verb(ctx, verb, in)
	if err != nil {
		return output.Errorf(output.ExitFailure, "unreachable", "", "%v", err)
	}
	if code != http.StatusOK {
		return brokerError(code, raw)
	}
	return a.p.Result(json.RawMessage(raw), func(w io.Writer) { writeConfigHuman(w, verb, raw) })
}

func writeConfigHuman(w io.Writer, verb string, raw []byte) {
	var res struct {
		Config  json.RawMessage `json:"config"`
		Mode    string          `json:"mode"`
		Error   string          `json:"error"`
		Changes *struct {
			Started, Restarted, Stopped []string
		} `json:"changes"`
	}
	_ = json.Unmarshal(raw, &res)
	if res.Changes == nil {
		if res.Mode == broker.ModeRecovery {
			fmt.Fprintf(w, "RECOVERY MODE: %s\nrun berth config revert or berth config reset\n", res.Error)
		}
		var pretty bytes.Buffer
		_ = json.Indent(&pretty, res.Config, "", "  ")
		fmt.Fprintln(w, pretty.String())
		return
	}
	var c config.Config
	_ = json.Unmarshal(res.Config, &c)
	var parts []string
	for _, p := range []struct {
		label string
		names []string
	}{{"started", res.Changes.Started}, {"restarted", res.Changes.Restarted}, {"stopped", res.Changes.Stopped}} {
		if len(p.names) > 0 {
			parts = append(parts, p.label+" "+strings.Join(p.names, ", "))
		}
	}
	if len(parts) == 0 {
		parts = []string{"no service changed"}
	}
	fmt.Fprintf(w, "applied revision %d; %s\n", c.Revision, strings.Join(parts, "; "))
}

// brokerError turns a broker error envelope into a CLI error.
func brokerError(code int, raw []byte) error {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Error.Code == "" {
		return output.Errorf(output.ExitFailure, "broker_error", "", "the supervisor answered %d: %s", code, bytes.TrimSpace(raw))
	}
	return output.Errorf(output.ExitFailure, body.Error.Code, "", "%s", body.Error.Message)
}

// setPatch turns KEY=VALUE pairs into a merge patch. VALUE is JSON when it
// parses as JSON (5, true, null, ["a"]), otherwise a string (Europe/Rome).
func setPatch(pairs []string) (json.RawMessage, error) {
	root := map[string]any{}
	for _, pair := range pairs {
		key, val, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("%q is not KEY=VALUE", pair)
		}
		parts := strings.Split(key, ".")
		if slices.Contains(parts, "") {
			return nil, fmt.Errorf("%q is not a dotted setting name", key)
		}
		var v any
		if err := json.Unmarshal([]byte(val), &v); err != nil {
			v = val
		}
		m := root
		for _, part := range parts[:len(parts)-1] {
			next, ok := m[part].(map[string]any)
			if !ok {
				next = map[string]any{}
				m[part] = next
			}
			m = next
		}
		m[parts[len(parts)-1]] = v
	}
	return json.Marshal(root)
}

// importPatch reads an exported config, bare or as {"config": ...}, and turns
// it into a patch: schema must be the current one, revision is dropped, and
// every other key is applied.
func importPatch(path string) (json.RawMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxImportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxImportBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxImportBytes)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s is not a JSON object: %w", path, err)
	}
	if inner, ok := doc["config"]; ok {
		doc = nil
		if err := json.Unmarshal(inner, &doc); err != nil {
			return nil, fmt.Errorf("%s: config is not a JSON object: %w", path, err)
		}
	}
	var schema int
	if err := json.Unmarshal(doc["schema"], &schema); err != nil || schema != config.Schema {
		return nil, fmt.Errorf("%s has config schema %s; this Berth reads schema %d", path, doc["schema"], config.Schema)
	}
	delete(doc, "schema")
	delete(doc, "revision")
	patch := map[string]json.RawMessage{}
	for _, k := range slices.Sorted(maps.Keys(doc)) {
		patch[k] = doc[k]
	}
	return json.Marshal(patch)
}

type healthResult struct {
	Healthy  bool     `json:"healthy"`
	Problems []string `json:"problems"`
}

// runHealth backs the image's HEALTHCHECK: healthy when the config is usable
// and every enabled service runs.
func runHealth(ctx context.Context, a *app, args []string) error {
	fs := newFlagSet("health")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	if len(pos) > 0 {
		return usageErr(errors.New("health takes no arguments"))
	}
	a.p.JSON = *jsonOut
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	res := healthResult{Problems: []string{}}
	st, err := broker.NewClient(a.cfg.BrokerSocket).Status(ctx)
	if err != nil {
		res.Problems = append(res.Problems, err.Error())
	} else {
		res.Problems = append(res.Problems, broker.Problems(st)...)
	}
	res.Healthy = len(res.Problems) == 0
	err = a.p.Result(res, func(w io.Writer) {
		if res.Healthy {
			fmt.Fprintln(w, "healthy")
		} else {
			fmt.Fprintln(w, "unhealthy: "+strings.Join(res.Problems, "; "))
		}
	})
	if err != nil {
		return err
	}
	if !res.Healthy {
		return output.ExitCode(1)
	}
	return nil
}
