package service

import (
	"context"
	"errors"

	"xsolla-capstone-api/internal/store"
)

// NotificationStore is the part of the notification database the in-app notification routes use.
type NotificationStore interface {
	ListUnreadNotifications(ctx context.Context, userID int64, limit int) ([]store.Notification, error)
	MarkNotificationRead(ctx context.Context, userID, id int64) error
	MarkAllNotificationsRead(ctx context.Context, userID int64) (int64, error)
}

// ErrNotificationsUnavailable means NOTIFY_DATABASE_URL is not set, so there is no notification database to read.
var ErrNotificationsUnavailable = errors.New("in-app notifications are not configured on this server")

// ErrNotificationNotFound is re-exported from the store package for handler use.
var ErrNotificationNotFound = store.ErrNotificationNotFound

// Unread list size: the default, and the most one request may ask for.
const (
	defaultUnreadNotifications = 50
	maxUnreadNotifications     = 100
)

// NotificationService serves each user their own in-app notifications.
type NotificationService struct {
	store NotificationStore // nil when the notification database is not configured
}

// NewNotificationService creates the service. A nil store makes every call return ErrNotificationsUnavailable.
func NewNotificationService(s NotificationStore) *NotificationService {
	return &NotificationService{store: s}
}

// Unread returns the user's unread notifications, newest first. limit 0 means the default; it is capped at the maximum.
func (s *NotificationService) Unread(ctx context.Context, userID int64, limit int) ([]store.Notification, error) {
	if s.store == nil {
		return nil, ErrNotificationsUnavailable
	}
	if limit <= 0 {
		limit = defaultUnreadNotifications
	}
	if limit > maxUnreadNotifications {
		limit = maxUnreadNotifications
	}
	return s.store.ListUnreadNotifications(ctx, userID, limit)
}

// MarkRead marks one of the user's notifications read (ErrNotificationNotFound for someone else's).
func (s *NotificationService) MarkRead(ctx context.Context, userID, id int64) error {
	if s.store == nil {
		return ErrNotificationsUnavailable
	}
	return s.store.MarkNotificationRead(ctx, userID, id)
}

// MarkAllRead marks all of the user's unread notifications read and returns how many there were.
func (s *NotificationService) MarkAllRead(ctx context.Context, userID int64) (int64, error) {
	if s.store == nil {
		return 0, ErrNotificationsUnavailable
	}
	return s.store.MarkAllNotificationsRead(ctx, userID)
}
