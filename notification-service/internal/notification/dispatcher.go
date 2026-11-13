package notification

import (
	"context"
	"log"

	"xsolla-notification-service/internal/events"
)

// Delivery is the work produced for one event: who to notify, on which
// channels, with which template.
type Delivery struct {
	Event      events.Envelope
	Recipients []events.Recipient
	Channels   []string
	Template   string
}

// DeliverFunc sends a delivery. It is the seam channel adapters plug into.
type DeliverFunc func(ctx context.Context, delivery Delivery) error

// Dispatcher routes a validated event to its policy and produces delivery work.
type Dispatcher struct {
	registry *Registry
	deliver  DeliverFunc
}

// NewDispatcher creates a Dispatcher. deliver may be nil only in tests that
// assert no delivery happens.
func NewDispatcher(registry *Registry, deliver DeliverFunc) *Dispatcher {
	return &Dispatcher{registry: registry, deliver: deliver}
}

// Handle looks up the event's policy and delivers to the matching recipients.
// An unknown, disabled, or channel-less policy is acknowledged without delivery.
func (d *Dispatcher) Handle(ctx context.Context, event events.Envelope) error {
	policy, ok := d.registry.Lookup(event.Type)
	if !ok {
		log.Printf("dispatcher: no policy for event %d type %s, ignoring", event.ID, event.Type)
		return nil
	}
	if !policy.Enabled {
		log.Printf("dispatcher: policy for event %d type %s is disabled, ignoring", event.ID, event.Type)
		return nil
	}
	if len(policy.Channels) == 0 {
		log.Printf("dispatcher: policy for event %d type %s has no channels, ignoring", event.ID, event.Type)
		return nil
	}

	recipients, err := resolveRecipients(policy.Recipients, event.Data)
	if err != nil {
		return err
	}

	// Never log email addresses; event payloads carry personal data.
	log.Printf("dispatcher: event %d type %s to %d recipient(s) on %v",
		event.ID, event.Type, len(recipients), policy.Channels)

	return d.deliver(ctx, Delivery{
		Event:      event,
		Recipients: recipients,
		Channels:   policy.Channels,
		Template:   policy.Template,
	})
}
