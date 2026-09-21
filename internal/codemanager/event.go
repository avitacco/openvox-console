package codemanager

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// DeployedSubject is the NATS subject published on successful deploy
// activation - the contract other in-cluster consumers (e.g. a future
// compiler distribution mechanism) subscribe to, independent of how
// many subscribers exist at any given time - see design.md and
// architecture-summary.md's "Code distribution to compilers" section.
const DeployedSubject = "codemanager.deployed"

// DeployedEvent is the payload published on DeployedSubject.
type DeployedEvent struct {
	// Source is the control repo the deploy came from. Environment
	// alone no longer identifies it: two sources can carry the same
	// branch name, and a subscriber deciding what to fetch needs to
	// know which repo's code just became live.
	Source      string    `json:"source"`
	Environment string    `json:"environment"`
	Ref         string    `json:"ref"`
	DeployedAt  time.Time `json:"deployedAt"`
}

// eventPublisher is the subset of *messaging.Bus this package needs to
// publish deployment-ready events, so it can be tested without a live
// NATS server.
type eventPublisher interface {
	Publish(subject string, data []byte) error
}

// PublishDeployed publishes a DeployedEvent for a source's
// environment/ref. Called only after a deploy's activation has
// genuinely succeeded - see design.md: "published only after the
// atomic swap completes successfully."
func PublishDeployed(bus eventPublisher, source, environment, ref string) error {
	data, err := json.Marshal(DeployedEvent{
		Source:      source,
		Environment: environment,
		Ref:         ref,
		DeployedAt:  time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("encode deployed event: %w", err)
	}
	if err := bus.Publish(DeployedSubject, data); err != nil {
		return fmt.Errorf("publish deployed event: %w", err)
	}
	return nil
}

// eventSubscriber is the subset of *messaging.Bus SubscribeDeployed
// needs, matching internal/activity.Recorder's subscriber pattern.
type eventSubscriber interface {
	Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error)
}

// SubscribeDeployed registers handler to be called for every published
// DeployedEvent - for tests and future compiler-distribution consumers.
func SubscribeDeployed(bus eventSubscriber, handler func(DeployedEvent)) (*nats.Subscription, error) {
	return bus.Subscribe(DeployedSubject, func(msg *nats.Msg) {
		var e DeployedEvent
		if err := json.Unmarshal(msg.Data, &e); err != nil {
			return
		}
		handler(e)
	})
}
