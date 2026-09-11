// Command node-agent-client is the console's own on-node orchestration
// client - see internal/nodeagent and openspec/specs/node-agent.
// It connects to the console's node transport
// using the node's existing Puppet certificate and executes run/task
// requests it receives.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/voxpupuli/enterprise-console/internal/nodeagent"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// On Windows, when launched by the Service Control Manager, this
	// runs the whole lifecycle itself (via run, with the SCM's
	// Stop/Shutdown request cancelling run's context instead of an OS
	// signal) and returns true - see service_windows.go. On every other
	// platform, and when run interactively on Windows, it's a no-op
	// stub (service_other.go) and the normal signal-driven path below
	// handles everything, matching this binary's original behavior.
	if runAsWindowsService(logger) {
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger); err != nil {
		os.Exit(1)
	}
}

// run loads configuration, builds the node-agent client, and runs it
// until ctx is cancelled. Shared by the plain foreground path above and
// the Windows-service path (service_windows.go) - the only difference
// between them is what cancels ctx.
func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := nodeagent.LoadConfig(os.Getenv)
	if err != nil {
		logger.Error("configuration error", "error", err)
		return err
	}

	handler := nodeagent.NewHandler(nodeagent.RunCommand, cfg.PuppetBinPath, cfg.FactsDDir, nodeagent.FileExists)
	client := nodeagent.New(cfg, handler, logger)

	logger.Info("node-agent-client starting", "transport", cfg.TransportAddr)
	if err := client.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("node-agent-client exited", "error", err)
		return err
	}
	return nil
}
