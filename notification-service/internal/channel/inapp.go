package channel

import (
	"context"
	"errors"
	"strings"
	"time"

	"xsolla-notification-service/internal/events"
)

// InAppNotification is one rendered notification kept for its recipient, who
// reads it in the LMS. Title is the template's subject, Body its plain text.
type InAppNotification struct {
	Producer    string
	EventID     int64
	EventType   string
	RecipientID int64
	Title       string
	Body        string
	CreatedAt   time.Time // when the event happened
}

// InAppStore keeps in-app notifications. Saving the same event for the same
// recipient twice must keep one notification.
type InAppStore interface {
	SaveNotification(ctx context.Context, n InAppNotification) error
}

// InApp is the in_app channel: instead of sending the message it stores it for
// the recipient. The LMS API lists a user's unread notifications and marks them
// read; this service never reads them back.
type InApp struct {
	store InAppStore
}

// NewInApp creates the in_app channel backed by store.
func NewInApp(store InAppStore) *InApp {
	return &InApp{store: store}
}

// Send stores the message for the recipient.
func (c *InApp) Send(ctx context.Context, recipient events.Recipient, message Message) error {
	if recipient.ID <= 0 {
		return errors.New("in_app: recipient id must be positive")
	}
	createdAt := message.OccurredAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return c.store.SaveNotification(ctx, InAppNotification{
		Producer:    message.Producer,
		EventID:     message.EventID,
		EventType:   message.EventType,
		RecipientID: recipient.ID,
		Title:       message.Subject,
		Body:        strings.TrimSpace(message.Body),
		CreatedAt:   createdAt,
	})
}
