package orchestrator

import (
	"context"
	"strings"
	"time"
)

// AgentlessLookup says which of certnames are agentless targets: hosts the
// console reaches over SSH rather than through a node agent. It returns
// them sorted, or nil when none is.
type AgentlessLookup func(ctx context.Context, certnames []string) []string

// AgentlessTargetsError is a Puppet run, patch run or plan run step that
// names agentless targets. They have no agent, so only a task can run on
// them; the request is refused whole.
type AgentlessTargetsError struct {
	Targets []string
	// What is what was asked: "a Puppet run", "a patch run", "a run step".
	What string
}

func (e *AgentlessTargetsError) Error() string {
	noun := "target"
	if len(e.Targets) > 1 {
		noun = "targets"
	}
	return "agentless " + noun + " can only run tasks, not " + e.What + ": " + strings.Join(e.Targets, ", ")
}

// SetAgentless lets requests that need the agent be refused for agentless
// targets. Without it every node is taken to have one.
func (h *Handlers) SetAgentless(l AgentlessLookup) { h.starter.agentless = l }

// refuseAgentless refuses what (a request that needs the agent) when any of
// certnames is agentless.
func (s *Starter) refuseAgentless(ctx context.Context, certnames []string, what string) error {
	if s.agentless == nil || len(certnames) == 0 {
		return nil
	}
	if bad := s.agentless(ctx, certnames); len(bad) > 0 {
		return &AgentlessTargetsError{Targets: bad, What: what}
	}
	return nil
}

// RoutedTransport sends a dispatch to the agentless executor for an
// agentless target and to the node transport for every other node, so
// that the dispatcher, plans and schedules need not know the difference.
type RoutedTransport struct {
	agent     Transport
	agentless Transport
	is        func(ctx context.Context, certname string) bool
}

// NewRoutedTransport builds a RoutedTransport. is says whether a name is an
// agentless target.
func NewRoutedTransport(agent, agentless Transport, is func(ctx context.Context, certname string) bool) *RoutedTransport {
	return &RoutedTransport{agent: agent, agentless: agentless, is: is}
}

// Dispatch implements Transport.
func (r *RoutedTransport) Dispatch(ctx context.Context, certname string, payload []byte, timeout time.Duration) ([]byte, error) {
	if r.agentless != nil && r.is != nil && r.is(ctx, certname) {
		return r.agentless.Dispatch(ctx, certname, payload, timeout)
	}
	return r.agent.Dispatch(ctx, certname, payload, timeout)
}
