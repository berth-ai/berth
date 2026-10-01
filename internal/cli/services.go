package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/salgnt/berth/internal/broker"
	"github.com/salgnt/berth/internal/output"
	"github.com/salgnt/berth/internal/panel"
)

// runBroker serves the root side of the web panel on a unix socket that only
// root and the agent group can reach. It runs until ctx is cancelled.
func runBroker(ctx context.Context, a *app, args []string) error {
	if len(args) > 0 {
		return usageErr(fmt.Errorf("broker takes no arguments"))
	}
	if !a.cfg.IsRoot {
		return output.Errorf(output.ExitNotRoot, "not_root", "", "berth broker must run as root")
	}
	if a.cfg.AgentErr != nil {
		return output.Errorf(output.ExitFailure, "no_agent_user", "", "%v", a.cfg.AgentErr)
	}
	l, err := broker.Listen(a.cfg.BrokerSocket, int(a.cfg.Agent.GID))
	if err != nil {
		return output.Errorf(output.ExitFailure, "listen_failed", "listen", "%v", err)
	}
	b := broker.New(ctx, broker.CommandExec(a.cfg.BerthPath))
	b.Log = a.p.Stderr
	fmt.Fprintf(a.p.Stderr, "berth broker: listening on %s\n", a.cfg.BrokerSocket)
	err = serveHTTP(ctx, l, b.Handler())
	b.Wait() // a running job received SIGTERM with ctx
	if err != nil {
		return output.Errorf(output.ExitFailure, "serve_failed", "serve", "%v", err)
	}
	return nil
}

// runPanel serves the web panel as the unprivileged agent user and forwards
// every privileged request to the broker.
func runPanel(ctx context.Context, a *app, args []string) error {
	if len(args) > 0 {
		return usageErr(fmt.Errorf("panel takes no arguments"))
	}
	if a.cfg.IsRoot {
		return output.Errorf(output.ExitFailure, "root", "", "berth panel must not run as root; the panel module starts it as agent")
	}
	if err := validListen(a.cfg.PanelListen); err != nil {
		return output.Errorf(output.ExitFailure, "invalid_config", "", "%v", err)
	}
	h, err := panel.New(a.cfg.PanelCredential, panel.UnixClient(a.cfg.BrokerSocket), panel.BrokerURL)
	if err != nil {
		return output.Errorf(output.ExitFailure, "invalid_config", "", "%v", err)
	}
	l, err := net.Listen("tcp", a.cfg.PanelListen)
	if err != nil {
		return output.Errorf(output.ExitFailure, "listen_failed", "listen", "%v", err)
	}
	fmt.Fprintf(a.p.Stderr, "berth panel: listening on %s\n", l.Addr())
	if err := serveHTTP(ctx, l, h); err != nil {
		return output.Errorf(output.ExitFailure, "serve_failed", "serve", "%v", err)
	}
	return nil
}

func validListen(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	n, convErr := strconv.Atoi(port)
	if err != nil || convErr != nil || n < 0 || n > 65535 {
		return fmt.Errorf("PANEL_PORT must be a port number between 1 and 65535, got %q", port)
	}
	return nil
}

// serveHTTP serves h on l until ctx is cancelled, then shuts down gracefully.
// No write timeout: `status --check` may legitimately take minutes.
func serveHTTP(ctx context.Context, l net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(l) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}
