package channel

import (
	"context"
	"log"
	"strings"

	"xsolla-notification-service/internal/events"
)

// LogSender is a mock channel that logs instead of sending. It stands in until
// a real provider is chosen; the dispatcher does not know the difference.
type LogSender struct {
	name string
}

// NewLogSender creates a logging sender for the named channel, e.g. email.
func NewLogSender(name string) *LogSender {
	return &LogSender{name: name}
}

// Send logs the delivery attempt and always succeeds.
func (s *LogSender) Send(_ context.Context, recipient events.Recipient, message Message) error {
	log.Printf("channel %s: event=%d recipient=%d email=%s subject=%q body=%q result=ok",
		s.name, message.EventID, recipient.ID, maskEmail(recipient.Email), message.Subject, message.Body)
	return nil
}

// maskEmail keeps the domain but hides the local part, which is personal data.
func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "(hidden)"
	}
	return email[:1] + "***" + email[at:]
}
