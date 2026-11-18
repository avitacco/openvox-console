package agentless

import (
	"context"
	"errors"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

type fakeNodes struct {
	nodes map[string]bool
	err   error
}

func (f fakeNodes) NodeByCertname(_ context.Context, name string) (*openvoxdb.Node, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.nodes[name] {
		return &openvoxdb.Node{Certname: name}, nil
	}
	return nil, openvoxdb.ErrNodeNotFound // what the real client says for a name it has never heard of
}

type fakeCA struct {
	names map[string]string
	err   error
}

func (f fakeCA) Statuses(context.Context) (map[string]string, error) { return f.names, f.err }

func TestNodeKnown(t *testing.T) {
	ctx := context.Background()
	known := NodeKnown(fakeNodes{nodes: map[string]bool{"web01": true}}, fakeCA{names: map[string]string{"pending01": "requested"}})
	for name, want := range map[string]bool{"web01": true, "pending01": true, "brand-new": false} {
		if got, err := known(ctx, name); err != nil || got != want {
			t.Errorf("%s = %v, %v; want %v", name, got, err, want)
		}
	}
	// No CA client configured: openvoxdb alone decides.
	if got, err := NodeKnown(fakeNodes{}, nil)(ctx, "x"); err != nil || got {
		t.Errorf("without a CA = %v, %v", got, err)
	}
	// A failure to find out is not an answer.
	if _, err := NodeKnown(fakeNodes{err: errors.New("openvoxdb unreachable")}, nil)(ctx, "x"); err == nil {
		t.Error("an unreachable openvoxdb was taken to mean the name is free")
	}
	if _, err := NodeKnown(fakeNodes{}, fakeCA{err: errors.New("ca down")})(ctx, "x"); err == nil {
		t.Error("an unreachable CA was taken to mean the name is free")
	}
}
