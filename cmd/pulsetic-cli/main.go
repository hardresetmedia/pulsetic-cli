// Command pulsetic-cli records Pulsetic API responses to an append-only,
// hash-chained JSONL audit log for SLA and compliance evidence.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/program-the-brain-not-the-heartbeat/pulsetic-cli/internal/cli"
)

// version is the tool version stamped into every audit record.
// Overridden at build time via -ldflags "-X main.version=...".
var version = "0.1.0"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	root := cli.NewRootCmd(version)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "pulsetic-cli:", err)
		os.Exit(1)
	}
}
