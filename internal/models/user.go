package models

import "time"

// User is the domain model for a registered user.
type User struct {
	ID          int64
	Email       string
	Name        string
	Roles       []string
	PasswordHash string
	CreatedAt   time.Time
}
