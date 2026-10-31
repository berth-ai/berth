package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

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

// runOrca prints a link that pairs an Orca app with this Berth. Pairing
// restarts Orca once; its terminal sessions survive.
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
		return usageErr(errors.New("orca needs a subcommand: pair"))
	case pos[0] != "pair":
		return usageErr(fmt.Errorf("unknown orca subcommand %q; use pair", pos[0]))
	case len(pos) > 1:
		return usageErr(errors.New("orca pair takes no arguments"))
	}
	if !a.cfg.IsRoot {
		return output.Errorf(output.ExitNotRoot, "not_root", "",
			"berth orca must run as root (docker exec -u root <container> berth orca pair)")
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
