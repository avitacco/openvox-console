package rbac

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func noopRecordSystemActivity(_, _ string) {}

func noopRecordAudit(auditlog.Event) {}

func TestNewOIDCService_UnconfiguredIsInactive(t *testing.T) {
	svc := NewOIDCService(context.Background(), discardLogger(), nil, nil, OIDCConfig{}, noopRecordSystemActivity, noopRecordAudit)
	if svc.Configured() {
		t.Error("Configured() = true, want false when Issuer is empty")
	}
}

func TestNewOIDCService_NilServiceIsNotConfigured(t *testing.T) {
	var svc *OIDCService
	if svc.Configured() {
		t.Error("Configured() = true on a nil *OIDCService, want false")
	}
}

func TestNewOIDCService_DiscoveryFailureIsInactiveNotFatal(t *testing.T) {
	// Nothing listens on this port; discovery must fail fast rather than
	// hang, and the failure must be logged and non-fatal - the function
	// returns a usable (inactive) service, it does not panic or exit.
	svc := NewOIDCService(context.Background(), discardLogger(), nil, nil, OIDCConfig{
		Issuer: "http://127.0.0.1:1",
	}, noopRecordSystemActivity, noopRecordAudit)
	if svc.Configured() {
		t.Error("Configured() = true, want false when provider discovery fails")
	}
}

func TestNewOIDCService_InvalidRoleMappingIsInactiveNotFatal(t *testing.T) {
	svc := NewOIDCService(context.Background(), discardLogger(), nil, nil, OIDCConfig{
		Issuer:      "http://127.0.0.1:1",
		RoleMapping: "not valid json",
	}, noopRecordSystemActivity, noopRecordAudit)
	if svc.Configured() {
		t.Error("Configured() = true, want false when role mapping fails to parse")
	}
}

func TestAuthorizationURL_NotConfiguredReturnsError(t *testing.T) {
	svc := NewOIDCService(context.Background(), discardLogger(), nil, nil, OIDCConfig{}, noopRecordSystemActivity, noopRecordAudit)
	if _, err := svc.AuthorizationURL(context.Background()); err != ErrOIDCNotConfigured {
		t.Errorf("error = %v, want ErrOIDCNotConfigured", err)
	}
}

func TestHandleCallback_NotConfiguredReturnsError(t *testing.T) {
	svc := NewOIDCService(context.Background(), discardLogger(), nil, nil, OIDCConfig{}, noopRecordSystemActivity, noopRecordAudit)
	if _, err := svc.HandleCallback(context.Background(), "code", "state"); err != ErrOIDCNotConfigured {
		t.Errorf("error = %v, want ErrOIDCNotConfigured", err)
	}
}

func TestClaimStringSlice(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want []string
	}{
		{"array of strings", []any{"a", "b"}, []string{"a", "b"}},
		{"single string", "solo", []string{"solo"}},
		{"nil", nil, nil},
		{"array with non-string elements ignored", []any{"a", 5, "b"}, []string{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := claimStringSlice(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("claimStringSlice(%v) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("claimStringSlice(%v)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}
