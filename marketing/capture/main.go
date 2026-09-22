// Command capture screenshots the console for the marketing site.
//
// It drives a real browser against a real console: it logs in, visits
// each view declared in marketing/shots, and writes a light and a dark
// PNG per view into docs/assets/screenshots - the directory GitHub Pages
// serves, so the images are committed once rather than copied at build
// time.
//
// Everything that would vary between runs and is not the page itself is
// pinned: the browser (a fixed container image), the clock (the demo
// instant), the viewport, and animation. See determinism.go. Byte-for-
// byte identical output is not guaranteed - see marketing/README.md.
//
// Usage:
//
//	go run ./marketing/capture --console-url http://console:8080
//
// or `make marketing-screenshots`, which starts the stack, seeds it and
// runs this.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/voxpupuli/enterprise-console/marketing/shots"
	"time"
)

type options struct {
	// consoleURL is how this tool reaches the console, to log in.
	consoleURL string
	// browserConsoleURL is how the *browser* reaches the same console.
	//
	// They differ whenever the browser is not on the same network as
	// this tool, which is the normal development case: the console runs
	// on the host and the browser in a container, so the host calls it
	// on localhost while the container must use host.docker.internal.
	// Collapsing the two into one flag means one of them is always
	// wrong.
	browserConsoleURL string
	browserURL        string
	outDir            string

	username string
	password string

	// locale is the console language to capture in. Empty or "en"
	// captures English, whose files keep their unsuffixed names.
	//
	// The console stores the choice in localStorage and reads it before
	// first paint, so this is set on the page rather than passed as a
	// URL parameter - there is no query string that changes the
	// console's language.
	locale string

	// only captures a single shot by name, for iterating on one view.
	only string

	timeout time.Duration
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "capture: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var opts options

	flag.StringVar(&opts.consoleURL, "console-url", envOr("CAPTURE_CONSOLE_URL", "http://localhost:8080"), "console URL as reachable from here, used to log in")
	flag.StringVar(&opts.browserConsoleURL, "browser-console-url", os.Getenv("CAPTURE_BROWSER_CONSOLE_URL"), "console URL as reachable from the browser container (default: same as --console-url; use http://host.docker.internal:8080 for a console running on the host)")
	flag.StringVar(&opts.browserURL, "browser-url", envOr("CAPTURE_BROWSER_URL", "http://localhost:9222"), "DevTools endpoint of the headless browser")
	flag.StringVar(&opts.outDir, "out", "", "directory to write PNGs into (default: docs/assets/screenshots in this repository)")
	flag.StringVar(&opts.username, "username", envOr("CONSOLE_BOOTSTRAP_ADMIN_USERNAME", "admin"), "console user to log in as")
	flag.StringVar(&opts.password, "password", os.Getenv("CONSOLE_BOOTSTRAP_ADMIN_PASSWORD"), "that user's password (or set CONSOLE_BOOTSTRAP_ADMIN_PASSWORD)")
	flag.StringVar(&opts.only, "only", "", "capture just this one shot, by name")
	flag.StringVar(&opts.locale, "locale", "en", "console language to capture in (en, de, es, ja, ar, ...)")
	flag.DurationVar(&opts.timeout, "timeout", 3*time.Minute, "overall deadline for the whole run")
	flag.Parse()

	if opts.password == "" {
		return fmt.Errorf("no console password: pass --password or set CONSOLE_BOOTSTRAP_ADMIN_PASSWORD")
	}
	if opts.browserConsoleURL == "" {
		opts.browserConsoleURL = opts.consoleURL
	}

	if opts.outDir == "" {
		dir, err := defaultOutDir()
		if err != nil {
			return err
		}
		opts.outDir = dir
	}
	if err := os.MkdirAll(opts.outDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", opts.outDir, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()

	return capture(ctx, opts)
}

// defaultOutDir resolves where screenshots are written.
//
// They go into docs/, the directory GitHub Pages serves, because that is
// where the published site reads them from. Writing them anywhere else
// would mean committing the same 2MB of images twice.
//
// The repository root is found by walking up for the marketing
// directory, so this works whether the tool is run from the root or from
// marketing/capture.
func defaultOutDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if info, err := os.Stat(filepath.Join(dir, "marketing")); err == nil && info.IsDir() {
			return filepath.Join(dir, "docs", shots.AssetDir), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find the repository root from %s - pass --out", wd)
		}
		dir = parent
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
