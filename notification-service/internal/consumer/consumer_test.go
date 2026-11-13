package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"xsolla-notification-service/internal/channel"
	"xsolla-notification-service/internal/events"
)

type fakeAcknowledger struct {
	mu       sync.Mutex
	acks     int
	nacks    int
	rejects  int
	requeues []bool
}

func (f *fakeAcknowledger) Ack(uint64, bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acks++
	return nil
}

func (f *fakeAcknowledger) Nack(_ uint64, _ bool, requeue bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nacks++
	f.requeues = append(f.requeues, requeue)
	return nil
}

func (f *fakeAcknowledger) Reject(_ uint64, requeue bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejects++
	f.requeues = append(f.requeues, requeue)
	return nil
}

func (f *fakeAcknowledger) counts() (acks, nacks, rejects int, requeues []bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acks, f.nacks, f.rejects, append([]bool(nil), f.requeues...)
}

func newDelivery(body []byte, acknowledger amqp.Acknowledger) amqp.Delivery {
	return amqp.Delivery{Acknowledger: acknowledger, DeliveryTag: 1, Body: body}
}

func validMessage(t *testing.T, version int) []byte {
	t.Helper()
	body, err := json.Marshal(events.Envelope{
		ID:         18422,
		Type:       "mentor_slot.booked",
		Version:    version,
		OccurredAt: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC),
		Producer:   "lms-api",
		Data:       json.RawMessage(`{"slot_id":123}`),
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return body
}

func TestProcessConsumesContractFixture(t *testing.T) {
	body, err := os.ReadFile("../events/testdata/mentor_slot_booked.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ack := &fakeAcknowledger{}
	c := New(func(_ context.Context, event events.Envelope) error {
		if event.ID != 18422 || event.Type != "mentor_slot.booked" {
			t.Fatalf("unexpected event: %+v", event)
		}
		return nil
	})
	c.process(context.Background(), newDelivery(body, ack))

	acks, nacks, rejects, _ := ack.counts()
	if acks != 1 || nacks != 0 || rejects != 0 {
		t.Fatalf("acks=%d nacks=%d rejects=%d, want one ack", acks, nacks, rejects)
	}
}

func TestProcessDoesNotAckBeforeHandlerReturns(t *testing.T) {
	ack := &fakeAcknowledger{}
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	c := New(func(_ context.Context, _ events.Envelope) error {
		close(started)
		<-release
		return nil
	})
	go func() {
		c.process(context.Background(), newDelivery(validMessage(t, 1), ack))
		close(done)
	}()

	<-started
	if acks, _, _, _ := ack.counts(); acks != 0 {
		t.Fatalf("acked before the handler returned")
	}
	close(release)
	<-done
	if acks, _, _, _ := ack.counts(); acks != 1 {
		t.Fatalf("acks=%d, want 1", acks)
	}
}

func TestProcessRetriesThenAcks(t *testing.T) {
	ack := &fakeAcknowledger{}
	calls := 0
	c := New(func(_ context.Context, _ events.Envelope) error {
		calls++
		if calls < 3 {
			return channel.TemporarySendError(errors.New("temporary failure"))
		}
		return nil
	})
	c.backoff = 0
	c.process(context.Background(), newDelivery(validMessage(t, 1), ack))

	if calls != 3 {
		t.Fatalf("calls=%d, want 3", calls)
	}
	acks, nacks, _, _ := ack.counts()
	if acks != 1 || nacks != 0 {
		t.Fatalf("acks=%d nacks=%d, want one ack", acks, nacks)
	}
}

func TestProcessDeadLettersAfterRetries(t *testing.T) {
	ack := &fakeAcknowledger{}
	calls := 0
	c := New(func(_ context.Context, _ events.Envelope) error {
		calls++
		return channel.TemporarySendError(errors.New("still failing"))
	})
	c.backoff = 0
	c.process(context.Background(), newDelivery(validMessage(t, 1), ack))

	if calls != maxAttempts {
		t.Fatalf("calls=%d, want %d", calls, maxAttempts)
	}
	acks, nacks, _, requeues := ack.counts()
	if acks != 0 || nacks != 1 {
		t.Fatalf("acks=%d nacks=%d, want one nack", acks, nacks)
	}
	if len(requeues) != 1 || requeues[0] {
		t.Fatalf("requeues=%v, want false", requeues)
	}
}

func TestProcessPermanentFailureNotRetried(t *testing.T) {
	ack := &fakeAcknowledger{}
	calls := 0
	c := New(func(_ context.Context, _ events.Envelope) error {
		calls++
		return channel.PermanentSendError(errors.New("invalid recipient"))
	})
	c.backoff = 0
	c.process(context.Background(), newDelivery(validMessage(t, 1), ack))

	if calls != 1 {
		t.Fatalf("calls=%d, want 1 (permanent failure not retried)", calls)
	}
	acks, nacks, _, requeues := ack.counts()
	if acks != 0 || nacks != 1 {
		t.Fatalf("acks=%d nacks=%d, want one nack", acks, nacks)
	}
	if len(requeues) != 1 || requeues[0] {
		t.Fatalf("requeues=%v, want false (no requeue for permanent failure)", requeues)
	}
}

func TestProcessRejectsMalformedMessage(t *testing.T) {
	ack := &fakeAcknowledger{}
	c := New(func(_ context.Context, _ events.Envelope) error {
		t.Fatal("handler called for malformed message")
		return nil
	})
	c.process(context.Background(), newDelivery([]byte("not json"), ack))

	acks, nacks, _, requeues := ack.counts()
	if acks != 0 || nacks != 1 || len(requeues) != 1 || requeues[0] {
		t.Fatalf("acks=%d nacks=%d requeues=%v, want one non-requeueing nack", acks, nacks, requeues)
	}
}

func TestProcessRejectsUnsupportedVersion(t *testing.T) {
	ack := &fakeAcknowledger{}
	c := New(func(_ context.Context, _ events.Envelope) error {
		t.Fatal("handler called for unsupported version")
		return nil
	})
	c.process(context.Background(), newDelivery(validMessage(t, 2), ack))

	acks, nacks, _, _ := ack.counts()
	if acks != 0 || nacks != 1 {
		t.Fatalf("acks=%d nacks=%d, want one nack", acks, nacks)
	}
}
