package notification

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"xsolla-notification-service/internal/channel"
	"xsolla-notification-service/internal/events"
)

type sentMessage struct {
	recipient events.Recipient
	message   channel.Message
}

type fakeChannel struct {
	name  string
	err   error
	sends []sentMessage
}

func (f *fakeChannel) Send(_ context.Context, recipient events.Recipient, message channel.Message) error {
	f.sends = append(f.sends, sentMessage{recipient: recipient, message: message})
	return f.err
}

type fakeRenderer struct {
	err        error
	templates  []string
	recipients []int64
}

func (f *fakeRenderer) Render(templateName string, event events.Envelope, recipient events.Recipient) (channel.Message, error) {
	f.templates = append(f.templates, templateName)
	f.recipients = append(f.recipients, recipient.ID)
	if f.err != nil {
		return channel.Message{}, f.err
	}
	return channel.Message{EventID: event.ID, Subject: templateName, Body: "rendered for " + recipient.Name}, nil
}

type deliveryKey struct {
	producer    string
	eventID     int64
	eventType   string
	recipientID int64
	channel     string
}

type fakeDeliveries struct {
	skip          bool
	claimErr      error
	markSentErr   error
	markFailedErr error
	nextID        int64
	claims        []deliveryKey
	sent          []int64
	failed        []int64
	failedCauses  []string
}

func (f *fakeDeliveries) ClaimDelivery(_ context.Context, producer string, eventID int64, eventType string, recipientID int64, channelName string) (int64, bool, error) {
	if f.claimErr != nil {
		return 0, false, f.claimErr
	}
	if f.skip {
		return 0, false, nil
	}
	f.nextID++
	f.claims = append(f.claims, deliveryKey{producer, eventID, eventType, recipientID, channelName})
	return f.nextID, true, nil
}

func (f *fakeDeliveries) MarkDeliverySent(_ context.Context, id int64) error {
	f.sent = append(f.sent, id)
	return f.markSentErr
}

func (f *fakeDeliveries) MarkDeliveryFailed(_ context.Context, id int64, cause string) error {
	f.failed = append(f.failed, id)
	f.failedCauses = append(f.failedCauses, cause)
	return f.markFailedErr
}

func validEvent(t *testing.T) events.Envelope {
	t.Helper()
	body, err := os.ReadFile("../events/testdata/mentor_slot_booked.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	event, err := events.Decode(body)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return event
}

func TestRegistryLookup(t *testing.T) {
	registry := NewRegistry(map[string]Policy{
		"mentor_slot.booked": {Enabled: true},
	})
	if policy, ok := registry.Lookup("mentor_slot.booked"); !ok || !policy.Enabled {
		t.Fatalf("got %+v, %v; want the registered policy", policy, ok)
	}
	if _, ok := registry.Lookup("submission.graded"); ok {
		t.Fatal("unknown type returned a policy")
	}
}

func TestHandleSkipsWithoutDelivery(t *testing.T) {
	cases := []struct {
		name     string
		policies map[string]Policy
	}{
		{"missing policy", map[string]Policy{}},
		{"disabled policy", map[string]Policy{
			"mentor_slot.booked": {Enabled: false, Recipients: []string{"mentor"}, Channels: []string{"email"}},
		}},
		{"no channels", map[string]Policy{
			"mentor_slot.booked": {Enabled: true, Recipients: []string{"mentor"}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			email := &fakeChannel{name: "email"}
			renderer := &fakeRenderer{}
			deliveries := &fakeDeliveries{}
			d := NewDispatcher(NewRegistry(tc.policies), renderer, deliveries, map[string]channel.Channel{"email": email})
			if err := d.Handle(context.Background(), validEvent(t)); err != nil {
				t.Fatalf("handle: %v", err)
			}
			if len(email.sends) != 0 {
				t.Fatalf("sent %d message(s), want 0", len(email.sends))
			}
			if len(renderer.templates) != 0 {
				t.Fatalf("rendered %d message(s), want 0", len(renderer.templates))
			}
			if len(deliveries.claims) != 0 {
				t.Fatalf("claimed %d delivery(ies), want 0", len(deliveries.claims))
			}
		})
	}
}

func TestHandleDeliversToMatchingRecipients(t *testing.T) {
	email := &fakeChannel{name: "email"}
	renderer := &fakeRenderer{}
	deliveries := &fakeDeliveries{}
	d := NewDispatcher(NewRegistry(map[string]Policy{
		"mentor_slot.booked": {
			Enabled:    true,
			Recipients: []string{"mentor", "student"},
			Channels:   []string{"email"},
			Template:   "booking_confirmation",
		},
	}), renderer, deliveries, map[string]channel.Channel{"email": email})

	event := validEvent(t)
	if err := d.Handle(context.Background(), event); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(email.sends) != 2 {
		t.Fatalf("sent %d message(s), want 2", len(email.sends))
	}
	if email.sends[0].recipient.Role != "mentor" || email.sends[1].recipient.Role != "student" {
		t.Fatalf("unexpected recipients: %+v", email.sends)
	}
	if email.sends[0].message.EventID != event.ID || email.sends[0].message.Subject != "booking_confirmation" {
		t.Fatalf("unexpected message: %+v", email.sends[0].message)
	}
	if email.sends[0].message.Body == "" {
		t.Fatal("message body is empty")
	}
	if len(renderer.templates) != 2 || renderer.templates[0] != "booking_confirmation" {
		t.Fatalf("unexpected render calls: %+v", renderer.templates)
	}
	wantClaims := []deliveryKey{
		{"lms-api", event.ID, "mentor_slot.booked", 7, "email"},
		{"lms-api", event.ID, "mentor_slot.booked", 42, "email"},
	}
	if len(deliveries.claims) != 2 || deliveries.claims[0] != wantClaims[0] || deliveries.claims[1] != wantClaims[1] {
		t.Fatalf("unexpected claims: %+v", deliveries.claims)
	}
	if len(deliveries.sent) != 2 {
		t.Fatalf("marked %d delivery(ies) sent, want 2", len(deliveries.sent))
	}
}

func TestHandleRendersOncePerRecipient(t *testing.T) {
	email := &fakeChannel{name: "email"}
	sms := &fakeChannel{name: "sms"}
	renderer := &fakeRenderer{}
	deliveries := &fakeDeliveries{}
	d := NewDispatcher(NewRegistry(map[string]Policy{
		"mentor_slot.booked": {
			Enabled:    true,
			Recipients: []string{"mentor", "student"},
			Channels:   []string{"email", "sms"},
			Template:   "booking_confirmation",
		},
	}), renderer, deliveries, map[string]channel.Channel{"email": email, "sms": sms})

	if err := d.Handle(context.Background(), validEvent(t)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(email.sends) != 2 || len(sms.sends) != 2 {
		t.Fatalf("email=%d sms=%d sends, want 2 each", len(email.sends), len(sms.sends))
	}
	if len(renderer.templates) != 2 {
		t.Fatalf("rendered %d time(s), want once per recipient", len(renderer.templates))
	}
	if len(deliveries.claims) != 4 || len(deliveries.sent) != 4 {
		t.Fatalf("claims=%d sent=%d, want one per recipient and channel", len(deliveries.claims), len(deliveries.sent))
	}
}

func TestResolveRecipientsFiltersRolesAndDedupes(t *testing.T) {
	data := json.RawMessage(`{"recipients": [
		{"id": 7, "role": "mentor", "name": "Mentor One", "email": "mentor@example.com"},
		{"id": 7, "role": "mentor", "name": "Mentor One", "email": "mentor@example.com"},
		{"id": 42, "role": "student", "name": "Student One", "email": "student@example.com"}
	]}`)

	recipients, err := resolveRecipients([]string{"mentor"}, data)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(recipients) != 1 || recipients[0].ID != 7 {
		t.Fatalf("unexpected recipients: %+v", recipients)
	}
}

func TestResolveRecipientsRejectsBadInput(t *testing.T) {
	cases := []struct {
		name  string
		roles []string
		data  string
		want  error
	}{
		{"no recipients", []string{"mentor"}, `{}`, ErrNoRecipients},
		{"no role match", []string{"admin"}, `{"recipients":[{"id":7,"role":"mentor"}]}`, ErrNoRecipients},
		{"invalid recipients", []string{"mentor"}, `{"recipients":"nope"}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveRecipients(tc.roles, json.RawMessage(tc.data))
			if err == nil {
				t.Fatal("want an error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestHandleDoesNotDeliverWithoutRecipients(t *testing.T) {
	email := &fakeChannel{name: "email"}
	renderer := &fakeRenderer{}
	deliveries := &fakeDeliveries{}
	d := NewDispatcher(NewRegistry(map[string]Policy{
		"mentor_slot.booked": {Enabled: true, Recipients: []string{"admin"}, Channels: []string{"email"}},
	}), renderer, deliveries, map[string]channel.Channel{"email": email})

	err := d.Handle(context.Background(), validEvent(t))
	if !errors.Is(err, ErrNoRecipients) {
		t.Fatalf("got %v, want ErrNoRecipients", err)
	}
	if len(email.sends) != 0 {
		t.Fatalf("sent %d message(s), want 0", len(email.sends))
	}
	if len(deliveries.claims) != 0 {
		t.Fatalf("claimed %d delivery(ies), want 0", len(deliveries.claims))
	}
}

func TestHandleRejectsUnregisteredChannel(t *testing.T) {
	email := &fakeChannel{name: "email"}
	renderer := &fakeRenderer{}
	deliveries := &fakeDeliveries{}
	d := NewDispatcher(NewRegistry(map[string]Policy{
		"mentor_slot.booked": {Enabled: true, Recipients: []string{"mentor"}, Channels: []string{"sms"}},
	}), renderer, deliveries, map[string]channel.Channel{"email": email})

	if err := d.Handle(context.Background(), validEvent(t)); err == nil {
		t.Fatal("want an error")
	}
	if len(email.sends) != 0 {
		t.Fatalf("sent %d message(s), want 0", len(email.sends))
	}
	if len(deliveries.claims) != 0 {
		t.Fatalf("claimed %d delivery(ies), want 0", len(deliveries.claims))
	}
}

func TestHandleDoesNotSendWhenRenderingFails(t *testing.T) {
	want := errors.New(`template "booking_confirmation" body: map has no entry for key "starts_at"`)
	email := &fakeChannel{name: "email"}
	deliveries := &fakeDeliveries{}
	d := NewDispatcher(NewRegistry(map[string]Policy{
		"mentor_slot.booked": {Enabled: true, Recipients: []string{"mentor", "student"}, Channels: []string{"email"}, Template: "booking_confirmation"},
	}), &fakeRenderer{err: want}, deliveries, map[string]channel.Channel{"email": email})

	if err := d.Handle(context.Background(), validEvent(t)); !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
	if len(email.sends) != 0 {
		t.Fatalf("sent %d message(s), want 0", len(email.sends))
	}
	if len(deliveries.claims) != 0 {
		t.Fatalf("claimed %d delivery(ies), want 0", len(deliveries.claims))
	}
}

func TestHandlePropagatesDeliveryError(t *testing.T) {
	want := errors.New("channel unavailable")
	email := &fakeChannel{name: "email", err: want}
	deliveries := &fakeDeliveries{}
	d := NewDispatcher(NewRegistry(map[string]Policy{
		"mentor_slot.booked": {Enabled: true, Recipients: []string{"mentor"}, Channels: []string{"email"}, Template: "booking_confirmation"},
	}), &fakeRenderer{}, deliveries, map[string]channel.Channel{"email": email})

	if err := d.Handle(context.Background(), validEvent(t)); !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
	if len(deliveries.failed) != 1 || deliveries.failedCauses[0] != want.Error() {
		t.Fatalf("failed=%v causes=%v, want one failure recorded", deliveries.failed, deliveries.failedCauses)
	}
	if len(deliveries.sent) != 0 {
		t.Fatalf("recorded %d sent delivery(ies), want 0", len(deliveries.sent))
	}
}

func TestHandleSkipsAlreadySentDelivery(t *testing.T) {
	email := &fakeChannel{name: "email"}
	deliveries := &fakeDeliveries{skip: true}
	d := NewDispatcher(NewRegistry(map[string]Policy{
		"mentor_slot.booked": {Enabled: true, Recipients: []string{"mentor", "student"}, Channels: []string{"email"}, Template: "booking_confirmation"},
	}), &fakeRenderer{}, deliveries, map[string]channel.Channel{"email": email})

	if err := d.Handle(context.Background(), validEvent(t)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(email.sends) != 0 {
		t.Fatalf("sent %d message(s), want 0 for already sent deliveries", len(email.sends))
	}
	if len(deliveries.sent) != 0 || len(deliveries.failed) != 0 {
		t.Fatalf("sent=%v failed=%v, want no state writes", deliveries.sent, deliveries.failed)
	}
}

func TestHandleDoesNotSendWhenClaimFails(t *testing.T) {
	want := errors.New("database unavailable")
	email := &fakeChannel{name: "email"}
	d := NewDispatcher(NewRegistry(map[string]Policy{
		"mentor_slot.booked": {Enabled: true, Recipients: []string{"mentor"}, Channels: []string{"email"}, Template: "booking_confirmation"},
	}), &fakeRenderer{}, &fakeDeliveries{claimErr: want}, map[string]channel.Channel{"email": email})

	if err := d.Handle(context.Background(), validEvent(t)); !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
	if len(email.sends) != 0 {
		t.Fatalf("sent %d message(s), want 0", len(email.sends))
	}
}

func TestHandleDoesNotAckWhenMarkingSentFails(t *testing.T) {
	want := errors.New("database unavailable")
	email := &fakeChannel{name: "email"}
	d := NewDispatcher(NewRegistry(map[string]Policy{
		"mentor_slot.booked": {Enabled: true, Recipients: []string{"mentor"}, Channels: []string{"email"}, Template: "booking_confirmation"},
	}), &fakeRenderer{}, &fakeDeliveries{markSentErr: want}, map[string]channel.Channel{"email": email})

	if err := d.Handle(context.Background(), validEvent(t)); !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
	if len(email.sends) != 1 {
		t.Fatalf("sent %d message(s), want 1", len(email.sends))
	}
}
