package rbac

import (
	"context"
	"testing"
	"time"
)

func TestPersistRevocation_RealPostgres(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	jti := "rbac-store-test-revoke-jti"
	expiresAt := time.Now().Add(time.Hour).Truncate(time.Second)

	if err := s.PersistRevocation(ctx, jti, expiresAt); err != nil {
		t.Fatalf("PersistRevocation() error: %v", err)
	}

	events, err := s.UnexpiredRevocations(ctx)
	if err != nil {
		t.Fatalf("UnexpiredRevocations() error: %v", err)
	}
	var found bool
	for _, e := range events {
		if e.JTI == jti {
			found = true
			if !e.ExpiresAt.Equal(expiresAt) {
				t.Errorf("ExpiresAt = %v, want %v", e.ExpiresAt, expiresAt)
			}
		}
	}
	if !found {
		t.Errorf("UnexpiredRevocations() did not include %q", jti)
	}
}

func TestUnexpiredRevocations_ExcludesExpired_RealPostgres(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	expiredJTI := "rbac-store-test-revoke-expired"
	if err := s.PersistRevocation(ctx, expiredJTI, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("PersistRevocation() error: %v", err)
	}

	events, err := s.UnexpiredRevocations(ctx)
	if err != nil {
		t.Fatalf("UnexpiredRevocations() error: %v", err)
	}
	for _, e := range events {
		if e.JTI == expiredJTI {
			t.Errorf("UnexpiredRevocations() included an already-expired jti %q", expiredJTI)
		}
	}
}
