package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"text/tabwriter"
	"time"

	"github.com/berth-ai/berth/internal/broker"
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
// devices lists the apps already paired.
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
		return usageErr(errors.New("orca needs a subcommand: pair or devices"))
	case pos[0] != "pair" && pos[0] != "devices":
		return usageErr(fmt.Errorf("unknown orca subcommand %q; use pair or devices", pos[0]))
	case len(pos) > 1:
		return usageErr(fmt.Errorf("orca %s takes no arguments", pos[0]))
	case pos[0] == "devices" && *mobile:
		return usageErr(errors.New("--mobile is for orca pair"))
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

// localTime is t in the container's time zone, or "never".
func localTime(t *time.Time) string {
	if t == nil {
		return "never"
	}
	return t.Local().Format("2006-01-02 15:04")
}
