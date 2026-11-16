package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrNotificationNotFound means the notification does not exist or belongs to someone else.
var ErrNotificationNotFound = errors.New("notification not found")

// Notification is one in-app notification. The notification worker writes them, rendered from the same policies and
// templates as its emails, into the notifications table of its own database
// (notification-service/internal/store/schema.sql). The LMS only reads them and marks them read, for their own user.
type Notification struct {
	ID        int64
	EventType string // e.g. enrollment.graded
	Title     string // the template's subject
	Body      string // the template's plain text body
	CreatedAt time.Time
	ReadAt    *time.Time
}

// NotificationStore reads the notification database (not the LMS database).
type NotificationStore struct {
	db *sql.DB
}

// NewNotificationStore creates a NotificationStore on the notification database.
func NewNotificationStore(db *sql.DB) *NotificationStore {
	return &NotificationStore{db: db}
}

// ListUnreadNotifications returns the user's unread notifications, newest first, at most limit.
func (s *NotificationStore) ListUnreadNotifications(ctx context.Context, userID int64, limit int) ([]Notification, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, title, body, created_at, read_at
		 FROM notifications
		 WHERE recipient_id = $1 AND read_at IS NULL
		 ORDER BY created_at DESC, id DESC
		 LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []Notification{}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.EventType, &n.Title, &n.Body, &n.CreatedAt, &n.ReadAt); err != nil {
			return nil, err
		}
		list = append(list, n)
	}
	return list, rows.Err()
}

// MarkNotificationRead marks one of the user's notifications read. Marking it again keeps the first read time.
func (s *NotificationStore) MarkNotificationRead(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = COALESCE(read_at, NOW()) WHERE id = $1 AND recipient_id = $2`,
		id, userID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotificationNotFound
	}
	return nil
}

// MarkAllNotificationsRead marks every unread notification of the user read and returns how many there were.
func (s *NotificationStore) MarkAllNotificationsRead(ctx context.Context, userID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = NOW() WHERE recipient_id = $1 AND read_at IS NULL`,
		userID,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
