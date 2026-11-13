// Package broker owns the RabbitMQ connection and the notification topology.
package broker

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	// Exchange is the topic exchange the LMS publishes domain events to.
	Exchange = "lms.events"
	// Queue receives every event published to Exchange.
	Queue = "notifications.events"
	// DeadExchange receives messages the queue rejects.
	DeadExchange = "lms.events.dlx"
	// DeadQueue holds rejected messages for inspection.
	DeadQueue = "notifications.dlq"

	deadRoutingKey = DeadQueue
)

const (
	minBackoff = time.Second
	maxBackoff = 30 * time.Second
)

// SessionFunc runs work on an established connection, such as consuming a queue.
// It must return when ctx is cancelled or the channel closes.
type SessionFunc func(ctx context.Context, ch *amqp.Channel) error

// Manager keeps a RabbitMQ connection open and redeclares the topology after reconnects.
type Manager struct {
	url       string
	sessionFn SessionFunc
	connected atomic.Bool
}

// NewManager creates a Manager for the given AMQP URL.
func NewManager(url string, session SessionFunc) *Manager {
	return &Manager{url: url, sessionFn: session}
}

// Connected reports whether the broker connection and topology are ready.
func (m *Manager) Connected() bool {
	return m.connected.Load()
}

// Run connects and blocks until ctx is cancelled, retrying with capped backoff.
func (m *Manager) Run(ctx context.Context) {
	backoff := minBackoff
	for {
		established, err := m.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if established {
			backoff = minBackoff
		}
		if err != nil {
			log.Printf("broker: %v; retrying in %s", err, backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if !established && backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// session holds one connection until it fails or ctx is cancelled.
// established reports whether the connection got as far as declaring the topology.
func (m *Manager) session(ctx context.Context) (bool, error) {
	conn, err := amqp.DialConfig(m.url, amqp.Config{Heartbeat: 10 * time.Second})
	if err != nil {
		return false, err
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return false, err
	}
	defer ch.Close()

	if err := declareTopology(ch); err != nil {
		return false, err
	}

	m.connected.Store(true)
	defer m.connected.Store(false)
	log.Print("broker: connected, topology ready")

	errCh := make(chan error, 1)
	if m.sessionFn != nil {
		go func() {
			errCh <- m.sessionFn(ctx, ch)
		}()
	}

	closed := conn.NotifyClose(make(chan *amqp.Error, 1))
	select {
	case <-ctx.Done():
		return true, nil
	case err := <-closed:
		if err != nil {
			return true, err
		}
		return true, nil
	case err := <-errCh:
		return true, err
	}
}

// declareTopology creates the exchange, queue, dead-letter route, and catch-all binding.
func declareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(DeadExchange, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s: %w", DeadExchange, err)
	}
	if _, err := ch.QueueDeclare(DeadQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s: %w", DeadQueue, err)
	}
	if err := ch.QueueBind(DeadQueue, deadRoutingKey, DeadExchange, false, nil); err != nil {
		return fmt.Errorf("bind %s: %w", DeadQueue, err)
	}

	if err := ch.ExchangeDeclare(Exchange, amqp.ExchangeTopic, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s: %w", Exchange, err)
	}
	args := amqp.Table{
		"x-dead-letter-exchange":    DeadExchange,
		"x-dead-letter-routing-key": deadRoutingKey,
	}
	if _, err := ch.QueueDeclare(Queue, true, false, false, false, args); err != nil {
		return fmt.Errorf("declare %s: %w", Queue, err)
	}
	// Catch-all binding: new routing keys need no broker change; policies filter later.
	if err := ch.QueueBind(Queue, "#", Exchange, false, nil); err != nil {
		return fmt.Errorf("bind %s: %w", Queue, err)
	}
	return nil
}
