package outbox

import (
	"context"
	"errors"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const confirmTimeout = 5 * time.Second

// AMQPBroker publishes to RabbitMQ with publisher confirmations. It connects
// lazily and reconnects after any channel or connection failure.
type AMQPBroker struct {
	url string

	mu      sync.Mutex
	conn    *amqp.Connection
	channel *amqp.Channel
	confirm chan amqp.Confirmation
}

// NewAMQPBroker creates a broker for the given AMQP URL.
func NewAMQPBroker(url string) *AMQPBroker {
	return &AMQPBroker{url: url}
}

// Publish sends a persistent message and returns nil only after a positive
// broker confirmation. The notification service owns the queue and DLQ; the
// publisher only declares the exchange.
func (b *AMQPBroker) Publish(ctx context.Context, routingKey string, body []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.channel == nil {
		if err := b.connect(); err != nil {
			return err
		}
	}
	err := b.channel.PublishWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
	if err != nil {
		b.reset()
		return err
	}

	select {
	case confirmation, ok := <-b.confirm:
		if !ok {
			b.reset()
			return errors.New("publisher confirm channel closed")
		}
		if !confirmation.Ack {
			b.reset()
			return errors.New("message rejected by broker")
		}
		return nil
	case <-time.After(confirmTimeout):
		b.reset()
		return errors.New("publisher confirm timed out")
	case <-ctx.Done():
		b.reset()
		return ctx.Err()
	}
}

// Close releases the connection.
func (b *AMQPBroker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reset()
	return nil
}

func (b *AMQPBroker) connect() error {
	conn, err := amqp.DialConfig(b.url, amqp.Config{Heartbeat: 10 * time.Second})
	if err != nil {
		return err
	}
	channel, err := conn.Channel()
	if err != nil {
		conn.Close()
		return err
	}
	if err := channel.ExchangeDeclare(exchange, amqp.ExchangeTopic, true, false, false, false, nil); err != nil {
		channel.Close()
		conn.Close()
		return err
	}
	if err := channel.Confirm(false); err != nil {
		channel.Close()
		conn.Close()
		return err
	}
	b.conn = conn
	b.channel = channel
	b.confirm = channel.NotifyPublish(make(chan amqp.Confirmation, 1))
	return nil
}

func (b *AMQPBroker) reset() {
	if b.channel != nil {
		_ = b.channel.Close()
	}
	if b.conn != nil {
		_ = b.conn.Close()
	}
	b.channel, b.conn, b.confirm = nil, nil, nil
}
