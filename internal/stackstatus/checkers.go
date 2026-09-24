package stackstatus

import (
	"context"
	"fmt"
)

// Dependency names, used both as the Checker's Name() and as the key
// into Config.Targets.
const (
	DepPostgres  = "postgres"
	DepNATS      = "nats"
	DepOpenvoxdb = "openvoxdb"
	DepCAClient  = "ca-client"
)

// openvoxdbQuerier is the subset of *openvoxdb.Client the check needs.
type openvoxdbQuerier interface {
	Query(ctx context.Context, pql string) ([]map[string]any, error)
}

// OpenvoxdbChecker reports whether openvoxdb is reachable.
//
// It probes with a real query rather than a bare TCP dial, because the
// interesting failures here are not "the port is closed" - they are an
// expired client certificate, a rejected mutual-TLS handshake, or an
// openvoxdb that accepts connections but refuses queries. A dial would
// call all of those healthy.
type OpenvoxdbChecker struct {
	Client openvoxdbQuerier
}

// Name identifies this checker.
func (OpenvoxdbChecker) Name() string { return DepOpenvoxdb }

// Check runs a trivial query. The query is chosen to be cheap and to
// return almost nothing: this runs on every status request, on every
// instance, and must not become load of its own.
func (c OpenvoxdbChecker) Check(ctx context.Context) error {
	if c.Client == nil {
		return fmt.Errorf("openvoxdb client is not configured")
	}
	if _, err := c.Client.Query(ctx, "nodes[certname] { limit 1 }"); err != nil {
		return err
	}
	return nil
}

// caStatuser is the subset of *certstatus.Client the check needs.
type caStatuser interface {
	Statuses(ctx context.Context) (map[string]string, error)
}

// CAClientChecker reports whether the openvoxserver CA is reachable with
// the console's own CA credential.
//
// Like the openvoxdb check, this exercises the credential rather than
// the socket: the CA endpoint is gated on an authorization extension, so
// "reachable" and "usable" are different questions and only the second
// one matters.
type CAClientChecker struct {
	Client caStatuser
}

// Name identifies this checker.
func (CAClientChecker) Name() string { return DepCAClient }

// Check asks the CA for certificate statuses.
func (c CAClientChecker) Check(ctx context.Context) error {
	if c.Client == nil {
		return fmt.Errorf("CA client is not configured")
	}
	if _, err := c.Client.Statuses(ctx); err != nil {
		return err
	}
	return nil
}
