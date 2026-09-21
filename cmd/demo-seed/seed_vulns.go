package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/demodata"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
	"github.com/voxpupuli/enterprise-console/internal/persistence"
	"github.com/voxpupuli/enterprise-console/internal/vulnerability"
	"github.com/voxpupuli/enterprise-console/internal/vulnerability/osv"
)

// seedVulnerabilities records demo vulnerability findings through
// vulnerability.Engine.Apply - the same call a real provider sync makes
// when it has collected its results.
//
// The findings themselves are fabricated, but they are matched against
// packages the demo fleet genuinely reports: a node only appears under a
// vulnerability if its package inventory actually contains the affected
// package. That keeps the Vulnerabilities page consistent with the
// Packages page, which a hand-written list of findings would not be.
//
// The engine is given the demo clock so "first seen" and "last synced"
// sit in the same timeline as the rest of the fleet.
func seedVulnerabilities(ctx context.Context, opts options) error {
	if opts.postgresDSN == "" {
		return fmt.Errorf("no console Postgres DSN: pass --postgres-dsn or set CONSOLE_POSTGRES_DSN (findings have no HTTP write contract, so they are written through the console's own engine)")
	}

	db, err := persistence.Connect(ctx, opts.postgresDSN)
	if err != nil {
		return err
	}
	defer db.Close()

	odb, err := openvoxdb.NewClient(opts.openvoxdbURL, opts.openvoxdbCert, opts.openvoxdbKey, opts.openvoxdbCA)
	if err != nil {
		return err
	}

	// The engine assesses against the real inventory, so the fleet must
	// already be seeded - which it is, this stage runs after it.
	inv, err := vulnerability.LoadInventory(ctx, odb)
	if err != nil {
		return fmt.Errorf("loading inventory for assessment: %w", err)
	}

	store, err := newVulnStore(db)
	if err != nil {
		return err
	}
	engine := vulnerability.NewEngineWithClock(db.Pool, func() time.Time { return demodata.Instant })

	var findings int
	for _, provider := range demodata.DemoProviders {
		// Bounded, because writing findings contends with the console's
		// own sync scheduler for the instance's lease. An unbounded wait
		// here looks exactly like a hang.
		stageCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		id, err := ensureProvider(stageCtx, store, provider)
		if err != nil {
			cancel()
			return describeLeaseContention(err, provider)
		}

		collection := collectionFor(provider.ID, inv)
		err = engine.Apply(stageCtx, id, inv, collection)
		cancel()
		if err != nil {
			return describeLeaseContention(fmt.Errorf("applying findings for %q: %w", provider.DisplayName, err), provider)
		}
		findings += len(collection.Observations)
	}

	fmt.Printf("    %d observations from %d providers\n", findings, len(demodata.DemoProviders))
	return nil
}

// demoSyncInterval is the sync interval the demo provider instances
// carry - see the comment where they are created for why it is absurd.
const demoSyncInterval = 87600 * time.Hour

// describeLeaseContention turns a timeout into the explanation the
// operator actually needs.
//
// The situation it describes is specific and easy to misread as a hang:
// the seed dates a provider's last successful sync to the demo instant,
// months ago. Until the long interval is in place, the console's
// scheduler sees an overdue instance, takes its sync lease, and starts
// fetching the real advisory feed - which can take minutes and ends by
// replacing every seeded finding with real ones for a fleet that does
// not exist. Meanwhile the seed waits for a lease it will not get.
//
// Re-running once the lease lapses fixes it permanently, because the
// seed sets the long interval before it writes anything.
func describeLeaseContention(err error, provider demodata.DemoProvider) error {
	if !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf(`timed out writing findings for %q.

The console is most likely syncing that provider against its real
advisory feed right now, holding the lease this needs. That happens when
a demo provider still carries a realistic sync interval: its last sync is
dated %s, so the scheduler considers it long overdue.

Wait for the sync to finish, then run the seed again. It sets a ten-year
interval before writing, so this only happens once per stack.

To check: select name, expires_at from vulnerability_sync_leases
          join vulnerability_providers on id = provider_id;`,
		provider.DisplayName, demodata.Instant.Format("2006-01-02"))
}

// newVulnStore builds a provider store with the same registry the
// console itself registers.
func newVulnStore(db *persistence.DB) (*vulnerability.Store, error) {
	registry := vulnerability.NewRegistry()
	if err := registry.Register(osv.Type); err != nil {
		return nil, fmt.Errorf("registering provider types: %w", err)
	}
	return vulnerability.NewStore(db.Pool, registry, nil), nil
}

// ensureProvider finds the demo provider instance by name, creating it
// if it is not there yet, and returns its id.
func ensureProvider(ctx context.Context, store *vulnerability.Store, provider demodata.DemoProvider) (string, error) {
	existing, err := store.List(ctx)
	if err != nil {
		return "", fmt.Errorf("listing vulnerability providers: %w", err)
	}
	for _, inst := range existing {
		if inst.Name != provider.DisplayName {
			continue
		}
		// Converge an instance created by an earlier seed onto the
		// current settings - notably the sync interval, whose whole
		// point is keeping the scheduler away from this data.
		enabled := true
		interval := demoSyncInterval
		if _, _, err := store.Update(ctx, inst.ID, vulnerability.UpdateInstance{
			Enabled:      &enabled,
			SyncInterval: &interval,
		}); err != nil {
			return "", fmt.Errorf("updating vulnerability provider %q: %w", provider.DisplayName, err)
		}
		return inst.ID, nil
	}

	created, err := store.Create(ctx, vulnerability.CreateInstance{
		Type:    osv.Type.ID,
		Name:    provider.DisplayName,
		Enabled: true,
		// Deliberately far longer than any real deployment would use.
		//
		// The instance has to be enabled - findings from a disabled
		// provider are excluded from every view, which would empty the
		// pages this data exists to fill. But an enabled instance is a
		// scheduling candidate, and the seed dates its last sync to the
		// demo instant, which is months in the past: with a realistic
		// interval the scheduler would consider it overdue, sync it
		// against the real OSV feed, and - because a sync applies a
		// complete collection - replace every seeded finding with real
		// ones for a fleet that does not exist.
		//
		// A ten-year interval puts the next sync far enough out that it
		// cannot happen during a capture run.
		SyncInterval: demoSyncInterval,
		Config:       vulnerability.Config{"base_url": "https://osv-vulnerabilities.storage.googleapis.com/"},
	})
	if err != nil {
		return "", fmt.Errorf("creating vulnerability provider %q: %w", provider.DisplayName, err)
	}
	return created.ID, nil
}

// collectionFor builds one provider's sync result: an observation for
// every (node, advisory) pair where the node actually has the affected
// package installed.
func collectionFor(providerID string, inv vulnerability.Inventory) vulnerability.Collection {
	advisories := demodata.AdvisoriesFor(providerID)

	byCertname := make(map[string]demodata.Node, len(demodata.Fleet))
	for _, n := range demodata.Fleet {
		byCertname[n.Certname] = n
	}

	var observations []vulnerability.Observation
	var coverage []vulnerability.Coverage

	for _, invNode := range inv.Nodes {
		node, ours := byCertname[invNode.Certname]
		if !ours {
			// A node this seed did not create is reported as not
			// assessed rather than silently omitted, which is what a
			// real provider does for a node it cannot evaluate.
			coverage = append(coverage, vulnerability.Coverage{
				Certname: invNode.Certname,
				Assessed: false,
				Reason:   "not part of the demo fleet",
			})
			continue
		}

		coverage = append(coverage, vulnerability.Coverage{Certname: node.Certname, Assessed: true})

		for _, advisory := range advisories {
			installed, has := demodata.InstalledVersion(node, advisory.Package)
			if !has {
				continue
			}
			observations = append(observations, vulnerability.Observation{
				ObservationKey: vulnerability.ObservationKey{
					Certname: node.Certname,
					VulnID:   advisory.VulnID,
					Package:  advisory.Package,
				},
				RecordID:         advisory.RecordID,
				InstalledVersion: installed,
				FixedVersion:     advisory.FixedVersion,
				FixAvailable:     advisory.FixedVersion != "",
				Severity:         vulnerability.Severity(advisory.Severity),
				URL:              advisory.URL,
			})
		}
	}

	// Complete: this is the provider's whole current view, so applying
	// it replaces anything a previous run recorded. That is what makes
	// this stage converge rather than accumulate.
	return vulnerability.Collection{
		Observations: observations,
		Complete:     true,
		Coverage:     coverage,
		Stats:        map[string]int{"advisories": len(advisories), "observations": len(observations)},
	}
}
