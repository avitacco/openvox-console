// Command node-agent-client is the console's own on-node orchestration
// client - see internal/nodeagent and openspec/specs/node-agent.
// It connects to the console's node transport
// using the node's existing Puppet certificate and executes run/task
// requests it receives.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/voxpupuli/enterprise-console/internal/nodeagent"
)

func main() {
	// Handled before anything else, and without configuration: a client
	// about to be installed is run this way to prove it starts and is
	// the build that was asked for (see nodeagent's upgrade validation).
	if len(os.Args) == 2 && os.Args[1] == "--build-info" {
		os.Exit(printBuildInfo())
	}

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

// printBuildInfo writes this client's build identity as one JSON object
// and returns the process exit code.
func printBuildInfo() int {
	info, err := nodeagent.CurrentBuildInfo()
	if err != nil {
		fmt.Fprintln(os.Stderr, "node-agent-client:", err)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(info); err != nil {
		return 1
	}
	return 0
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

	refreshPackageInventoryFact(logger, cfg.FactsDDir, nodeagent.FileExists)

	// Tasks need the node's certificate to download task files. The
	// client could not connect without that certificate either, so a
	// failure here is reported, and runs keep working without tasks.
	tasks, err := nodeagent.NewTaskExecutor(cfg, nodeagent.RunCommand)
	if err != nil {
		logger.Error("task execution is unavailable", "error", err)
		tasks = nil
	}
	// Without an Upgrader the client still runs and the console sees it
	// as one it cannot upgrade, which is the truth.
	var agent nodeagent.AgentActions
	upgrader, err := nodeagent.NewUpgrader(cfg, nodeagent.RunCommand, logger)
	if err != nil {
		logger.Error("build reporting and upgrades are unavailable", "error", err)
	} else {
		upgrader.CleanupAfterUpgrade()
		agent = upgrader
	}

	patcher := nodeagent.NewPatcher(nodeagent.RunCommand, nodeagent.FileExists, nodeagent.PatchStateFile(cfg), logger)
	handler := nodeagent.NewHandlerWithPatches(nodeagent.RunCommand, cfg.PuppetBinPath, cfg.FactsDDir, nodeagent.FileExists, tasks, agent, patcher)
	client := nodeagent.New(cfg, handler, logger)
	client.SetAfterReply(func() func(context.Context) {
		if upgrader != nil {
			if f := upgrader.TakeAfterReply(); f != nil {
				return f
			}
		}
		return patcher.TakeAfterReply()
	})

	logger.Info("node-agent-client starting", "transport", cfg.TransportAddr,
		"version", nodeagent.Version, "buildTime", nodeagent.BuildTime)
	if err := client.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("node-agent-client exited", "error", err)
		return err
	}
	return nil
}

// refreshPackageInventoryFact brings an already-enabled package-inventory
// fact up to this client's version (see
// nodeagent.RefreshPackageInventoryFact). A failure is logged, never
// fatal - a stale fact is no reason to leave the node unreachable for
// runs and tasks.
func refreshPackageInventoryFact(logger *slog.Logger, factsDir string, fileExists func(string) bool) {
	changed, err := nodeagent.RefreshPackageInventoryFact(factsDir, fileExists)
	if err != nil {
		logger.Warn("could not refresh package-inventory fact", "error", err)
		return
	}
	if changed {
		logger.Info("refreshed package-inventory fact to this client's version", "factsDir", factsDir)
	}
}
