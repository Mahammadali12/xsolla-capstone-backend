package service

import (
	"context"
	"errors"
	"testing"

	"xsolla-capstone-api/internal/store"
)

type ntfStore struct {
	limit   int
	userID  int64
	readID  int64
	readErr error
}

func (f *ntfStore) ListUnreadNotifications(_ context.Context, userID int64, limit int) ([]store.Notification, error) {
	f.userID, f.limit = userID, limit
	return []store.Notification{{ID: 1}}, nil
}

func (f *ntfStore) MarkNotificationRead(_ context.Context, userID, id int64) error {
	f.userID, f.readID = userID, id
	return f.readErr
}

func (f *ntfStore) MarkAllNotificationsRead(_ context.Context, userID int64) (int64, error) {
	f.userID = userID
	return 3, nil
}

func TestNotificationsWithoutTheirDatabase(t *testing.T) {
	svc := NewNotificationService(nil)
	ctx := context.Background()
	if _, err := svc.Unread(ctx, 1, 0); !errors.Is(err, ErrNotificationsUnavailable) {
		t.Errorf("Unread: %v", err)
	}
	if err := svc.MarkRead(ctx, 1, 2); !errors.Is(err, ErrNotificationsUnavailable) {
		t.Errorf("MarkRead: %v", err)
	}
	if _, err := svc.MarkAllRead(ctx, 1); !errors.Is(err, ErrNotificationsUnavailable) {
		t.Errorf("MarkAllRead: %v", err)
	}
}

func TestNotificationsUnreadLimit(t *testing.T) {
	for _, c := range []struct{ asked, want int }{{0, 50}, {-3, 50}, {10, 10}, {100, 100}, {500, 100}} {
		st := &ntfStore{}
		if _, err := NewNotificationService(st).Unread(context.Background(), 9, c.asked); err != nil {
			t.Fatal(err)
		}
		if st.limit != c.want || st.userID != 9 {
			t.Errorf("asked %d: store got limit %d for user %d, want %d for 9", c.asked, st.limit, st.userID, c.want)
		}
	}
}

func TestNotificationsMarkReadForTheCaller(t *testing.T) {
	st := &ntfStore{readErr: store.ErrNotificationNotFound}
	err := NewNotificationService(st).MarkRead(context.Background(), 9, 41)
	if !errors.Is(err, ErrNotificationNotFound) || st.userID != 9 || st.readID != 41 {
		t.Errorf("err %v, user %d, id %d", err, st.userID, st.readID)
	}
	if n, err := NewNotificationService(st).MarkAllRead(context.Background(), 9); err != nil || n != 3 {
		t.Errorf("MarkAllRead: %d, %v", n, err)
	}
}
