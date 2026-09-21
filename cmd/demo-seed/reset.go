package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/demodata"
	"github.com/voxpupuli/enterprise-console/internal/persistence"
)

// resetFleet removes every demo node from openvoxdb, so the next seed
// writes into empty space.
//
// This exists because openvoxdb deduplicates reports by content hash and
// the seed is deterministic: once a report is stored, submitting it again
// is a no-op. That is ordinarily what you want, but it means a fleet
// stored under the wrong configuration - notably one whose resource
// events were dropped by retention - cannot be repaired by re-running the
// seed. Deleting first is the only way back.
//
// It deletes only the certnames the demo fleet defines. A node somebody
// else put in openvoxdb is left alone: this is a development stack, and
// wiping data the tool did not create is not its business.
//
// openvoxdb's admin API is used rather than its command API because
// "deactivate node" only hides a node from queries - its reports stay,
// and so does the dedup entry that causes the problem. The admin delete
// removes the node and its data outright.
func resetFleet(ctx context.Context, opts options) error {
	admin, err := newAdminClient(opts)
	if err != nil {
		return err
	}

	var deleted int
	for _, node := range demodata.Fleet {
		ok, err := admin.deleteNode(ctx, node.Certname)
		if err != nil {
			return fmt.Errorf("deleting %s: %w", node.Certname, err)
		}
		if ok {
			deleted++
		}
	}

	fmt.Printf("    removed %d previously seeded node(s) from openvoxdb\n", deleted)

	return resetVulnProviders(ctx, opts)
}

// resetVulnProviders deletes the demo vulnerability provider instances,
// so the seed recreates them from scratch.
//
// This is what repairs a stack whose demo providers were created with a
// realistic sync interval: while such an instance exists, the console's
// scheduler keeps taking its sync lease (its last sync is dated months
// ago), and the seed cannot get the lease to correct the interval.
// Deleting the instance breaks that standoff - the replacement is
// created with an interval far enough out that the scheduler never looks
// at it.
//
// Only the instances this seed creates are touched, matched by name. A
// provider somebody configured themselves is left alone.
func resetVulnProviders(ctx context.Context, opts options) error {
	if opts.postgresDSN == "" {
		// Findings live in the console's database; without a DSN there
		// is nothing to reset, and the seeding stage will explain that
		// more usefully than a failure here would.
		return nil
	}

	db, err := persistence.Connect(ctx, opts.postgresDSN)
	if err != nil {
		return err
	}
	defer db.Close()

	store, err := newVulnStore(db)
	if err != nil {
		return err
	}
	existing, err := store.List(ctx)
	if err != nil {
		return fmt.Errorf("listing vulnerability providers: %w", err)
	}

	ours := make(map[string]bool, len(demodata.DemoProviders))
	for _, p := range demodata.DemoProviders {
		ours[p.DisplayName] = true
	}

	var deleted int
	for _, inst := range existing {
		if !ours[inst.Name] {
			continue
		}
		// Bounded: an in-flight sync holds row locks on the instance it
		// is syncing, so this delete can block for as long as that sync
		// takes - which, against a real advisory feed, is minutes. An
		// unbounded wait is indistinguishable from a hang.
		deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := store.Delete(deleteCtx, inst.ID)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf(`timed out deleting vulnerability provider %q.

The console is syncing it right now and holds the row. Wait for that sync
to finish and run --reset again; the replacement instance is created with
a ten-year sync interval, so it will not happen a second time`, inst.Name)
			}
			return fmt.Errorf("deleting vulnerability provider %q: %w", inst.Name, err)
		}
		deleted++
	}

	if deleted > 0 {
		fmt.Printf("    removed %d previously seeded vulnerability provider(s)\n", deleted)
	}
	return nil
}

// adminClient talks to openvoxdb's admin API.
//
// It is a separate, deliberately tiny client rather than a method on
// internal/openvoxdb's Client: hard-deleting a node is not something the
// console does or should be able to do, and the product's openvoxdb
// package should not grow an operation only a development tool wants.
type adminClient struct {
	baseURL string
	http    *http.Client
}

func newAdminClient(opts options) (*adminClient, error) {
	cert, err := tls.LoadX509KeyPair(opts.openvoxdbCert, opts.openvoxdbKey)
	if err != nil {
		return nil, fmt.Errorf("load openvoxdb client certificate: %w", err)
	}
	caPEM, err := os.ReadFile(opts.openvoxdbCA)
	if err != nil {
		return nil, fmt.Errorf("read openvoxdb CA certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no certificates found in openvoxdb CA file %s", opts.openvoxdbCA)
	}

	return &adminClient{
		baseURL: strings.TrimSuffix(opts.openvoxdbURL, "/"),
		http: &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: pool},
		}},
	}, nil
}

// deleteNode removes certname and everything openvoxdb holds for it,
// reporting whether there was anything there to remove.
func (c *adminClient) deleteNode(ctx context.Context, certname string) (bool, error) {
	body, err := json.Marshal(map[string]any{
		"command": "delete",
		"version": 1,
		"payload": map[string]string{"certname": certname},
	})
	if err != nil {
		return false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/pdb/admin/v1/cmd", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("openvoxdb unreachable: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("openvoxdb admin delete failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	// openvoxdb answers {"deleted": "<certname>"} whether or not it held
	// anything, so an unknown certname is a success with nothing done.
	var result struct {
		Deleted string `json:"deleted"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return false, fmt.Errorf("parse openvoxdb admin response: %w", err)
	}
	return result.Deleted == certname, nil
}
