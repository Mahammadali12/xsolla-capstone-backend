package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// AnnouncementCreatedEventType is emitted when an admin sends an announcement.
const AnnouncementCreatedEventType = "announcement.created"

// Broadcast is an announcement an admin sent.
type Broadcast struct {
	ID         int64
	Message    string
	Link       string // an LMS page such as /calendar, or ""
	Audience   string // all, students, mentors or lecturers
	SentBy     *int64
	SentByName string
	Recipients int
	SentAt     time.Time
}

// audienceRoles is who an audience reaches. Admins are never in it (they do not use the pages the bell is in).
var audienceRoles = map[string][]string{
	"all":       {"student", "mentor", "lecturer"},
	"students":  {"student"},
	"mentors":   {"mentor"},
	"lecturers": {"lecturer"},
}

// audienceRecipients returns the active people an announcement reaches, each once, with the role it reaches them in.
func (q *Query) audienceRecipients(ctx context.Context, audience string) ([]recipientSnapshot, error) {
	roles, ok := audienceRoles[audience]
	if !ok {
		return nil, fmt.Errorf("unknown audience %q", audience)
	}
	rows, err := q.QueryContext(ctx, `
		SELECT u.id, u.name, u.email, array_agg(ur.role ORDER BY ur.role)
		FROM users u
		JOIN user_roles ur ON ur.user_id = u.id AND (ur.expires_at IS NULL OR ur.expires_at > NOW())
		WHERE u.deleted_at IS NULL AND u.left_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM user_roles a WHERE a.user_id = u.id AND a.role = 'admin'
		                  AND (a.expires_at IS NULL OR a.expires_at > NOW()))
		GROUP BY u.id, u.name, u.email
		HAVING bool_or(ur.role = ANY($1))
		ORDER BY u.id`, pq.Array(roles))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []recipientSnapshot{}
	for rows.Next() {
		var r recipientSnapshot
		var held []string
		if err := rows.Scan(&r.ID, &r.Name, &r.Email, pq.Array(&held)); err != nil {
			return nil, err
		}
		// the role the announcement reaches them in, so the notification policy matches them
		for _, want := range roles {
			for _, h := range held {
				if h == want && r.Role == "" {
					r.Role = want
				}
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateBroadcast stores an announcement and, in the same transaction, the event that delivers it to its audience.
// With nobody in the audience it is stored with 0 recipients and no event.
func (s *StoreImpl) CreateBroadcast(ctx context.Context, message, link, audience string, sentBy int64) (*Broadcast, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := WithTx(tx)

	recipients, err := q.audienceRecipients(ctx, audience)
	if err != nil {
		return nil, err
	}
	b := Broadcast{Message: message, Link: link, Audience: audience, SentBy: &sentBy, Recipients: len(recipients)}
	if err := q.QueryRowContext(ctx,
		`INSERT INTO broadcasts (message, link, audience, sent_by, recipients) VALUES ($1, $2, $3, $4, $5) RETURNING id, sent_at`,
		message, link, audience, sentBy, len(recipients),
	).Scan(&b.ID, &b.SentAt); err != nil {
		return nil, err
	}
	senderName, _, err := q.userNameEmail(ctx, sentBy)
	if err != nil {
		return nil, err
	}
	b.SentByName = senderName

	if len(recipients) > 0 {
		payload, err := json.Marshal(map[string]any{
			"broadcast_id": b.ID,
			"message":      message,
			"link":         link,
			"audience":     audience,
			"sent_by":      sentBy,
			"sent_by_name": senderName,
			"recipients":   recipients,
		})
		if err != nil {
			return nil, err
		}
		if err := q.InsertOutboxEvent(ctx, AnnouncementCreatedEventType, payload); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &b, nil
}

// ListBroadcasts returns every announcement, newest first.
func (s *StoreImpl) ListBroadcasts(ctx context.Context) ([]Broadcast, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id, b.message, b.link, b.audience, b.sent_by, u.name, b.recipients, b.sent_at
		FROM broadcasts b LEFT JOIN users u ON u.id = b.sent_by
		ORDER BY b.sent_at DESC, b.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []Broadcast{}
	for rows.Next() {
		var b Broadcast
		var name sql.NullString
		if err := rows.Scan(&b.ID, &b.Message, &b.Link, &b.Audience, &b.SentBy, &name, &b.Recipients, &b.SentAt); err != nil {
			return nil, err
		}
		b.SentByName = name.String
		list = append(list, b)
	}
	return list, rows.Err()
}
