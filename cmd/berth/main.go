// Command berth manages the agent store of a Berth container.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/salgnt/berth/internal/cli"
)

// Set with -ldflags at build time.
var (
	version     = "dev"
	commit      = "unknown"
	defaultRepo = "SalGnt/berth"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := cli.DefaultConfig(os.Getenv, cli.Build{Version: version, Commit: commit, Repo: defaultRepo})
	if err != nil {
		fmt.Fprintln(os.Stderr, "berth: error:", err)
		return 1
	}
	return cli.Run(ctx, cfg, os.Args[1:], os.Stdout, os.Stderr)
}
