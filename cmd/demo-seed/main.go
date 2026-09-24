// Command demo-seed populates a local OpenVox Console with a fabricated
// fleet - nodes, facts, reports, groups, users, jobs, deployments and
// vulnerability findings - so that screenshots and hands-on demos show a
// console in use rather than a stack of empty states.
//
// It is a development-time tool. It is not part of the console binary,
// not reachable at runtime, and not included in the published images.
//
// The data it writes is fabricated. Orchestration job history in
// particular is invented rather than a record of work that happened: it
// is fine in a screenshot of the Jobs page, and it is not evidence of
// anything. See marketing/README.md.
//
// Usage:
//
//	go run ./cmd/demo-seed --i-know-this-is-a-demo-console
//
// See `--help` for the connection flags. The seed refuses any target that
// is not a local development console (see safety.go).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

type options struct {
	consoleURL   string
	openvoxdbURL string

	openvoxdbCert string
	openvoxdbKey  string
	openvoxdbCA   string

	adminUser string
	adminPass string

	postgresDSN string

	confirmed bool
	reset     bool
	remove    bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "demo-seed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var opts options

	flag.StringVar(&opts.consoleURL, "console-url", envOr("CONSOLE_DEMO_SEED_URL", "http://localhost:8080"), "base URL of the local console to seed")
	flag.StringVar(&opts.openvoxdbURL, "openvoxdb-url", envOr("CONSOLE_OPENVOXDB_URL", "https://localhost:8081"), "base URL of the local openvoxdb to seed")
	flag.StringVar(&opts.openvoxdbCert, "openvoxdb-cert", envOr("CONSOLE_OPENVOXDB_CERT_FILE", "certs/console-cert.pem"), "client certificate for openvoxdb")
	flag.StringVar(&opts.openvoxdbKey, "openvoxdb-key", envOr("CONSOLE_OPENVOXDB_KEY_FILE", "certs/console-key.pem"), "client key for openvoxdb")
	flag.StringVar(&opts.openvoxdbCA, "openvoxdb-ca", envOr("CONSOLE_OPENVOXDB_CA_FILE", "certs/ca.pem"), "CA certificate trusted for openvoxdb")
	flag.StringVar(&opts.adminUser, "admin-user", envOr("CONSOLE_BOOTSTRAP_ADMIN_USERNAME", "admin"), "console admin username used for API writes")
	flag.StringVar(&opts.adminPass, "admin-password", os.Getenv("CONSOLE_BOOTSTRAP_ADMIN_PASSWORD"), "console admin password (or set CONSOLE_BOOTSTRAP_ADMIN_PASSWORD)")
	flag.StringVar(&opts.postgresDSN, "postgres-dsn", os.Getenv("CONSOLE_POSTGRES_DSN"), "console Postgres DSN, for the records that have no HTTP write contract")
	flag.BoolVar(&opts.confirmed, "i-know-this-is-a-demo-console", false, "confirm the targets are throwaway local stacks that may be filled with fabricated data")
	flag.BoolVar(&opts.remove, "remove", false, "only delete the demo fleet from openvoxdb, then exit - for cleaning up after a screenshot run (leaves nodes this tool did not create alone)")
	flag.BoolVar(&opts.reset, "reset", false, "delete the demo fleet from openvoxdb before seeding, so previously stored reports are rewritten rather than deduplicated away (leaves nodes this tool did not create alone)")

	flag.Parse()

	// The safety check runs before anything else opens a connection, so a
	// refusal costs nothing and cannot half-write.
	if err := checkTarget("console", opts.consoleURL, opts.confirmed, defaultLookup); err != nil {
		return err
	}
	if err := checkTarget("openvoxdb", opts.openvoxdbURL, opts.confirmed, defaultLookup); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if opts.remove {
		return resetFleet(ctx, opts)
	}
	return seed(ctx, opts)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
