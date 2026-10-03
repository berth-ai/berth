// Command berth manages the agent store of a Berth container.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/berth-ai/berth/internal/cli"
)

// Set with -ldflags at build time.
var (
	version     = "dev"
	commit      = "unknown"
	defaultRepo = "berth-ai/berth"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigc)
	go func() {
		if sig, ok := <-sigc; ok {
			cancel(cli.SignalCause{Signal: sig})
		}
	}()
	cfg, err := cli.DefaultConfig(os.Getenv, cli.Build{Version: version, Commit: commit, Repo: defaultRepo})
	if err != nil {
		fmt.Fprintln(os.Stderr, "berth: error:", err)
		return 1
	}
	return cli.Run(ctx, cfg, os.Args[1:], os.Stdout, os.Stderr)
}
