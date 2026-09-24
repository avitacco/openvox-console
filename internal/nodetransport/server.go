// Package nodetransport implements the console-side connection point for
// managed nodes (see openspec/specs/node-transport): a mutual-TLS
// listener, authenticated against the OpenVox CA and its CRL, that
// node-agent instances connect to for on-demand orchestration, and the
// dispatcher that sends them requests.
//
// Both live on the console's one embedded NATS server (see
// internal/messaging), in its own account, messaging.AccountNodes -
// isolated from the console's internal bus by NATS's account boundary
// rather than by running a second server, and clustered along with it.
// A dispatch from any routed instance reaches a node connected to any
// other.
package nodetransport

import (
	"fmt"

	"github.com/voxpupuli/enterprise-console/internal/messaging"
)

// Server is a node listener and a dispatcher over a bus of their own - a
// complete, single-instance node transport in one value. The console
// itself shares one bus between everything and composes a Listener and a
// Dispatcher directly (see internal/app); this is for the places that
// want the transport alone, tests above all.
type Server struct {
	*Listener
	*Dispatcher
	bus *messaging.Bus
}

// New starts a bus configured by busCfg with cfg's node listener on it,
// and attaches a Listener and a Dispatcher. busCfg's Nodes is set here.
func New(cfg Config, busCfg messaging.Config) (*Server, error) {
	listener, err := NewListener(cfg)
	if err != nil {
		return nil, err
	}
	busCfg.Nodes = listener.NodeListener()
	bus, err := messaging.StartWith(busCfg)
	if err != nil {
		return nil, fmt.Errorf("start node transport bus: %w", err)
	}
	if err := listener.Start(bus); err != nil {
		bus.Close()
		return nil, err
	}
	dispatcher, err := NewDispatcher(bus)
	if err != nil {
		listener.Close()
		bus.Close()
		return nil, err
	}
	return &Server{Listener: listener, Dispatcher: dispatcher, bus: bus}, nil
}

// Bus returns the server's own bus.
func (s *Server) Bus() *messaging.Bus { return s.bus }

// Close stops the dispatcher and listener, then the bus under them.
func (s *Server) Close() {
	s.Dispatcher.Close()
	s.Listener.Close()
	s.bus.Close()
}
