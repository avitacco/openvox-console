package app

import (
	"context"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/patchinventory"
	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

func TestPatchRunInventory_ReplacesTheSnapshotAndRecordsFailures(t *testing.T) {
	store := patchinventory.NewStore(testdb.ScratchPool(t))
	at := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	inv := patchRunInventory{store: store, scanner: patchinventory.NewScanner(nil, store), now: func() time.Time { return at }}
	ctx := context.Background()

	// Before the run: two pending updates.
	if err := store.Save(ctx, "web01", patchinventory.ScanResult{Manager: "apt", Updates: []patchinventory.Update{
		{Name: "curl", Available: "2"}, {Name: "libssl3", Available: "4", Security: true},
	}}, at.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	// The scan the run took afterwards: one left.
	if err := inv.SaveScan(ctx, "web01", orchestrator.PatchScan{Manager: "apt", RebootRequired: true, Updates: []orchestrator.PatchPending{{Name: "curl", Installed: "1", Available: "2"}}}); err != nil {
		t.Fatal(err)
	}
	snap, updates, err := store.Get(ctx, "web01")
	if err != nil || snap == nil {
		t.Fatalf("%v %v", snap, err)
	}
	if snap.UpdateCount != 1 || snap.SecurityCount != 0 || !snap.RebootRequired || len(updates) != 1 || !snap.ScannedAt.Equal(at) {
		t.Errorf("snapshot = %+v updates %+v", snap, updates)
	}

	// A node with no transport is not reached, which is not a failure.
	if reached, err := inv.Rescan(ctx, "web01"); reached || err != nil {
		t.Errorf("Rescan = %v, %v; want not reached", reached, err)
	}

	// A node that did not return: its snapshot says so and keeps its updates.
	if err := inv.RecordScanFailure(ctx, "web01", "did not come back"); err != nil {
		t.Fatal(err)
	}
	snap, updates, _ = store.Get(ctx, "web01")
	if snap.Error != "did not come back" || len(updates) != 1 {
		t.Errorf("after failure: %+v updates %d", snap, len(updates))
	}
}
