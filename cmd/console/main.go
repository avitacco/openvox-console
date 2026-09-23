// Command console is the OpenVox Console: a single binary serving the
// control plane (dashboard, classification, RBAC, code deployment,
// orchestration) alongside the reused openvox-server/openvoxdb data plane.
//
// This file is deliberately thin: it does only what an entrypoint must -
// the container health check, logging setup, configuration loading, and
// signal handling - then hands off to internal/app, which decides what
// the configured run mode actually starts.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/voxpupuli/enterprise-console/internal/app"
	"github.com/voxpupuli/enterprise-console/internal/runtime"
)

// runHealthCheck implements the container healthcheck. The image has no
// shell and no wget - it is distroless, so a HEALTHCHECK expressed as a
// shell pipeline can never run and the container reports unhealthy
// forever while serving traffic perfectly well. The binary is the only
// executable present, so it has to check itself.
//
// CONSOLE_HTTP_ADDR is the address the server binds, so dialling its port
// on the loopback checks the same listener a request would reach.
func runHealthCheck() int {
	addr := os.Getenv("CONSOLE_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		fmt.Fprintf(os.Stderr, "health-check: cannot parse CONSOLE_HTTP_ADDR %q\n", addr)
		return 1
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "health-check: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "health-check: HTTP %d\n", resp.StatusCode)
		return 1
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		fmt.Fprintf(os.Stderr, "health-check: %v\n", err)
		return 1
	}
	if body.Status != "ok" {
		fmt.Fprintf(os.Stderr, "health-check: status %q\n", body.Status)
		return 1
	}
	return 0
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--health-check" {
		os.Exit(runHealthCheck())
	}

	logger := runtime.NewLogger(os.Stdout)

	// .env is a local development convenience (see internal/runtime.Config)
	// and is entirely optional: it's fine, and expected, for it not to
	// exist (e.g. in production, where real env vars are set directly).
	// Existing environment variables always take precedence over it.
	if _, statErr := os.Stat(".env"); statErr == nil {
		if err := godotenv.Load(); err != nil {
			logger.Error("failed to load .env file", "error", err)
			os.Exit(1)
		}
	}

	cfg, err := runtime.LoadConfig(os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	// Logged before any subsystem is constructed: which mode an instance
	// is in determines what it will and will not start, so an operator
	// reading these logs needs it first, not alongside the "ready" line
	// at the end.
	logger.Info("starting console", "mode", cfg.Mode)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg, logger); err != nil {
		logger.Error("console exited with an error", "error", err)
		// stop() is deferred above and os.Exit skips it, so release the
		// signal handler explicitly before exiting non-zero.
		stop()
		os.Exit(1)
	}
}
