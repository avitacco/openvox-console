// Package agentupgrade keeps node-agent-client current across the
// fleet: it records which client build each node runs, decides which
// are out of date against the build this console embeds, and tells
// out-of-date clients to upgrade. See design.md in
// add-agent-auto-upgrade.
package agentupgrade

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Upgrade states stored per node - see the migration for what each
// means.
const (
	UpgradeIdle      = "idle"
	UpgradeRequested = "requested"
	UpgradeFailed    = "failed"
	UpgradeExhausted = "exhausted"
)

// Status is a client's answer to a build-identity request.
type Status struct {
	Digest          string
	BuildTime       *time.Time // nil for a client built without a stamp
	Version         string
	OS              string
	Arch            string
	InstallMethod   string
	AcceptsUpgrades bool
}

// Record is everything stored about one node's client.
type Record struct {
	Certname string
	// Capable is false for a client that does not support build
	// reporting at all. The Status fields are then empty.
	Capable bool
	Status
	ReportedAt time.Time

	UpgradeState  string
	TargetDigest  string
	Attempts      int
	LastError     string
	RequestedAt   *time.Time
	NextAttemptAt *time.Time
}

// Store is the per-node client state. *PgStore is the real one.
type Store interface {
	// RecordStatus stores what a node reported. A nil status records a
	// client that cannot report (Capable false). Reporting the build an
	// upgrade was aiming for completes that upgrade and clears its
	// failure history.
	RecordStatus(ctx context.Context, certname string, status *Status) error
	// Claim moves a node into the requested state for targetDigest and
	// reports whether this caller did so, with the attempts already
	// made at that digest. It refuses when another caller holds the
	// node, and - unless force - when limit nodes are already being
	// upgraded, when attempts at this digest are exhausted, or when the
	// node's next attempt is not yet due.
	Claim(ctx context.Context, certname, targetDigest string, limit int, force bool, now time.Time) (won bool, attempts int, err error)
	// Release returns a claimed node to idle without counting an
	// attempt, due again at retryAt.
	Release(ctx context.Context, certname string, retryAt time.Time) error
	// Fail records a failed attempt on a claimed node.
	Fail(ctx context.Context, certname, reason string, exhausted bool, retryAt time.Time) error
	// Get returns one node's record, or nil when it has none.
	Get(ctx context.Context, certname string) (*Record, error)
	// List returns every record.
	List(ctx context.Context) ([]Record, error)
}

// PgStore is Store on Postgres.
type PgStore struct {
	pool *pgxpool.Pool
}

// NewStore builds a PgStore backed by pool.
func NewStore(pool *pgxpool.Pool) *PgStore {
	return &PgStore{pool: pool}
}

// claimLockKey serializes claims across console instances, so the count
// of nodes being upgraded cannot be read by two claimers at once and
// exceeded by both. Arbitrary, fixed, and used for nothing else.
const claimLockKey = 0x61677570 // "agup"

func (s *PgStore) RecordStatus(ctx context.Context, certname string, status *Status) error {
	st := Status{AcceptsUpgrades: true}
	capable := status != nil
	if capable {
		st = *status
	}
	// "reached" is the upgrade completing: the node reports the build
	// it was told to install. It is also true when someone re-ran the
	// install script on a node whose upgrades had failed, which should
	// clear the failure just the same.
	_, err := s.pool.Exec(ctx, `
		INSERT INTO node_agents
			(certname, capable, digest, build_time, version, os, arch, install_method, accepts_upgrades, reported_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
		ON CONFLICT (certname) DO UPDATE SET
			capable = EXCLUDED.capable,
			digest = EXCLUDED.digest,
			build_time = EXCLUDED.build_time,
			version = EXCLUDED.version,
			os = EXCLUDED.os,
			arch = EXCLUDED.arch,
			install_method = EXCLUDED.install_method,
			accepts_upgrades = EXCLUDED.accepts_upgrades,
			reported_at = EXCLUDED.reported_at,
			upgrade_state = CASE WHEN `+reached+` THEN 'idle' ELSE node_agents.upgrade_state END,
			attempts = CASE WHEN `+reached+` THEN 0 ELSE node_agents.attempts END,
			last_error = CASE WHEN `+reached+` THEN '' ELSE node_agents.last_error END,
			requested_at = CASE WHEN `+reached+` THEN NULL ELSE node_agents.requested_at END,
			next_attempt_at = CASE WHEN `+reached+` THEN NULL ELSE node_agents.next_attempt_at END`,
		certname, capable, st.Digest, st.BuildTime, st.Version, st.OS, st.Arch, st.InstallMethod, st.AcceptsUpgrades)
	if err != nil {
		return fmt.Errorf("record client build for %q: %w", certname, err)
	}
	return nil
}

// reached is true in RecordStatus's upsert when the reported digest is
// the one an upgrade was aiming for.
const reached = `(node_agents.target_digest <> '' AND EXCLUDED.digest = node_agents.target_digest)`

func (s *PgStore) Claim(ctx context.Context, certname, targetDigest string, limit int, force bool, now time.Time) (bool, int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, 0, fmt.Errorf("claim upgrade of %q: %w", certname, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(claimLockKey)); err != nil {
		return false, 0, fmt.Errorf("claim upgrade of %q: %w", certname, err)
	}

	// attempts restarts from zero for a digest not tried before (the
	// console now serves a different build) and for a forced claim (an
	// operator asked). The row is only ever taken from a state other
	// than 'requested', which is the whole claim.
	var attempts int
	err = tx.QueryRow(ctx, `
		UPDATE node_agents SET
			upgrade_state = 'requested',
			requested_at = $4,
			next_attempt_at = NULL,
			attempts = CASE WHEN $5 OR target_digest <> $2 THEN 0 ELSE attempts END,
			target_digest = $2
		WHERE certname = $1
		  AND upgrade_state <> 'requested'
		  AND ($5 OR target_digest <> $2 OR (
		        upgrade_state <> 'exhausted'
		        AND (next_attempt_at IS NULL OR next_attempt_at <= $4)))
		  AND ($5 OR (SELECT count(*) FROM node_agents WHERE upgrade_state = 'requested') < $3)
		RETURNING attempts`,
		certname, targetDigest, limit, now, force).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("claim upgrade of %q: %w", certname, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, 0, fmt.Errorf("claim upgrade of %q: %w", certname, err)
	}
	return true, attempts, nil
}

func (s *PgStore) Release(ctx context.Context, certname string, retryAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE node_agents SET upgrade_state = 'idle', requested_at = NULL, next_attempt_at = $2
		WHERE certname = $1 AND upgrade_state = 'requested'`, certname, retryAt)
	if err != nil {
		return fmt.Errorf("release upgrade of %q: %w", certname, err)
	}
	return nil
}

func (s *PgStore) Fail(ctx context.Context, certname, reason string, exhausted bool, retryAt time.Time) error {
	state := UpgradeFailed
	if exhausted {
		state = UpgradeExhausted
	}
	// Only a row still in 'requested': if the node reported the new
	// build in the meantime, or another instance already recorded this
	// failure, there is nothing to record.
	_, err := s.pool.Exec(ctx, `
		UPDATE node_agents SET
			upgrade_state = $2, attempts = attempts + 1, last_error = $3,
			requested_at = NULL, next_attempt_at = $4
		WHERE certname = $1 AND upgrade_state = 'requested'`, certname, state, reason, retryAt)
	if err != nil {
		return fmt.Errorf("record failed upgrade of %q: %w", certname, err)
	}
	return nil
}

const recordColumns = `certname, capable, digest, build_time, version, os, arch, install_method,
	accepts_upgrades, reported_at, upgrade_state, target_digest, attempts, last_error, requested_at, next_attempt_at`

func scanRecord(row pgx.Row) (Record, error) {
	var r Record
	err := row.Scan(&r.Certname, &r.Capable, &r.Digest, &r.BuildTime, &r.Version, &r.OS, &r.Arch, &r.InstallMethod,
		&r.AcceptsUpgrades, &r.ReportedAt, &r.UpgradeState, &r.TargetDigest, &r.Attempts, &r.LastError,
		&r.RequestedAt, &r.NextAttemptAt)
	return r, err
}

func (s *PgStore) Get(ctx context.Context, certname string) (*Record, error) {
	r, err := scanRecord(s.pool.QueryRow(ctx, `SELECT `+recordColumns+` FROM node_agents WHERE certname = $1`, certname))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // no record is a normal answer, not an error
	}
	if err != nil {
		return nil, fmt.Errorf("read client build for %q: %w", certname, err)
	}
	return &r, nil
}

func (s *PgStore) List(ctx context.Context) ([]Record, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+recordColumns+` FROM node_agents`)
	if err != nil {
		return nil, fmt.Errorf("list client builds: %w", err)
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("list client builds: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
