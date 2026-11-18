package patchinventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
)

// scanTimeout bounds one node's scan. Refreshing package lists on a slow
// mirror takes minutes, and a scan that outlives it is a failed scan.
const scanTimeout = 5 * time.Minute

// DefaultWorkers is how many nodes a group or periodic scan runs at once.
const DefaultWorkers = 8

// Transport is the subset of *nodetransport.Dispatcher a scan needs.
type Transport interface {
	Dispatch(ctx context.Context, certname string, payload []byte, timeout time.Duration) ([]byte, error)
}

// Outcome is how one node's scan went.
type Outcome string

const (
	OutcomeScanned     Outcome = "scanned"
	OutcomeFailed      Outcome = "failed"
	OutcomeOffline     Outcome = "offline"
	OutcomeUnsupported Outcome = "unsupported"
)

// NodeResult is one node's outcome, with its new counts when scanned.
type NodeResult struct {
	Certname string  `json:"certname"`
	Outcome  Outcome `json:"outcome"`
	Error    string  `json:"error,omitempty"`
	Updates  int     `json:"updates"`
	Security int     `json:"security"`
}

// Wire types mirror internal/nodeagent's copies: the JSON is the contract.
type request struct {
	Module string          `json:"module"`
	Action string          `json:"action"`
	Params json.RawMessage `json:"params,omitempty"`
}

type wireResponse struct {
	Result *struct {
		PatchScan *struct {
			Manager        string   `json:"manager"`
			Unsupported    bool     `json:"unsupported"`
			RebootRequired bool     `json:"reboot_required"`
			Updates        []Update `json:"updates"`
		} `json:"patch_scan"`
	} `json:"result"`
	Error string `json:"error"`
}

// Scanner scans nodes and stores what they report.
type Scanner struct {
	transport Transport
	store     *Store
	workers   int
	now       func() time.Time
}

// NewScanner builds a Scanner. transport may be nil, in which case every
// node is offline.
func NewScanner(transport Transport, store *Store) *Scanner {
	return &Scanner{transport: transport, store: store, workers: DefaultWorkers, now: time.Now}
}

// ScanNode asks one node for its pending updates and stores the answer.
// The only error returned is a failure to store; a node that cannot be
// scanned is reported in the result.
func (s *Scanner) ScanNode(ctx context.Context, certname string) (NodeResult, error) {
	res := NodeResult{Certname: certname}
	if s.transport == nil {
		res.Outcome = OutcomeOffline
		return res, nil
	}
	payload, _ := json.Marshal(request{Module: "puppet", Action: "patch_scan"})
	raw, err := s.transport.Dispatch(ctx, certname, payload, scanTimeout)
	switch {
	case errors.Is(err, nodetransport.ErrNodeNotConnected):
		res.Outcome = OutcomeOffline // nothing changes; it is retried when it connects
		return res, nil
	case err != nil:
		return s.fail(ctx, res, fmt.Sprintf("the scan did not complete: %v", err), false)
	}

	var w wireResponse
	if err := json.Unmarshal(raw, &w); err != nil {
		return s.fail(ctx, res, "the node's answer could not be read: "+err.Error(), false)
	}
	if strings.HasPrefix(w.Error, `unsupported action "patch_scan"`) {
		return s.fail(ctx, res, "this node's agent is too old to scan for updates; upgrade it", true)
	}
	if w.Error != "" {
		return s.fail(ctx, res, w.Error, false)
	}
	if w.Result == nil || w.Result.PatchScan == nil {
		return s.fail(ctx, res, "the node's answer had no scan result", false)
	}
	ps := w.Result.PatchScan
	if ps.Unsupported {
		return s.fail(ctx, res, "this node has no package manager the scan supports (apt, dnf or yum)", true)
	}
	if err := s.store.Save(ctx, certname, ScanResult{Manager: ps.Manager, RebootRequired: ps.RebootRequired, Updates: ps.Updates}, s.now()); err != nil {
		return res, err
	}
	res.Outcome = OutcomeScanned
	snap, _, err := s.store.Get(ctx, certname)
	if err != nil {
		return res, err
	}
	if snap != nil {
		res.Updates, res.Security = snap.UpdateCount, snap.SecurityCount
	}
	return res, nil
}

func (s *Scanner) fail(ctx context.Context, res NodeResult, message string, unsupported bool) (NodeResult, error) {
	res.Error = message
	res.Outcome = OutcomeFailed
	if unsupported {
		res.Outcome = OutcomeUnsupported
	}
	if err := s.store.RecordFailure(ctx, res.Certname, message, unsupported, s.now()); err != nil {
		return res, err
	}
	return res, nil
}

// ScanMany scans certnames with a bounded number at once and returns each
// node's result in the order given. A storage failure fails that node.
func (s *Scanner) ScanMany(ctx context.Context, certnames []string) []NodeResult {
	results := make([]NodeResult, len(certnames))
	sem := make(chan struct{}, max(1, s.workers))
	var wg sync.WaitGroup
	for i, c := range certnames {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			r, err := s.ScanNode(ctx, c)
			if err != nil {
				r = NodeResult{Certname: c, Outcome: OutcomeFailed, Error: err.Error()}
			}
			results[i] = r
		}()
	}
	wg.Wait()
	return results
}
