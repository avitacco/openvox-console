package rbac

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// RevocationSubject is the NATS subject revocation events are published
// on. Every running instance subscribes to it, so a revocation issued on
// one instance is reflected on all of them without a restart - see
// design.md.
const RevocationSubject = "rbac.revoked"

// publisher is the subset of *messaging.Bus this package needs, so it can
// be tested without a live NATS server.
type publisher interface {
	Publish(subject string, data []byte) error
	Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error)
}

// revocationEvent is the payload published on RevocationSubject.
type revocationEvent struct {
	JTI       string    `json:"jti"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// revokedTokenStore is the subset of *Store the Revoker needs.
type revokedTokenStore interface {
	PersistRevocation(ctx context.Context, jti string, expiresAt time.Time) error
	UnexpiredRevocations(ctx context.Context) ([]revocationEvent, error)
}

// Revoker tracks revoked token jtis in memory for zero-latency,
// zero-database-call checks on the request hot path. Postgres is the
// source of truth (durable, survives restarts); NATS propagates a
// revocation to every other already-running instance immediately.
type Revoker struct {
	store publisher
	db    revokedTokenStore

	mu      sync.RWMutex
	revoked map[string]time.Time // jti -> expiry, for eventual cleanup
}

// NewRevoker builds a Revoker backed by bus (for propagation) and db (for
// durability). Call Start to subscribe and load existing revocations
// before serving requests.
func NewRevoker(bus publisher, db revokedTokenStore) *Revoker {
	return &Revoker{
		store:   bus,
		db:      db,
		revoked: map[string]time.Time{},
	}
}

// Start loads unexpired revocations from Postgres into memory and
// subscribes to revocation events on NATS. Call it once at startup,
// before the HTTP server begins accepting requests.
func (r *Revoker) Start(ctx context.Context) error {
	events, err := r.db.UnexpiredRevocations(ctx)
	if err != nil {
		return fmt.Errorf("load revoked tokens: %w", err)
	}
	r.mu.Lock()
	for _, e := range events {
		r.revoked[e.JTI] = e.ExpiresAt
	}
	r.mu.Unlock()

	// Deliberately a fan-out subscription, not a queue group: every
	// instance keeps its own in-memory revocation cache, so every
	// instance must receive every revocation. A queue group here would
	// deliver each revocation to exactly one instance and leave the
	// others still accepting the token - see internal/messaging's
	// package documentation for the rule this follows.
	_, err = r.store.Subscribe(RevocationSubject, func(msg *nats.Msg) {
		var e revocationEvent
		if err := json.Unmarshal(msg.Data, &e); err != nil {
			return // malformed event; nothing sensible to do but drop it
		}
		r.mu.Lock()
		r.revoked[e.JTI] = e.ExpiresAt
		r.mu.Unlock()
	})
	if err != nil {
		return fmt.Errorf("subscribe to revocation events: %w", err)
	}
	return nil
}

// Revoke persists jti as revoked (durable), updates this instance's own
// in-memory set synchronously (so it's rejected here immediately, not
// dependent on the NATS round-trip's timing), and publishes it so every
// other running instance's in-memory set picks it up too.
func (r *Revoker) Revoke(ctx context.Context, jti string, expiresAt time.Time) error {
	if err := r.db.PersistRevocation(ctx, jti, expiresAt); err != nil {
		return fmt.Errorf("persist revocation: %w", err)
	}

	r.mu.Lock()
	r.revoked[jti] = expiresAt
	r.mu.Unlock()

	data, err := json.Marshal(revocationEvent{JTI: jti, ExpiresAt: expiresAt})
	if err != nil {
		return fmt.Errorf("encode revocation event: %w", err)
	}
	if err := r.store.Publish(RevocationSubject, data); err != nil {
		return fmt.Errorf("publish revocation event: %w", err)
	}
	return nil
}

// Len reports the number of currently-tracked revocations in this
// instance's in-memory cache - used to back the
// console_rbac_revoked_tokens metrics gauge. Unlike the broker/
// dispatcher gauges, this one genuinely reflects global state: every
// running instance's cache converges to the same set via NATS
// propagation (see the type doc above), so this number is the same on
// whichever instance is scraped.
func (r *Revoker) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.revoked)
}

// IsRevoked reports whether jti has been revoked.
func (r *Revoker) IsRevoked(jti string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, revoked := r.revoked[jti]
	return revoked
}
