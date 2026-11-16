// Package store persists notification delivery state in Postgres.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"

	_ "github.com/lib/pq"

	"xsolla-notification-service/internal/channel"
)

//go:embed schema.sql
var schema string

// Store owns the notification database.
type Store struct {
	db *sql.DB
}

// Open connects, verifies the connection, and creates the schema.
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// Ping reports whether the database answers.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// ClaimDelivery reserves one logical delivery and increments its attempt count.
// claimed is false when the delivery already succeeded, so a redelivered event
// never sends twice. The unique constraint makes the claim atomic.
func (s *Store) ClaimDelivery(ctx context.Context, producer string, eventID int64, eventType string, recipientID int64, channel string) (int64, bool, error) {
	const query = `
		INSERT INTO deliveries (producer, event_id, event_type, recipient_id, channel, status, attempts)
		VALUES ($1, $2, $3, $4, $5, 'pending', 1)
		ON CONFLICT (producer, event_id, recipient_id, channel)
		DO UPDATE SET status = 'pending', attempts = deliveries.attempts + 1
		WHERE deliveries.status <> 'sent'
		RETURNING id`

	var id int64
	err := s.db.QueryRowContext(ctx, query, producer, eventID, eventType, recipientID, channel).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// MarkDeliverySent records a successful delivery.
func (s *Store) MarkDeliverySent(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE deliveries SET status = 'sent', sent_at = now(), last_error = NULL WHERE id = $1`, id)
	return err
}

// SaveNotification keeps one in-app notification. A redelivered event finds its
// row already there and changes nothing, so a read notification stays read.
func (s *Store) SaveNotification(ctx context.Context, n channel.InAppNotification) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notifications (producer, event_id, event_type, recipient_id, title, body, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (producer, event_id, recipient_id) DO NOTHING`,
		n.Producer, n.EventID, n.EventType, n.RecipientID, n.Title, n.Body, n.CreatedAt)
	return err
}

// MarkDeliveryFailed records a failed attempt and its cause.
func (s *Store) MarkDeliveryFailed(ctx context.Context, id int64, cause string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE deliveries SET status = 'failed', last_error = $2 WHERE id = $1`, id, cause)
	return err
}
