package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/voxpupuli/enterprise-console/internal/demodata"
)

// seedAgentAccess writes the demo agent tokens and the requests they have
// made. There is no API to write these as they would be written for real: a
// token is issued with a signature the seed has no use for, and a request
// is made by an agent. So the registry rows go in directly, and no token
// string exists for anyone to use.
//
// Dates are relative to now, not to the demo Instant: a pending request
// expires on the database's clock, and one dated months ago would be born
// expired. These screenshots therefore show real times, like the
// instances' uptime on the System page, and differ a little between runs.
func seedAgentAccess(ctx context.Context, opts options) error {
	if opts.postgresDSN == "" {
		return fmt.Errorf("no console Postgres DSN: pass --postgres-dsn or set CONSOLE_POSTGRES_DSN (agent tokens and requests are written directly)")
	}
	client, err := newConsoleClient(ctx, opts)
	if err != nil {
		return err
	}
	var groups struct {
		Items []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := client.do(ctx, http.MethodGet, "/api/v1/groups?all=true", nil, &groups); err != nil {
		return fmt.Errorf("listing node groups: %w", err)
	}
	groupIDs := map[string]int64{}
	for _, g := range groups.Items {
		groupIDs[g.Name] = g.ID
	}

	conn, err := pgx.Connect(ctx, opts.postgresDSN)
	if err != nil {
		return fmt.Errorf("connecting to the console database: %w", err)
	}
	defer conn.Close(ctx)

	tokenIDs := map[string]int64{}
	fresh := map[string]bool{}
	for _, t := range demodata.AgentTokens {
		var ids []int64
		for _, name := range t.NodeGroups {
			id, ok := groupIDs[name]
			if !ok {
				return fmt.Errorf("agent token %q reaches group %q, which does not exist", t.Name, name)
			}
			ids = append(ids, id)
		}
		var id int64
		err := conn.QueryRow(ctx,
			`INSERT INTO agent_tokens (name, jti, owner, permissions, node_groups, task_patterns, expires_at)
			 VALUES ($1, $2, $3, $4, $5, '{}', now() + $6 * interval '1 day')
			 ON CONFLICT (name) DO NOTHING RETURNING id`,
			t.Name, "demo-"+t.Name, opts.adminUser, t.Permissions, ids, t.Days).Scan(&id)
		switch {
		case err == nil:
			fresh[t.Name] = true
		case err == pgx.ErrNoRows:
			if err := conn.QueryRow(ctx, `SELECT id FROM agent_tokens WHERE name = $1`, t.Name).Scan(&id); err != nil {
				return fmt.Errorf("finding agent token %q: %w", t.Name, err)
			}
		default:
			return fmt.Errorf("writing agent token %q: %w", t.Name, err)
		}
		tokenIDs[t.Name] = id
	}

	var requests int
	for _, r := range demodata.AgentRequests {
		// Requests are written once, with their token: a second run leaves
		// them as they are, so a decision made in between is not undone.
		if !fresh[r.Token] {
			continue
		}
		created := time.Now().Add(-r.Age)
		expires := created.Add(8 * time.Hour)
		status := r.Status
		var decidedBy *string
		var decidedAt *time.Time
		if status == "rejected" {
			decidedBy = &r.DecidedBy
			at := created.Add(40 * time.Minute)
			decidedAt = &at
		}
		if status == "expired" {
			expires = created.Add(time.Hour)
		}
		if _, err := conn.Exec(ctx,
			`INSERT INTO agent_requests (token_id, token_name, kind, summary, body, targets, status, created_at, expires_at, decided_by, decided_at, note)
			 VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10, $11, $12)`,
			tokenIDs[r.Token], r.Token, r.Kind, r.Summary, r.Body, r.Targets, status, created, expires, decidedBy, decidedAt, r.Note); err != nil {
			return fmt.Errorf("writing a request from %q: %w", r.Token, err)
		}
		requests++
	}
	fmt.Printf("    %d agent tokens, %d requests\n", len(demodata.AgentTokens), requests)
	return nil
}
