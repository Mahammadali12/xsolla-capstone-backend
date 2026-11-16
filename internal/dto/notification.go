package dto

import "time"

// NotificationResponse is one in-app notification. It is rendered from the same template as the email for the same
// event: Title is the email's subject and Message its plain text body.
type NotificationResponse struct {
	ID        int64      `json:"id"`
	Type      string     `json:"type"` // the event, e.g. enrollment.graded, module.created, mentor_slot.booked
	Title     string     `json:"title"`
	Message   string     `json:"message"`
	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at"`
}

// MarkAllNotificationsReadResponse says how many notifications POST /notifications/read-all marked read.
type MarkAllNotificationsReadResponse struct {
	Marked int64 `json:"marked"`
}
