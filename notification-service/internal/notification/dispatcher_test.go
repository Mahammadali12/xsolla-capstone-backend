package notification

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"xsolla-notification-service/internal/events"
)

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
			delivered := 0
			d := NewDispatcher(NewRegistry(tc.policies), func(context.Context, Delivery) error {
				delivered++
				return nil
			})
			if err := d.Handle(context.Background(), validEvent(t)); err != nil {
				t.Fatalf("handle: %v", err)
			}
			if delivered != 0 {
				t.Fatalf("delivered %d time(s), want 0", delivered)
			}
		})
	}
}

func TestHandleDeliversToMatchingRecipients(t *testing.T) {
	var got Delivery
	d := NewDispatcher(NewRegistry(map[string]Policy{
		"mentor_slot.booked": {
			Enabled:    true,
			Recipients: []string{"mentor", "student"},
			Channels:   []string{"email"},
			Template:   "slot_booked",
		},
	}), func(_ context.Context, delivery Delivery) error {
		got = delivery
		return nil
	})

	event := validEvent(t)
	if err := d.Handle(context.Background(), event); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if got.Event.ID != event.ID {
		t.Fatalf("event id = %d, want %d", got.Event.ID, event.ID)
	}
	if len(got.Recipients) != 2 || got.Recipients[0].Role != "mentor" || got.Recipients[1].Role != "student" {
		t.Fatalf("unexpected recipients: %+v", got.Recipients)
	}
	if len(got.Channels) != 1 || got.Channels[0] != "email" || got.Template != "slot_booked" {
		t.Fatalf("unexpected delivery: %+v", got)
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
	delivered := 0
	d := NewDispatcher(NewRegistry(map[string]Policy{
		"mentor_slot.booked": {Enabled: true, Recipients: []string{"admin"}, Channels: []string{"email"}},
	}), func(context.Context, Delivery) error {
		delivered++
		return nil
	})

	err := d.Handle(context.Background(), validEvent(t))
	if !errors.Is(err, ErrNoRecipients) {
		t.Fatalf("got %v, want ErrNoRecipients", err)
	}
	if delivered != 0 {
		t.Fatalf("delivered %d time(s), want 0", delivered)
	}
}

func TestHandlePropagatesDeliveryError(t *testing.T) {
	want := errors.New("channel unavailable")
	d := NewDispatcher(NewRegistry(map[string]Policy{
		"mentor_slot.booked": {Enabled: true, Recipients: []string{"mentor"}, Channels: []string{"email"}},
	}), func(context.Context, Delivery) error {
		return want
	})

	if err := d.Handle(context.Background(), validEvent(t)); !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}
