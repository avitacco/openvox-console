package stackstatus_test

import (
	"context"
	"errors"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/stackstatus"
)

type fakeQuerier struct{ err error }

func (f fakeQuerier) Query(context.Context, string) ([]map[string]any, error) {
	return nil, f.err
}

type fakeStatuser struct{ err error }

func (f fakeStatuser) Statuses(context.Context) (map[string]string, error) {
	return nil, f.err
}

func TestOpenvoxdbCheckerHealthy(t *testing.T) {
	c := stackstatus.OpenvoxdbChecker{Client: fakeQuerier{}}
	if c.Name() != stackstatus.DepOpenvoxdb {
		t.Errorf("Name() = %q", c.Name())
	}
	if err := c.Check(context.Background()); err != nil {
		t.Errorf("Check() = %v, want nil", err)
	}
}

func TestOpenvoxdbCheckerUnreachable(t *testing.T) {
	c := stackstatus.OpenvoxdbChecker{Client: fakeQuerier{err: errors.New("connection refused")}}
	if err := c.Check(context.Background()); err == nil {
		t.Error("Check() = nil, want the underlying failure")
	}
}

// A nil client is a configuration state, and Check must say so rather
// than panicking - the reporter turns an absent target into
// not-configured before it ever gets here, but the checker must be safe
// on its own.
func TestOpenvoxdbCheckerWithNoClient(t *testing.T) {
	c := stackstatus.OpenvoxdbChecker{}
	if err := c.Check(context.Background()); err == nil {
		t.Error("Check() = nil with no client, want an error")
	}
}

func TestCAClientCheckerHealthy(t *testing.T) {
	c := stackstatus.CAClientChecker{Client: fakeStatuser{}}
	if c.Name() != stackstatus.DepCAClient {
		t.Errorf("Name() = %q", c.Name())
	}
	if err := c.Check(context.Background()); err != nil {
		t.Errorf("Check() = %v, want nil", err)
	}
}

func TestCAClientCheckerUnreachable(t *testing.T) {
	c := stackstatus.CAClientChecker{Client: fakeStatuser{err: errors.New("403")}}
	if err := c.Check(context.Background()); err == nil {
		t.Error("Check() = nil, want the underlying failure")
	}
}

func TestCAClientCheckerWithNoClient(t *testing.T) {
	if err := (stackstatus.CAClientChecker{}).Check(context.Background()); err == nil {
		t.Error("Check() = nil with no client, want an error")
	}
}
