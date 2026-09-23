// Package leases coordinates work that must run on exactly one console
// instance at a time.
//
// Clustering makes this necessary rather than merely tidy. Scheduled
// background work - a vulnerability sync, a code deploy - is written
// assuming one runner; with several instances running the same schedule,
// each would do it independently, producing duplicate provider fetches,
// racing writes, and in the deploy case two g10k runs against the same
// directory.
//
// Expiry is judged by the database clock, not by any instance's, so
// instances with skewed clocks still agree on whether a lease is live.
// That matters more than it sounds: a lease judged by a fast local clock
// would be considered expired early and taken over while its holder was
// still working.
//
// A lease is not a guarantee of mutual exclusion under arbitrary failure
// - a holder that stalls for longer than the TTL loses it while still
// running. Hold narrows that window by cancelling the work's context as
// soon as a renewal fails, so the previous holder stops rather than
// continuing unaware. Work that must never overlap needs its own
// idempotency on top of this.
package leases

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultTTL is how long a lease lasts between renewals.
const DefaultTTL = 2 * time.Minute

// Leases coordinates which console instance performs a named unit of
// work.
type Leases struct {
	pool   *pgxpool.Pool
	holder string
	ttl    time.Duration
}

// New builds Leases held under a name unique to this process, so two
// instances - including two on the same host - never mistake each
// other's lease for their own.
func New(pool *pgxpool.Pool, ttl time.Duration) *Leases {
	host, _ := os.Hostname()
	return &Leases{
		pool:   pool,
		holder: fmt.Sprintf("%s/%d/%s", host, os.Getpid(), uuid.NewString()),
		ttl:    ttl,
	}
}

// Holder returns this process's lease-holder identity, for logging and
// for a test that needs to tell two holders apart.
func (l *Leases) Holder() string { return l.holder }

// Acquire takes name's lease if it is free or expired, reporting whether
// this holder now has it.
func (l *Leases) Acquire(ctx context.Context, name string) (bool, error) {
	tag, err := l.pool.Exec(ctx,
		`INSERT INTO singleton_leases (name, holder, expires_at)
		 VALUES ($1, $2, now() + $3 * interval '1 millisecond')
		 ON CONFLICT (name) DO UPDATE SET holder = EXCLUDED.holder, expires_at = EXCLUDED.expires_at
		 WHERE singleton_leases.expires_at < now()`,
		name, l.holder, l.ttl.Milliseconds())
	if err != nil {
		return false, fmt.Errorf("acquire lease %q: %w", name, err)
	}
	return tag.RowsAffected() == 1, nil
}

// Renew extends the lease if this holder still has it, reporting whether
// it did.
func (l *Leases) Renew(ctx context.Context, name string) (bool, error) {
	tag, err := l.pool.Exec(ctx,
		`UPDATE singleton_leases SET expires_at = now() + $3 * interval '1 millisecond'
		 WHERE name = $1 AND holder = $2 AND expires_at > now()`,
		name, l.holder, l.ttl.Milliseconds())
	if err != nil {
		return false, fmt.Errorf("renew lease %q: %w", name, err)
	}
	return tag.RowsAffected() == 1, nil
}

// Release gives up the lease if this holder has it.
func (l *Leases) Release(ctx context.Context, name string) error {
	if _, err := l.pool.Exec(ctx,
		`DELETE FROM singleton_leases WHERE name = $1 AND holder = $2`, name, l.holder); err != nil {
		return fmt.Errorf("release lease %q: %w", name, err)
	}
	return nil
}

// Held reports whether any holder has an unexpired lease on name - that
// is, whether the work is in progress somewhere in the cluster.
func (l *Leases) Held(ctx context.Context, name string) (bool, error) {
	var held bool
	if err := l.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM singleton_leases WHERE name = $1 AND expires_at > now())`,
		name).Scan(&held); err != nil {
		return false, fmt.Errorf("check lease %q: %w", name, err)
	}
	return held, nil
}

// Hold runs fn while holding name's lease, renewing it every third of
// the TTL. If a renewal fails - the database is unreachable, or another
// holder took over an expired lease - fn's context is cancelled, so the
// work stops rather than continuing without the lease it assumed.
//
// acquired is false (and fn not run) when the lease was already held
// elsewhere.
func (l *Leases) Hold(ctx context.Context, name string, fn func(context.Context) error) (acquired bool, err error) {
	ok, err := l.Acquire(ctx, name)
	if err != nil || !ok {
		return false, err
	}

	holdCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan struct{})
	renewerDone := make(chan struct{})
	go func() {
		defer close(renewerDone)
		ticker := time.NewTicker(l.ttl / 3)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-holdCtx.Done():
				return
			case <-ticker.C:
				renewed, err := l.Renew(holdCtx, name)
				if err != nil || !renewed {
					cancel()
					return
				}
			}
		}
	}()

	err = fn(holdCtx)
	close(done)
	<-renewerDone

	// WithoutCancel so the release still happens when fn returned
	// because ctx was cancelled - otherwise a shutdown would leave the
	// lease to expire on its own, blocking another instance for a TTL.
	_ = l.Release(context.WithoutCancel(ctx), name)
	return true, err
}
