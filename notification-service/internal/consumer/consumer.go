// Package consumer reads domain events from RabbitMQ and hands them to a handler.
package consumer

import (
	"context"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"xsolla-notification-service/internal/broker"
	"xsolla-notification-service/internal/channel"
	"xsolla-notification-service/internal/events"
)

const (
	maxAttempts      = 3
	baseBackoff      = time.Second
	supportedVersion = 1
)

// Handler processes one valid domain event. A returned error marks the attempt as failed.
type Handler func(ctx context.Context, event events.Envelope) error

// Consumer consumes the notification queue with manual acknowledgements.
type Consumer struct {
	handler     Handler
	maxAttempts int
	backoff     time.Duration
}

// New creates a Consumer for the given handler.
func New(handler Handler) *Consumer {
	return &Consumer{handler: handler, maxAttempts: maxAttempts, backoff: baseBackoff}
}

// Session consumes until ctx is cancelled or the channel closes.
func (c *Consumer) Session(ctx context.Context, ch *amqp.Channel) error {
	if err := ch.Qos(1, 0, false); err != nil {
		return err
	}
	deliveries, err := ch.Consume(broker.Queue, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	log.Printf("consumer: waiting for messages on %s", broker.Queue)

	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return amqp.ErrClosed
			}
			c.process(ctx, delivery)
		}
	}
}

// process decodes one message and acknowledges it only after the handler succeeds.
func (c *Consumer) process(ctx context.Context, delivery amqp.Delivery) {
	event, err := events.Decode(delivery.Body)
	if err != nil {
		// Do not log the body: malformed messages may contain personal data.
		log.Printf("consumer: rejecting malformed message: %v", err)
		c.nack(delivery, false)
		return
	}
	if event.Version != supportedVersion {
		log.Printf("consumer: rejecting unsupported version %d for event %d", event.Version, event.ID)
		c.nack(delivery, false)
		return
	}

	// shortcut: retries block this single consumer; upgrade to delayed TTL retry queues when throughput matters.
	backoff := c.backoff
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		err := c.handler(ctx, event)
		if err == nil {
			if ackErr := delivery.Ack(false); ackErr != nil {
				log.Printf("consumer: ack event %d: %v", event.ID, ackErr)
			}
			return
		}
		log.Printf("consumer: event %d attempt %d/%d failed: %v", event.ID, attempt, c.maxAttempts, err)
		// Permanent failures (invalid recipient, bad auth) are not retried;
		// temporary failures (network, 4xx) follow bounded retries.
		if !channel.IsTemporary(err) {
			log.Printf("consumer: event %d permanent failure, moving to %s", event.ID, broker.DeadQueue)
			c.nack(delivery, false)
			return
		}
		if attempt == c.maxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return // the closed channel requeues the unacked message
		case <-time.After(backoff):
		}
		backoff *= 2
	}

	log.Printf("consumer: event %d moved to %s", event.ID, broker.DeadQueue)
	c.nack(delivery, false)
}

func (c *Consumer) nack(delivery amqp.Delivery, requeue bool) {
	if err := delivery.Nack(false, requeue); err != nil {
		log.Printf("consumer: nack delivery %d: %v", delivery.DeliveryTag, err)
	}
}
