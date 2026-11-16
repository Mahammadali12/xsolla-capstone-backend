package channel

import (
	"context"
	"errors"
	"testing"
	"time"

	"xsolla-notification-service/internal/events"
)

type memInApp struct {
	saved []InAppNotification
	err   error
}

func (m *memInApp) SaveNotification(_ context.Context, n InAppNotification) error {
	m.saved = append(m.saved, n)
	return m.err
}

func TestInAppStoresTheRenderedMessage(t *testing.T) {
	mem := &memInApp{}
	at := time.Date(2026, 11, 16, 9, 30, 0, 0, time.UTC)
	err := NewInApp(mem).Send(context.Background(),
		events.Recipient{ID: 42, Role: "student", Name: "Student One", Email: "student@example.com"},
		Message{EventID: 9, Subject: "Grade posted for Backend", Body: "Hi Student One,\n\nMentor One posted your grade.\n",
			HTMLBody: "<p>ignored</p>", Producer: "lms-api", EventType: "enrollment.graded", OccurredAt: at})
	if err != nil {
		t.Fatal(err)
	}
	want := InAppNotification{Producer: "lms-api", EventID: 9, EventType: "enrollment.graded", RecipientID: 42,
		Title: "Grade posted for Backend", Body: "Hi Student One,\n\nMentor One posted your grade.", CreatedAt: at}
	if len(mem.saved) != 1 || mem.saved[0] != want {
		t.Fatalf("saved %+v, want %+v", mem.saved, want)
	}
}

func TestInAppWithoutEventTimeUsesNow(t *testing.T) {
	mem := &memInApp{}
	if err := NewInApp(mem).Send(context.Background(), events.Recipient{ID: 7}, Message{Subject: "s", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if got := mem.saved[0].CreatedAt; time.Since(got) > time.Minute {
		t.Fatalf("created_at %v, want about now", got)
	}
}

func TestInAppRefusesAnUnknownRecipientAndReportsStoreErrors(t *testing.T) {
	mem := &memInApp{}
	if err := NewInApp(mem).Send(context.Background(), events.Recipient{ID: 0}, Message{Subject: "s"}); err == nil || len(mem.saved) != 0 {
		t.Fatalf("recipient 0: err=%v saved=%d, want an error and nothing saved", err, len(mem.saved))
	}
	want := errors.New("database down")
	if err := NewInApp(&memInApp{err: want}).Send(context.Background(), events.Recipient{ID: 7}, Message{}); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v so the dispatcher retries", err, want)
	}
}
