package agentless

import (
	"context"
	"errors"

	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

// NodeFinder is the part of openvoxdb's client that says whether it has a
// node. It answers openvoxdb.ErrNodeNotFound for one it has never heard of.
type NodeFinder interface {
	NodeByCertname(ctx context.Context, certname string) (*openvoxdb.Node, error)
}

// CertificateStatuses is the CA's registry: every certname it knows, with
// its state. May be nil when no CA client is configured.
type CertificateStatuses interface {
	Statuses(ctx context.Context) (map[string]string, error)
}

// NodeKnown returns a function that says whether name is already a node's:
// its certname in openvoxdb, or in the CA's registry. A target may not take
// such a name, or a task meant for one node would reach another.
func NodeKnown(nodes NodeFinder, ca CertificateStatuses) func(ctx context.Context, name string) (bool, error) {
	return func(ctx context.Context, name string) (bool, error) {
		node, err := nodes.NodeByCertname(ctx, name)
		switch {
		case err == nil && node != nil:
			return true, nil
		case err != nil && !errors.Is(err, openvoxdb.ErrNodeNotFound):
			return false, err
		}
		if ca == nil {
			return false, nil
		}
		statuses, err := ca.Statuses(ctx)
		if err != nil {
			return false, err
		}
		_, known := statuses[name]
		return known, nil
	}
}
