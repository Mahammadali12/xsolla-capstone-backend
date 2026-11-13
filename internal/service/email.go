package service

import (
	"errors"
	"regexp"
	"strings"
)

// ErrInvalidEmail is returned when an address fails the format/length checks.
var ErrInvalidEmail = errors.New("enter a valid email address")

// emailPattern mirrors the frontend rule: one @, no whitespace, a dot in the domain.
var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// normalizeEmail trims surrounding spaces and lowercases the address.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail checks a normalized address: the frontend's format, plus the
// 254-character total and 64-character local-part limits.
func ValidateEmail(email string) error {
	if email == "" || len(email) > 254 {
		return ErrInvalidEmail
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 || len(email[:at]) > 64 {
		return ErrInvalidEmail
	}
	if !emailPattern.MatchString(email) {
		return ErrInvalidEmail
	}
	return nil
}
