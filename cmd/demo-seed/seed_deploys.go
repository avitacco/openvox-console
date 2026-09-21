package main

import (
	"context"
	"fmt"
	"net/http"
)

// deployTarget is one code deployment the seed triggers.
var deployTargets = []struct {
	Source      string
	Environment string
}{
	{Source: "control", Environment: "production"},
	{Source: "team_a", Environment: "production"},
}

// seedDeploys triggers real code deployments through the console's own
// API, against the control repositories `make code-sources-fixture`
// creates.
//
// These are genuine deploys, not fabricated history: g10k really clones
// the fixture repos and really writes an environment. That is worth the
// extra seconds - the Code page is one the site screenshots, and a
// deploy that actually ran is the difference between showing the feature
// and drawing it.
//
// Deployment history is append-only by nature, so converging means "do
// not deploy again if this source already has a successful deploy"
// rather than "update the existing row".
func seedDeploys(ctx context.Context, opts options) error {
	client, err := newConsoleClient(ctx, opts)
	if err != nil {
		return err
	}

	// A source the local stack has not configured is skipped rather than
	// failed: code deployment is optional, and a developer without the
	// fixtures should still get a seeded console.
	var sources []struct {
		Name       string `json:"name"`
		Configured bool   `json:"configured"`
	}
	if err := client.do(ctx, http.MethodGet, "/api/v1/code-deploys/sources", nil, &sources); err != nil {
		return fmt.Errorf("listing code sources: %w", err)
	}
	configured := map[string]bool{}
	for _, s := range sources {
		configured[s.Name] = s.Configured
	}

	var existing struct {
		Items []struct {
			Source string `json:"source"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := client.do(ctx, http.MethodGet, "/api/v1/code-deploys?pageSize=500", nil, &existing); err != nil {
		return fmt.Errorf("listing deploys: %w", err)
	}
	succeeded := map[string]bool{}
	for _, d := range existing.Items {
		if d.Status == "succeeded" {
			succeeded[d.Source] = true
		}
	}

	var ran, skipped int
	for _, target := range deployTargets {
		if !configured[target.Source] {
			fmt.Printf("    skipping source %q - not configured on this stack (run `make code-sources-fixture`)\n", target.Source)
			skipped++
			continue
		}
		if succeeded[target.Source] {
			skipped++
			continue
		}

		body := map[string]string{"source": target.Source, "environment": target.Environment}
		if err := client.do(ctx, http.MethodPost, "/api/v1/code-deploys", body, nil); err != nil {
			return fmt.Errorf("deploying %s/%s: %w", target.Source, target.Environment, err)
		}
		ran++
	}

	fmt.Printf("    %d deploy(s) run, %d already present or unconfigured\n", ran, skipped)
	return nil
}
