package store

import (
	"database/sql"
)

// Store wraps the database connection and provides access to queries.
type Store struct {
	db *sql.DB
}

// NewStore creates a Store from an existing *sql.DB.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// DB returns a Query bound to this store's connection (works in transactions too).
func (s *Store) DB() *Query {
	return &Query{DBTX: s.db}
}

// WithTx returns a Query bound to the given transaction.
func WithTx(tx *sql.Tx) *Query {
	return &Query{DBTX: tx}
}
