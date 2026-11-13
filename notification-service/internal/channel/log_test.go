package channel

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"xsolla-notification-service/internal/events"
)

func TestLogSenderLogsDelivery(t *testing.T) {
	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(previous)

	sender := NewLogSender("email")
	err := sender.Send(context.Background(), events.Recipient{
		ID:    7,
		Role:  "mentor",
		Name:  "Mentor One",
		Email: "mentor@example.com",
	}, Message{
		EventID: 18422,
		Subject: "booking_confirmation",
		Body:    "mentor_slot.booked (event 18422)",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		"channel email:",
		"event=18422",
		"recipient=7",
		"email=m***@example.com",
		`subject="booking_confirmation"`,
		`body="mentor_slot.booked (event 18422)"`,
		"result=ok",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("log %q does not contain %q", output, want)
		}
	}
	if strings.Contains(output, "mentor@example.com") {
		t.Fatalf("log leaks the full email address: %q", output)
	}
}

func TestMaskEmail(t *testing.T) {
	cases := map[string]string{
		"mentor@example.com": "m***@example.com",
		"":                   "(hidden)",
		"no-at-sign":         "(hidden)",
		"@example.com":       "(hidden)",
	}
	for email, want := range cases {
		if got := maskEmail(email); got != want {
			t.Fatalf("maskEmail(%q) = %q, want %q", email, got, want)
		}
	}
}
