package service

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"xsolla-capstone-api/internal/store"
)

// Announcements: an admin writes a short message, optionally pointing to an LMS page, for everyone or one group.
// The notification service delivers it as an in-app notification and an email (event announcement.created).

const (
	maxBroadcastMessage = 500
	maxBroadcastLink    = 200
)

// BroadcastAudiences are who an announcement can go to.
var BroadcastAudiences = []string{"all", "students", "mentors", "lecturers"}

// BroadcastInvalidError lists every problem with an announcement, field by field.
type BroadcastInvalidError struct {
	Fields []ReportFieldError
}

func (e *BroadcastInvalidError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Message
	}
	return strings.Join(parts, "; ")
}

type broadcastStore interface {
	CreateBroadcast(ctx context.Context, message, link, audience string, sentBy int64) (*store.Broadcast, error)
	ListBroadcasts(ctx context.Context) ([]store.Broadcast, error)
}

// BroadcastService sends and lists announcements (admins only; the routes check the role).
type BroadcastService struct {
	store broadcastStore
}

// NewBroadcastService creates the announcement service.
func NewBroadcastService(s broadcastStore) *BroadcastService {
	return &BroadcastService{store: s}
}

// validBroadcastLink accepts "" or a page of the LMS ("/calendar"): never another site.
func validBroadcastLink(link string) bool {
	if link == "" {
		return true
	}
	if !strings.HasPrefix(link, "/") || strings.HasPrefix(link, "//") || len(link) > maxBroadcastLink {
		return false
	}
	return !strings.ContainsFunc(link, func(r rune) bool { return unicode.IsSpace(r) || r == '\\' })
}

// Send stores an announcement and hands it to the notification service.
func (s *BroadcastService) Send(ctx context.Context, adminID int64, message, link, audience string) (*store.Broadcast, error) {
	var problems []ReportFieldError
	message = strings.TrimSpace(message)
	switch {
	case message == "":
		problems = append(problems, ReportFieldError{"message", "message is required"})
	case utf8.RuneCountInString(message) > maxBroadcastMessage:
		problems = append(problems, ReportFieldError{"message", fmt.Sprintf("message must be at most %d characters", maxBroadcastMessage)})
	}
	link = strings.TrimSpace(link)
	if !validBroadcastLink(link) {
		problems = append(problems, ReportFieldError{"link", "link must be a page of the LMS such as /calendar, or empty"})
	}
	known := false
	for _, a := range BroadcastAudiences {
		known = known || a == audience
	}
	if !known {
		problems = append(problems, ReportFieldError{"audience", "audience must be all, students, mentors or lecturers"})
	}
	if len(problems) > 0 {
		return nil, &BroadcastInvalidError{Fields: problems}
	}
	return s.store.CreateBroadcast(ctx, message, link, audience, adminID)
}

// List returns every announcement, newest first.
func (s *BroadcastService) List(ctx context.Context) ([]store.Broadcast, error) {
	return s.store.ListBroadcasts(ctx)
}
