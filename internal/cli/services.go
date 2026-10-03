package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/salgnt/berth/internal/broker"
	"github.com/salgnt/berth/internal/output"
	"github.com/salgnt/berth/internal/panel"
)

// runPanel serves the web panel as the unprivileged berth-panel user and
// forwards every privileged request to the broker.
func runPanel(ctx context.Context, a *app, args []string) error {
	if len(args) > 0 {
		return usageErr(fmt.Errorf("panel takes no arguments"))
	}
	if a.cfg.IsRoot {
		return output.Errorf(output.ExitFailure, "root", "", "berth panel must not run as root; the panel module starts it as berth-panel")
	}
	h := panel.New(broker.UnixClient(a.cfg.BrokerSocket), broker.BaseURL, panel.DefaultUpstreams())
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

// serveHTTP serves h on l until ctx is cancelled.
func serveHTTP(ctx context.Context, l net.Listener, h http.Handler) error {
	return serveServer(ctx, l, &http.Server{Handler: h})
}

// serveServer serves srv on l until ctx is cancelled, then shuts down
// gracefully. No write timeout: jobs and the proxied frontends stream.
func serveServer(ctx context.Context, l net.Listener, srv *http.Server) error {
	srv.ReadHeaderTimeout = 10 * time.Second
	srv.IdleTimeout = 2 * time.Minute
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
