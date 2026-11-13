// Package outbox publishes LMS domain events from the transactional outbox.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"xsolla-capstone-api/internal/store"
)

const (
	exchange     = "lms.events"
	producer     = "lms-api"
	eventVersion = 1

	batchSize    = 20
	pollInterval = time.Second
	minBackoff   = time.Second
	maxBackoff   = 30 * time.Second
)

// Store is the outbox persistence the publisher needs.
type Store interface {
	PendingOutboxEvents(ctx context.Context, limit int) ([]store.OutboxEvent, error)
	MarkOutboxEventPublished(ctx context.Context, id int64) error
	MarkOutboxEventFailed(ctx context.Context, id int64, cause string) error
}

// Broker publishes one message and only returns nil after the broker confirmed it.
type Broker interface {
	Publish(ctx context.Context, routingKey string, body []byte) error
	Close() error
}

// Publisher drains unpublished outbox rows to the broker.
type Publisher struct {
	store  Store
	broker Broker
}

// NewPublisher creates a Publisher.
func NewPublisher(store Store, broker Broker) *Publisher {
	return &Publisher{store: store, broker: broker}
}

// Run publishes until ctx is cancelled, retrying failures with capped backoff.
func (p *Publisher) Run(ctx context.Context) {
	backoff := minBackoff
	for {
		published, err := p.publishBatch(ctx)
		switch {
		case err != nil:
			log.Printf("outbox: %v; retrying in %s", err, backoff)
			if !sleep(ctx, backoff) {
				return
			}
			backoff = min(2*backoff, maxBackoff)
		case published > 0:
			backoff = minBackoff // keep draining the backlog
		default:
			backoff = minBackoff
			if !sleep(ctx, pollInterval) {
				return
			}
		}
	}
}

// publishBatch publishes the oldest pending events, stopping at the first failure
// so ordering is preserved.
func (p *Publisher) publishBatch(ctx context.Context) (int, error) {
	events, err := p.store.PendingOutboxEvents(ctx, batchSize)
	if err != nil {
		return 0, err
	}
	for _, event := range events {
		if err := p.publish(ctx, event); err != nil {
			return 0, err
		}
	}
	return len(events), nil
}

// publish sends one event and records the outcome. Failure leaves the row
// unpublished so the next poll retries it.
func (p *Publisher) publish(ctx context.Context, event store.OutboxEvent) error {
	body, err := envelopeJSON(event)
	if err != nil {
		return fmt.Errorf("event %d: %w", event.ID, err)
	}
	if err := p.broker.Publish(ctx, event.Type, body); err != nil {
		if markErr := p.store.MarkOutboxEventFailed(ctx, event.ID, err.Error()); markErr != nil {
			log.Printf("outbox: record failure for event %d: %v", event.ID, markErr)
		}
		return fmt.Errorf("publish event %d: %w", event.ID, err)
	}
	if err := p.store.MarkOutboxEventPublished(ctx, event.ID); err != nil {
		return fmt.Errorf("mark event %d published: %w", event.ID, err)
	}
	log.Printf("outbox: published event %d type %s", event.ID, event.Type)
	return nil
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// envelope is the contract in notification-service/docs/event-contract.md.
type envelope struct {
	ID         int64           `json:"id"`
	Type       string          `json:"type"`
	Version    int             `json:"version"`
	OccurredAt time.Time       `json:"occurred_at"`
	Producer   string          `json:"producer"`
	Data       json.RawMessage `json:"data"`
}

// envelopeJSON builds the message from the outbox row. The row id is the event
// id, so re-publishing after a crash reuses it.
func envelopeJSON(event store.OutboxEvent) ([]byte, error) {
	return json.Marshal(envelope{
		ID:         event.ID,
		Type:       event.Type,
		Version:    eventVersion,
		OccurredAt: event.CreatedAt.UTC(),
		Producer:   producer,
		Data:       json.RawMessage(event.Payload),
	})
}
