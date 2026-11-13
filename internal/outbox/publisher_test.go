package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"xsolla-capstone-api/internal/store"
)

type fakeStore struct {
	events      []store.OutboxEvent
	pendingErr  error
	published   []int64
	failed      []int64
	failedCause []string
}

func (f *fakeStore) PendingOutboxEvents(context.Context, int) ([]store.OutboxEvent, error) {
	if f.pendingErr != nil {
		return nil, f.pendingErr
	}
	return f.events, nil
}

func (f *fakeStore) MarkOutboxEventPublished(_ context.Context, id int64) error {
	f.published = append(f.published, id)
	return nil
}

func (f *fakeStore) MarkOutboxEventFailed(_ context.Context, id int64, cause string) error {
	f.failed = append(f.failed, id)
	f.failedCause = append(f.failedCause, cause)
	return nil
}

type fakeBroker struct {
	err    error
	keys   []string
	bodies [][]byte
}

func (f *fakeBroker) Publish(_ context.Context, routingKey string, body []byte) error {
	f.keys = append(f.keys, routingKey)
	f.bodies = append(f.bodies, body)
	return f.err
}

func (f *fakeBroker) Close() error {
	return nil
}

func TestPublishConfirmedEvent(t *testing.T) {
	fakeStore := &fakeStore{}
	broker := &fakeBroker{}
	publisher := NewPublisher(fakeStore, broker)

	event := store.OutboxEvent{
		ID:        42,
		Type:      "mentor_slot.booked",
		Payload:   []byte(`{"slot_id":123}`),
		CreatedAt: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC),
	}
	if err := publisher.publish(context.Background(), event); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if len(broker.keys) != 1 || broker.keys[0] != event.Type {
		t.Fatalf("routing keys = %v, want %q", broker.keys, event.Type)
	}
	if !reflect.DeepEqual(fakeStore.published, []int64{event.ID}) {
		t.Fatalf("published = %v, want [%d]", fakeStore.published, event.ID)
	}
	if len(fakeStore.failed) != 0 {
		t.Fatalf("failed = %v, want none", fakeStore.failed)
	}

	var got envelope
	if err := json.Unmarshal(broker.bodies[0], &got); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if got.ID != event.ID || got.Type != event.Type || got.Version != eventVersion || got.Producer != producer {
		t.Fatalf("unexpected envelope: %+v", got)
	}
	if !got.OccurredAt.UTC().Equal(event.CreatedAt) {
		t.Fatalf("occurred_at = %v, want %v", got.OccurredAt, event.CreatedAt)
	}
	if string(got.Data) != string(event.Payload) {
		t.Fatalf("data = %s, want %s", got.Data, event.Payload)
	}
}

func TestPublishFailureLeavesEventPending(t *testing.T) {
	fakeStore := &fakeStore{}
	brokerErr := errors.New("broker unavailable")
	broker := &fakeBroker{err: brokerErr}
	publisher := NewPublisher(fakeStore, broker)

	event := store.OutboxEvent{ID: 42, Type: "mentor_slot.booked", Payload: []byte(`{}`)}
	if err := publisher.publish(context.Background(), event); !errors.Is(err, brokerErr) {
		t.Fatalf("got %v, want %v", err, brokerErr)
	}
	if len(fakeStore.published) != 0 {
		t.Fatalf("published = %v, want none", fakeStore.published)
	}
	if !reflect.DeepEqual(fakeStore.failed, []int64{event.ID}) || fakeStore.failedCause[0] != brokerErr.Error() {
		t.Fatalf("failed = %v causes = %v", fakeStore.failed, fakeStore.failedCause)
	}
}

func TestEnvelopeJSONUsesUTC(t *testing.T) {
	zone := time.FixedZone("UTC+2", 2*60*60)
	body, err := envelopeJSON(store.OutboxEvent{
		ID:        7,
		Type:      "mentor_slot.booked",
		Payload:   []byte(`{}`),
		CreatedAt: time.Date(2026, 11, 13, 12, 0, 0, 0, zone),
	})
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}

	var got struct {
		OccurredAt time.Time `json:"occurred_at"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.OccurredAt.Location() != time.UTC || got.OccurredAt.Hour() != 10 {
		t.Fatalf("occurred_at = %v, want the same instant in UTC", got.OccurredAt)
	}
}

func TestRunStopsWhenCancelled(t *testing.T) {
	publisher := NewPublisher(&fakeStore{}, &fakeBroker{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		publisher.Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}
