// Package channel sends notifications through a delivery channel.
package channel

import (
	"context"
	"time"

	"xsolla-notification-service/internal/events"
)

// Message is the rendered notification for one recipient.
type Message struct {
	EventID  int64 // for tracing
	Subject  string
	Body     string // plain text, always present
	HTMLBody string // optional HTML alternate; empty means plain text only

	// The event the message was rendered from, for channels that keep it (in_app).
	Producer   string
	EventType  string
	OccurredAt time.Time
}

// Channel sends one message to one recipient. Implementations must be safe for
// the dispatcher's sequential use and return errors for temporary failures.
type Channel interface {
	Send(ctx context.Context, recipient events.Recipient, message Message) error
}
