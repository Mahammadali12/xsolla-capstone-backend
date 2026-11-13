package notification

import (
	"context"
	"fmt"
	"log"
	"strings"

	"xsolla-notification-service/internal/channel"
	"xsolla-notification-service/internal/events"
)

// sanitizeError strips credentials and internal details from an error before
// it is persisted to delivery history.
func sanitizeError(err error) string {
	msg := err.Error()
	// Strip SMTP credentials that may appear in auth errors.
	if idx := strings.Index(msg, "smtp auth:"); idx >= 0 {
		msg = msg[:idx] + "smtp auth: <redacted>"
	}
	// Strip any embedded passwords from connection strings.
	if idx := strings.Index(msg, "password="); idx >= 0 {
		msg = msg[:idx] + "password=<redacted>"
	}
	// Keep messages short; long stack traces leak internals.
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	return msg
}

// Renderer renders a policy's template into a message for one recipient.
type Renderer interface {
	Render(templateName string, event events.Envelope, recipient events.Recipient) (channel.Message, error)
}

// Deliveries records delivery attempts and deduplicates redelivered events.
type Deliveries interface {
	// ClaimDelivery returns claimed=false when the delivery already succeeded.
	ClaimDelivery(ctx context.Context, producer string, eventID int64, eventType string, recipientID int64, channel string) (int64, bool, error)
	MarkDeliverySent(ctx context.Context, id int64) error
	MarkDeliveryFailed(ctx context.Context, id int64, cause string) error
}

// Dispatcher routes a validated event to its policy and sends each recipient's
// message through the policy's channels.
type Dispatcher struct {
	registry   *Registry
	renderer   Renderer
	deliveries Deliveries
	channels   map[string]channel.Channel
}

// NewDispatcher creates a Dispatcher with the template renderer, delivery store,
// and channel adapters keyed by channel name.
func NewDispatcher(registry *Registry, renderer Renderer, deliveries Deliveries, channels map[string]channel.Channel) *Dispatcher {
	return &Dispatcher{registry: registry, renderer: renderer, deliveries: deliveries, channels: channels}
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
	// Fail before sending anything rather than after a partial delivery.
	for _, name := range policy.Channels {
		if _, ok := d.channels[name]; !ok {
			return fmt.Errorf("no channel adapter for %q", name)
		}
	}

	recipients, err := resolveRecipients(policy.Recipients, event.Data)
	if err != nil {
		return err
	}

	// Render before sending: a template error must not leave a partial delivery.
	messages := make([]channel.Message, len(recipients))
	for i, recipient := range recipients {
		message, err := d.renderer.Render(policy.Template, event, recipient)
		if err != nil {
			return err
		}
		messages[i] = message
	}

	// Never log email addresses; event payloads carry personal data.
	log.Printf("dispatcher: event %d type %s to %d recipient(s) on %v",
		event.ID, event.Type, len(recipients), policy.Channels)

	for i, recipient := range recipients {
		for _, name := range policy.Channels {
			id, claimed, err := d.deliveries.ClaimDelivery(ctx, event.Producer, event.ID, event.Type, recipient.ID, name)
			if err != nil {
				return fmt.Errorf("claim delivery for recipient %d channel %s: %w", recipient.ID, name, err)
			}
			if !claimed {
				log.Printf("dispatcher: event %d recipient %d channel %s already sent, skipping", event.ID, recipient.ID, name)
				continue
			}
			if err := d.channels[name].Send(ctx, recipient, messages[i]); err != nil {
				if markErr := d.deliveries.MarkDeliveryFailed(ctx, id, sanitizeError(err)); markErr != nil {
					log.Printf("dispatcher: record failed delivery %d: %v", id, markErr)
				}
				return fmt.Errorf("channel %s recipient %d: %w", name, recipient.ID, err)
			}
			if err := d.deliveries.MarkDeliverySent(ctx, id); err != nil {
				return fmt.Errorf("record sent delivery %d: %w", id, err)
			}
		}
	}
	return nil
}
