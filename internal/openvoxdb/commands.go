package openvoxdb

import (
	"context"
	"time"
)

// deactivateNodePayload is "deactivate node" version 3's own JSON body -
// confirmed live against a real openvoxdb instance (see design.md in
// add-node-deletion): certname is a required key, and producer_timestamp
// must not be stale (older than the node's own last-known activity) or
// openvoxdb silently ignores the command rather than applying it.
type deactivateNodePayload struct {
	Certname          string `json:"certname"`
	ProducerTimestamp string `json:"producer_timestamp"`
}

// DeactivateNode marks certname deactivated in openvoxdb - it stops
// appearing in Nodes(ctx, false)'s results, but its historical data is
// left for openvoxdb's own background garbage collection to eventually
// erase, on its own schedule (see design.md - this is deliberate, not a
// limitation). producer_timestamp is always generated here, at call
// time, never accepted from a caller - an old one would be silently
// ignored as stale.
func (c *Client) DeactivateNode(ctx context.Context, certname string) error {
	payload := deactivateNodePayload{
		Certname:          certname,
		ProducerTimestamp: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
	}
	return c.command(ctx, "deactivate node", 3, certname, payload)
}
