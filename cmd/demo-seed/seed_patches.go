package main

import (
	"context"
	"fmt"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/demodata"
	"github.com/voxpupuli/enterprise-console/internal/patchinventory"
	"github.com/voxpupuli/enterprise-console/internal/persistence"
)

// seedPatches records the demo nodes' pending updates through
// patchinventory.Store.Save - the same call a real scan's result goes
// through - so the Patching page has a fleet to show. A scan result has no
// HTTP write contract: it arrives from a node.
//
// Scans are dated from now, not from the demo Instant. The console's own
// scheduler forgets a node's snapshot once it has gone ninety days without
// an attempt, so one dated to the demo's June would be purged the moment
// the demo console started - and the page would show nothing scanned. The
// Patching screenshot therefore shows real times, like the instances'
// uptime on the System page, and differs a little between runs.
func seedPatches(ctx context.Context, opts options) error {
	if opts.postgresDSN == "" {
		return fmt.Errorf("no console Postgres DSN: pass --postgres-dsn or set CONSOLE_POSTGRES_DSN (scan results are written through the console's own store)")
	}
	db, err := persistence.Connect(ctx, opts.postgresDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	store := patchinventory.NewStore(db.Pool)

	var updates int
	for _, scan := range demodata.PatchScans {
		result := patchinventory.ScanResult{Manager: scan.Manager, RebootRequired: scan.RebootRequired}
		for _, u := range scan.Updates {
			result.Updates = append(result.Updates, patchinventory.Update{Name: u.Name, Installed: u.Installed, Available: u.Available, Security: u.Security})
		}
		// Save replaces a node's updates, so running the seed again
		// converges rather than accumulating.
		if err := store.Save(ctx, scan.Certname, result, time.Now().Add(-scan.Age)); err != nil {
			return fmt.Errorf("saving the scan of %s: %w", scan.Certname, err)
		}
		updates += len(scan.Updates)
	}
	// A SLES node is not scanned: the page must say it cannot report, not
	// show it as fully patched.
	for _, n := range demodata.Fleet {
		if n.Platform.PkgProvider == "zypper" {
			if err := store.RecordFailure(ctx, n.Certname, "this operating system's package manager is not supported", true, time.Now().Add(-2*time.Hour)); err != nil {
				return fmt.Errorf("recording that %s cannot be scanned: %w", n.Certname, err)
			}
		}
	}
	fmt.Printf("    %d nodes scanned, %d pending updates\n", len(demodata.PatchScans), updates)
	return nil
}
