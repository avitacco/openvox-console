package app

import (
	"context"
	"errors"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/patchinventory"
)

// patchRunInventory lets patch runs refresh the patch inventory: it
// stores the scan a finished run took and scans a rebooted node again.
type patchRunInventory struct {
	store   *patchinventory.Store
	scanner *patchinventory.Scanner
	now     func() time.Time
}

var _ orchestrator.PatchInventory = patchRunInventory{}

func (p patchRunInventory) SaveScan(ctx context.Context, certname string, scan orchestrator.PatchScan) error {
	if scan.Unsupported {
		return p.store.RecordFailure(ctx, certname, "this node has no package manager the scan supports (apt, dnf or yum)", true, p.now())
	}
	updates := make([]patchinventory.Update, 0, len(scan.Updates))
	for _, u := range scan.Updates {
		updates = append(updates, patchinventory.Update{Name: u.Name, Installed: u.Installed, Available: u.Available, Security: u.Security})
	}
	return p.store.Save(ctx, certname, patchinventory.ScanResult{Manager: scan.Manager, RebootRequired: scan.RebootRequired, Updates: updates}, p.now())
}

func (p patchRunInventory) Rescan(ctx context.Context, certname string) (bool, error) {
	res, err := p.scanner.ScanNode(ctx, certname)
	if err != nil {
		return true, err
	}
	switch res.Outcome {
	case patchinventory.OutcomeOffline:
		return false, nil
	case patchinventory.OutcomeScanned:
		return true, nil
	}
	return true, errors.New(res.Error)
}

func (p patchRunInventory) RecordScanFailure(ctx context.Context, certname, message string) error {
	return p.store.RecordFailure(ctx, certname, message, false, p.now())
}
