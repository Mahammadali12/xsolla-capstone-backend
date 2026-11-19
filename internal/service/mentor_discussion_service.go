package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/store"
)

var (
	// ErrOnlyMentorCanSetDiscussions is returned when someone who is not a mentor tries to set up a discussion.
	ErrOnlyMentorCanSetDiscussions = errors.New("only mentors can set up a discussion with students")
	// ErrDiscussionNotFound is returned when the discussion does not exist or the caller may not remove it.
	ErrDiscussionNotFound = errors.New("discussion not found")
)

// A discussion is not a 1:1: no Friday/Monday rule, no booking, and it never moves a deadline (one_on_one_deadline.go reads
// only the booked 1:1 slots). It is kept short so it stays a conversation.
const (
	maxDiscussionTitle  = 100
	maxDiscussionNote   = 500
	minDiscussionLength = 10 * time.Minute
	maxDiscussionLength = 4 * time.Hour
)

// DiscussionService sets up discussions between a mentor and the students of their team.
type DiscussionService struct {
	store store.Store
}

func NewDiscussionService(s store.Store) *DiscussionService {
	return &DiscussionService{store: s}
}

// List returns the discussions the caller may see.
func (s *DiscussionService) List(ctx context.Context, userID int64, roles []string) ([]store.Discussion, error) {
	return s.store.ListMentorDiscussions(ctx, userID, roles)
}

// Create sets up a discussion with the chosen students (all of them must be in the mentor's team) or the whole team.
func (s *DiscussionService) Create(ctx context.Context, mentorID int64, roles []string, title, note string, startsAt, endsAt time.Time, studentIDs []int64, wholeTeam bool) ([]store.Discussion, error) {
	if !auth.HasRole(roles, "mentor") {
		return nil, ErrOnlyMentorCanSetDiscussions
	}
	title, note = strings.TrimSpace(title), strings.TrimSpace(note)
	if title == "" {
		return nil, errors.New("title is required")
	}
	if utf8.RuneCountInString(title) > maxDiscussionTitle {
		return nil, errors.New("title can be at most 100 characters")
	}
	if utf8.RuneCountInString(note) > maxDiscussionNote {
		return nil, errors.New("note can be at most 500 characters")
	}
	if startsAt.IsZero() || endsAt.IsZero() {
		return nil, errors.New("starts_at and ends_at are required")
	}
	if !endsAt.After(startsAt) {
		return nil, errors.New("end time must be after start time")
	}
	if length := endsAt.Sub(startsAt); length < minDiscussionLength || length > maxDiscussionLength {
		return nil, errors.New("a discussion lasts from 10 minutes to 4 hours")
	}
	if !startsAt.After(time.Now()) {
		return nil, errors.New("that time has already passed")
	}

	team, err := s.store.GetMentorTeamDetail(ctx, mentorID)
	if err != nil {
		if errors.Is(err, store.ErrTeamNotFound) {
			return nil, errors.New("you have no team to invite")
		}
		return nil, err
	}
	inTeam := make(map[int64]bool, len(team.Members))
	for _, m := range team.Members {
		inTeam[m.ID] = true
	}
	chosen := studentIDs
	if wholeTeam {
		chosen = nil
		for _, m := range team.Members {
			chosen = append(chosen, m.ID)
		}
	}
	seen := make(map[int64]bool, len(chosen))
	ids := make([]int64, 0, len(chosen))
	for _, id := range chosen {
		if !inTeam[id] {
			return nil, errors.New("you can only invite students of your own team")
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("choose at least one student")
	}
	return s.store.CreateMentorDiscussions(ctx, mentorID, ids, title, note, startsAt, endsAt)
}

// Remove cancels a discussion (for every student invited to it): the mentor who set it up, or an admin.
func (s *DiscussionService) Remove(ctx context.Context, userID int64, roles []string, id int64) error {
	if !auth.HasRole(roles, "mentor") && !auth.HasRole(roles, "admin") {
		return ErrOnlyMentorCanSetDiscussions
	}
	done, err := s.store.DeleteMentorDiscussion(ctx, userID, auth.HasRole(roles, "admin"), id)
	if err != nil {
		return err
	}
	if !done {
		return ErrDiscussionNotFound
	}
	return nil
}
