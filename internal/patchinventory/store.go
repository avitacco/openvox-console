// Package patchinventory records which updates each node is missing, from
// a read-only scan the node agent runs, and serves it (add-patch-inventory).
//
// It only reads. Nothing here installs, upgrades or removes a package.
package patchinventory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Interval limits for the periodic scan.
const (
	MinInterval     = 15 * time.Minute
	MaxInterval     = 7 * 24 * time.Hour
	DefaultInterval = 24 * time.Hour
)

// Update is one pending update.
type Update struct {
	Name      string `json:"name"`
	Installed string `json:"installed,omitempty"`
	Available string `json:"available"`
	Security  bool   `json:"security"`
}

// ScanResult is what a successful scan reports.
type ScanResult struct {
	Manager        string
	RebootRequired bool
	Updates        []Update
}

// Snapshot is a node's latest stored scan. ScannedAt is nil when no scan
// has ever succeeded; Error is the last attempt's failure, if it failed.
type Snapshot struct {
	Certname       string     `json:"certname"`
	Manager        string     `json:"manager"`
	RebootRequired bool       `json:"rebootRequired"`
	UpdateCount    int        `json:"updates"`
	SecurityCount  int        `json:"security"`
	ScannedAt      *time.Time `json:"scannedAt"`
	AttemptedAt    time.Time  `json:"attemptedAt"`
	Error          string     `json:"error,omitempty"`
	Unsupported    bool       `json:"unsupported,omitempty"`
}

// Match is a node that has a package pending.
type Match struct {
	Certname  string `json:"certname"`
	Name      string `json:"name"`
	Installed string `json:"installed,omitempty"`
	Available string `json:"available"`
	Security  bool   `json:"security"`
}

// Settings are the periodic scan's settings.
type Settings struct {
	Enabled         bool          `json:"enabled"`
	IntervalMinutes int           `json:"intervalMinutes"`
	Interval        time.Duration `json:"-"`
}

// ErrInterval is returned for an interval outside the allowed range.
var ErrInterval = errors.New("the scan interval must be between 15 minutes and 7 days")

// Store is the Postgres store.
type Store struct{ pool *pgxpool.Pool }

// NewStore builds a Store.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Save records a successful scan, replacing the node's previous updates
// in one transaction.
func (s *Store) Save(ctx context.Context, certname string, res ScanResult, at time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		security := 0
		seen := map[string]bool{}
		updates := make([]Update, 0, len(res.Updates))
		for _, u := range res.Updates {
			if seen[u.Name] { // one row per package; the first wins
				continue
			}
			seen[u.Name] = true
			updates = append(updates, u)
			if u.Security {
				security++
			}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO node_patch_scans (certname, manager, reboot_required, update_count, security_count,
			                              scanned_at, attempted_at, error, unsupported)
			 VALUES ($1, $2, $3, $4, $5, $6, $6, '', FALSE)
			 ON CONFLICT (certname) DO UPDATE SET manager = $2, reboot_required = $3, update_count = $4,
			   security_count = $5, scanned_at = $6, attempted_at = $6, error = '', unsupported = FALSE`,
			certname, res.Manager, res.RebootRequired, len(updates), security, at); err != nil {
			return fmt.Errorf("save patch scan: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM node_patch_updates WHERE certname = $1`, certname); err != nil {
			return fmt.Errorf("clear patch updates: %w", err)
		}
		if len(updates) == 0 {
			return nil
		}
		batch := &pgx.Batch{}
		for _, u := range updates {
			batch.Queue(`INSERT INTO node_patch_updates (certname, name, installed, available, security) VALUES ($1,$2,$3,$4,$5)`,
				certname, u.Name, u.Installed, u.Available, u.Security)
		}
		br := tx.SendBatch(ctx, batch)
		defer br.Close()
		for range updates {
			if _, err := br.Exec(); err != nil {
				return fmt.Errorf("save patch update: %w", err)
			}
		}
		return br.Close()
	})
}

// RecordFailure records a failed attempt, keeping the last good updates.
// unsupported marks a node the scan cannot cover; it clears any older
// updates, since nothing says they still apply.
func (s *Store) RecordFailure(ctx context.Context, certname, message string, unsupported bool, at time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO node_patch_scans (certname, attempted_at, error, unsupported) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (certname) DO UPDATE SET attempted_at = $2, error = $3, unsupported = $4`,
			certname, at, message, unsupported); err != nil {
			return fmt.Errorf("record patch scan failure: %w", err)
		}
		if unsupported {
			if _, err := tx.Exec(ctx, `DELETE FROM node_patch_updates WHERE certname = $1`, certname); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`UPDATE node_patch_scans SET update_count = 0, security_count = 0, scanned_at = NULL WHERE certname = $1`, certname); err != nil {
				return err
			}
		}
		return nil
	})
}

// Get returns a node's snapshot and updates, or nil when it was never
// scanned or attempted. Both are read in one snapshot-isolated
// transaction, so a scan landing between them cannot give a summary that
// disagrees with its list.
func (s *Store) Get(ctx context.Context, certname string) (*Snapshot, []Update, error) {
	var (
		snap    *Snapshot
		updates []Update
	)
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, snapshotSelect+` WHERE certname = $1`, certname)
		if err != nil {
			return err
		}
		snaps, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Snapshot])
		if err != nil {
			return err
		}
		if len(snaps) == 0 {
			return nil
		}
		snap = &snaps[0]
		rows, err = tx.Query(ctx,
			`SELECT name, installed, available, security FROM node_patch_updates WHERE certname = $1
			 ORDER BY security DESC, name`, certname)
		if err != nil {
			return err
		}
		updates, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Update])
		return err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("read patch snapshot: %w", err)
	}
	return snap, updates, nil
}

const snapshotSelect = `SELECT certname, manager, reboot_required, update_count, security_count, scanned_at, attempted_at, error, unsupported
	      FROM node_patch_scans`

// Snapshots reads the snapshots of certnames, or of every node when
// certnames is nil.
func (s *Store) Snapshots(ctx context.Context, certnames []string) (map[string]Snapshot, error) {
	q := snapshotSelect
	args := []any{}
	if certnames != nil {
		q += ` WHERE certname = ANY($1)`
		args = append(args, certnames)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("read patch snapshots: %w", err)
	}
	snaps, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Snapshot])
	if err != nil {
		return nil, fmt.Errorf("read patch snapshots: %w", err)
	}
	out := make(map[string]Snapshot, len(snaps))
	for _, sn := range snaps {
		out[sn.Certname] = sn
	}
	return out, nil
}

// Search lists the nodes with a package (name contains) pending, limited
// to certnames unless that is nil.
func (s *Store) Search(ctx context.Context, name string, certnames []string) ([]Match, error) {
	q := `SELECT certname, name, installed, available, security FROM node_patch_updates
	      WHERE name ILIKE '%' || $1 || '%'`
	args := []any{escapeLike(name)}
	if certnames != nil {
		q += ` AND certname = ANY($2)`
		args = append(args, certnames)
	}
	q += ` ORDER BY name, certname`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search pending updates: %w", err)
	}
	matches, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Match])
	if err != nil {
		return nil, fmt.Errorf("search pending updates: %w", err)
	}
	return matches, nil
}

func escapeLike(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if c := s[i]; c == '\\' || c == '%' || c == '_' {
			out = append(out, '\\')
		}
		out = append(out, s[i])
	}
	return string(out)
}

// Settings reads the periodic scan's settings.
func (s *Store) Settings(ctx context.Context) (Settings, error) {
	var st Settings
	if err := s.pool.QueryRow(ctx, `SELECT enabled, interval_minutes FROM patch_scan_settings`).Scan(&st.Enabled, &st.IntervalMinutes); err != nil {
		return Settings{}, fmt.Errorf("read patch scan settings: %w", err)
	}
	st.Interval = time.Duration(st.IntervalMinutes) * time.Minute
	return st, nil
}

// SetSettings validates and stores the settings.
func (s *Store) SetSettings(ctx context.Context, enabled bool, intervalMinutes int) (Settings, error) {
	d := time.Duration(intervalMinutes) * time.Minute
	if d < MinInterval || d > MaxInterval {
		return Settings{}, ErrInterval
	}
	if _, err := s.pool.Exec(ctx, `UPDATE patch_scan_settings SET enabled = $1, interval_minutes = $2`, enabled, intervalMinutes); err != nil {
		return Settings{}, fmt.Errorf("store patch scan settings: %w", err)
	}
	return s.Settings(ctx)
}

// LastAttempts returns when each node was last tried, for the periodic
// pass to find those that are due.
func (s *Store) LastAttempts(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.pool.Query(ctx, `SELECT certname, attempted_at FROM node_patch_scans`)
	if err != nil {
		return nil, fmt.Errorf("read patch attempts: %w", err)
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var c string
		var at time.Time
		if err := rows.Scan(&c, &at); err != nil {
			return nil, err
		}
		out[c] = at
	}
	return out, rows.Err()
}

// Purge removes snapshots of nodes not attempted since before, returning
// how many it removed.
func (s *Store) Purge(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM node_patch_scans WHERE attempted_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("purge patch snapshots: %w", err)
	}
	return tag.RowsAffected(), nil
}
