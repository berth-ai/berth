package cli

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
	"path/filepath"
	"slices"
	"text/tabwriter"
	"time"

	"github.com/berth-ai/berth/internal/broker"
	"github.com/berth-ai/berth/internal/orcasync"
	"github.com/berth-ai/berth/internal/output"
	"github.com/berth-ai/berth/internal/pairqr"
)

// orcaPairResult is berth orca pair --json. The QR matrix stays out: the
// link is all a script needs.
type orcaPairResult struct {
	URL     string `json:"url"`
	Scope   string `json:"scope"`
	Address string `json:"address"`
	Warning string `json:"warning,omitempty"`
}

// orcaDevicesResult is berth orca devices --json, the broker's answer.
type orcaDevicesResult struct {
	Devices []orcaDevice `json:"devices"`
}

type orcaDevice struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Scope      string     `json:"scope"`
	PairedAt   *time.Time `json:"paired_at"`
	LastSeenAt *time.Time `json:"last_seen_at"`
}

// runOrca is berth orca: pair prints a link that pairs an Orca app with
// this Berth, restarting Orca once while its terminal sessions survive;
// devices lists the apps already paired; sync, run as agent, adds the
// workspaces to Orca and removes the ones Berth added that are gone;
// status, run as agent, says which workspaces Orca has.
func runOrca(ctx context.Context, a *app, args []string) error {
	fs := newFlagSet("orca")
	jsonOut := fs.Bool("json", false, "print JSON")
	mobile := fs.Bool("mobile", false, "pair the Orca mobile app instead of the desktop app")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	a.p.JSON = *jsonOut
	switch {
	case len(pos) == 0:
		return usageErr(errors.New("orca needs a subcommand: pair, devices, sync or status"))
	case !slices.Contains([]string{"pair", "devices", "sync", "status"}, pos[0]):
		return usageErr(fmt.Errorf("unknown orca subcommand %q; use pair, devices, sync or status", pos[0]))
	case len(pos) > 1:
		return usageErr(fmt.Errorf("orca %s takes no arguments", pos[0]))
	case pos[0] != "pair" && *mobile:
		return usageErr(errors.New("--mobile is for orca pair"))
	}
	if pos[0] == "sync" || pos[0] == "status" {
		return orcaSync(ctx, a, pos[0])
	}
	if !a.cfg.IsRoot {
		return output.Errorf(output.ExitNotRoot, "not_root", "",
			"berth orca must run as root (docker exec -u root <container> berth orca %s)", pos[0])
	}
	if pos[0] == "devices" {
		return orcaDevices(ctx, a)
	}
	scope := "desktop"
	if *mobile {
		scope = "mobile"
	}
	code, raw, err := broker.NewClient(a.cfg.BrokerSocket).Verb(ctx, "orca.pair", map[string]string{"scope": scope})
	if err != nil {
		return output.Errorf(output.ExitFailure, "unreachable", "", "%v", err)
	}
	if code != http.StatusOK {
		return brokerError(code, raw)
	}
	var res struct {
		orcaPairResult
		QR []string `json:"qr"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return output.Errorf(output.ExitFailure, "broker_error", "", "the supervisor answered: %v", err)
	}
	if res.Warning != "" && !a.p.JSON {
		fmt.Fprintf(a.p.Stderr, "berth: warning: %s\n", res.Warning)
	}
	return a.p.Result(res.orcaPairResult, func(w io.Writer) {
		fmt.Fprint(w, pairqr.Terminal(res.QR))
		fmt.Fprintf(w, "\nOpen this link in Orca (%s, dialing %s):\n%s\n\nA new link replaces any unused one.\n", res.Scope, res.Address, res.URL)
	})
}

// orcaDevices prints the Orca apps paired with this Berth.
func orcaDevices(ctx context.Context, a *app) error {
	code, raw, err := broker.NewClient(a.cfg.BrokerSocket).Verb(ctx, "orca.devices", nil)
	if err != nil {
		return output.Errorf(output.ExitFailure, "unreachable", "", "%v", err)
	}
	if code != http.StatusOK {
		return brokerError(code, raw)
	}
	var res orcaDevicesResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return output.Errorf(output.ExitFailure, "broker_error", "", "the supervisor answered: %v", err)
	}
	if res.Devices == nil {
		res.Devices = []orcaDevice{}
	}
	return a.p.Result(res, func(w io.Writer) {
		if len(res.Devices) == 0 {
			fmt.Fprintln(w, "No Orca apps are paired. Pair one with berth orca pair.")
			return
		}
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tAPP\tPAIRED\tLAST SEEN")
		for _, d := range res.Devices {
			app := d.Scope
			if app == "mobile" {
				app = "phone"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", d.Name, app, localTime(d.PairedAt), localTime(d.LastSeenAt))
		}
		tw.Flush()
	})
}

// orcaSync is the Orca projects converger, or with verb status its report.
// It runs as agent, whose Orca profile holds the projects; root's would be
// another, empty one.
func orcaSync(ctx context.Context, a *app, verb string) error {
	if a.cfg.IsRoot {
		return output.Errorf(output.ExitFailure, "wrong_user", "orca",
			"berth orca %s must run as agent (docker exec -u agent <container> berth orca %[1]s)", verb)
	}
	home := a.cfg.Getenv("HOME")
	if home == "" {
		return output.Errorf(output.ExitFailure, "no_home", "orca", "HOME is not set")
	}
	s := orcasync.Syncer{
		Orca: func(ctx context.Context, args ...string) ([]byte, error) {
			var stdout, stderr bytes.Buffer
			cmd := exec.CommandContext(ctx, a.cfg.OrcaIDE, args...)
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			cmd.WaitDelay = 5 * time.Second
			if err := cmd.Run(); err != nil {
				return nil, fmt.Errorf("%s", cmp.Or(orcasync.Message(stdout.Bytes(), stderr.Bytes()), err.Error()))
			}
			return stdout.Bytes(), nil
		},
		Workspaces: filepath.Join(a.cfg.ConfigDir, "workspaces.json"),
		State:      filepath.Join(home, ".local", "state", "berth", "orca.json"),
		Wait:       cmp.Or(a.cfg.OrcaWait, 90*time.Second),
		Poll:       2 * time.Second,
	}
	if verb == "status" {
		return orcaStatus(ctx, a, s)
	}
	ctx, cancel := context.WithTimeout(ctx, orcasync.Timeout)
	defer cancel()
	res, err := s.Run(ctx)
	if errors.Is(err, orcasync.ErrUnavailable) {
		return output.Errorf(output.ExitFailure, "orca_unavailable", "orca", "%v", err)
	}
	if err != nil {
		return output.Errorf(output.ExitFailure, "orca_sync_failed", "orca", "%v", err)
	}
	for _, p := range slices.Sorted(maps.Keys(res.Kept)) {
		a.p.Logf("warning: kept %s in Orca: %s", p, res.Kept[p])
	}
	for _, p := range slices.Sorted(maps.Keys(res.Failed)) {
		a.p.Logf("warning: could not add %s to Orca: %s", p, res.Failed[p])
	}
	return a.p.Result(res, func(w io.Writer) {
		for _, p := range res.Added {
			fmt.Fprintf(w, "added %s\n", p)
		}
		for _, p := range res.Removed {
			fmt.Fprintf(w, "removed %s\n", p)
		}
		if len(res.Added)+len(res.Removed) == 0 {
			fmt.Fprintln(w, "Orca already has every workspace.")
		}
	})
}

// orcaStatus asks Orca once, without waiting: a report must not hang on an
// Orca that is still starting.
func orcaStatus(ctx context.Context, a *app, s orcasync.Syncer) error {
	s.Wait = 0
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	st, err := s.Status(ctx)
	if errors.Is(err, orcasync.ErrUnavailable) {
		return output.Errorf(output.ExitFailure, "orca_unavailable", "orca", "%v", err)
	}
	if err != nil {
		return output.Errorf(output.ExitFailure, "orca_status_failed", "orca", "%v", err)
	}
	return a.p.Result(st, func(w io.Writer) {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "WORKSPACE\tIN ORCA\tFOLDER")
		for _, id := range slices.Sorted(maps.Keys(st.Workspaces)) {
			p := st.Workspaces[id]
			in := "no"
			if p.InOrca {
				in = "yes"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", id, in, p.Path)
		}
		tw.Flush()
		for _, id := range slices.Sorted(maps.Keys(st.Workspaces)) {
			if e := st.Workspaces[id].Error; e != "" {
				fmt.Fprintf(w, "%s: the last sync could not add it: %s\n", id, e)
			}
		}
		fmt.Fprintf(w, "Last sync: %s\n", localTime(st.SyncedAt))
	})
}

// localTime is t in the container's time zone, or "never".
func localTime(t *time.Time) string {
	if t == nil {
		return "never"
	}
	return t.Local().Format("2006-01-02 15:04")
}
