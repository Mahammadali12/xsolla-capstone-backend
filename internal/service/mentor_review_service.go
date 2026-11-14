package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/store"
)

// Weekly mentor review: once per module (week), every student in a team with a mentor rates the week with that
// mentor. Rules agreed with the front-end (xsolla-capstone-frontend backend_requirements/BACKEND_REQUIREMENTS.md).

const (
	// MentorReviewOpensBeforeClose is how long before a module closes its review can be sent (Saturday and Sunday).
	MentorReviewOpensBeforeClose = 48 * time.Hour
	// MentorReviewMaxComment is the longest comment, in characters.
	MentorReviewMaxComment = 1000
	// MentorReviewMaxOtherTopic is the longest "other" topic, in characters.
	MentorReviewMaxOtherTopic = 120
)

var (
	// MentorReviewTopics are the things a 1:1 can be about.
	MentorReviewTopics = []string{"goals", "homework", "lecture", "project", "career", "other"}
	// MentorReviewNoSessionReasons are the answers to "why was there no session?".
	MentorReviewNoSessionReasons = []string{"mentor-unavailable", "student-unavailable", "not-scheduled", "other"}
	// MentorReviewStatements are the keys of the four rated statements.
	MentorReviewStatements = []string{"clarity", "engagement", "comfort", "time"}
)

var (
	// ErrMentorReviewForbidden is returned to anyone who is not only a student (staff never review mentors).
	ErrMentorReviewForbidden = errors.New("only students send mentor reviews")
	// ErrMentorReviewModuleNotFound is returned for an unknown or deleted module, or one the student is not enrolled in.
	ErrMentorReviewModuleNotFound = errors.New("module not found")
	// ErrMentorReviewNotOpen is returned before a module's review window opens.
	ErrMentorReviewNotOpen = errors.New("the review of this week is not open yet")
	// ErrMentorReviewExists is returned when the student already reviewed the module.
	ErrMentorReviewExists = store.ErrMentorReviewExists
	// ErrMentorReviewNoTeam is returned when the student is not in a team.
	ErrMentorReviewNoTeam = store.ErrMentorReviewNoTeam
	// ErrMentorReviewNoMentor is returned when the student's team has no mentor.
	ErrMentorReviewNoMentor = store.ErrMentorReviewNoMentor
)

// FieldError is one broken rule of a submitted form.
type FieldError struct {
	Field   string
	Message string
}

// MentorReviewValidationError lists every broken rule of a review.
type MentorReviewValidationError struct {
	Fields []FieldError
}

func (e *MentorReviewValidationError) Error() string {
	if len(e.Fields) == 0 {
		return "invalid mentor review"
	}
	return e.Fields[0].Message
}

// MentorReviewSubmission is the review as the client sent it. A nil field was not sent.
type MentorReviewSubmission struct {
	ModuleID        *int64
	HadSession      *bool
	NoSessionReason *string
	Topics          []string
	OtherTopic      *string
	Ratings         map[string]int
	Overall         *int
	Comment         *string
}

// mentorReviewStore is the part of the store the mentor review service needs.
type mentorReviewStore interface {
	GetModuleByID(ctx context.Context, id int64) (*store.Module, error)
	IsUserEnrolled(ctx context.Context, moduleID int64, userID int64) (bool, error)
	HasMentorReview(ctx context.Context, moduleID, studentID int64) (bool, error)
	CreateMentorReview(ctx context.Context, in store.MentorReviewInput) (*store.MentorReview, error)
	ListReviewedModules(ctx context.Context, studentID int64) ([]store.ReviewedModule, error)
}

// MentorReviewService handles the weekly mentor reviews.
type MentorReviewService struct {
	store mentorReviewStore
	now   func() time.Time
}

// NewMentorReviewService creates the service.
func NewMentorReviewService(s mentorReviewStore) *MentorReviewService {
	return &MentorReviewService{store: s, now: time.Now}
}

// ListMine returns the modules the student has already reviewed.
func (s *MentorReviewService) ListMine(ctx context.Context, userID int64, roles []string) ([]store.ReviewedModule, error) {
	if !isOnlyStudent(roles) {
		return nil, ErrMentorReviewForbidden
	}
	return s.store.ListReviewedModules(ctx, userID)
}

// Submit validates and stores one week's review. The mentor is the mentor of the student's team right now.
func (s *MentorReviewService) Submit(ctx context.Context, userID int64, roles []string, sub MentorReviewSubmission) (*store.MentorReview, error) {
	if !isOnlyStudent(roles) {
		return nil, ErrMentorReviewForbidden
	}
	in, fields := ValidateMentorReview(sub)
	if len(fields) > 0 {
		return nil, &MentorReviewValidationError{Fields: fields}
	}
	in.StudentID = userID

	module, err := s.store.GetModuleByID(ctx, in.ModuleID)
	if errors.Is(err, store.ErrModuleNotFound) {
		return nil, ErrMentorReviewModuleNotFound
	}
	if err != nil {
		return nil, err
	}
	enrolled, err := s.store.IsUserEnrolled(ctx, module.ID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		return nil, ErrMentorReviewModuleNotFound
	}

	// "Already sent" wins over every other answer: the front-end treats 409 as done.
	exists, err := s.store.HasMentorReview(ctx, module.ID, userID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrMentorReviewExists
	}

	if opens := MentorReviewOpensAt(module.OpensAt, module.ClosesAt); s.now().Before(opens) {
		return nil, fmt.Errorf("%w: it opens at %s", ErrMentorReviewNotOpen, opens.UTC().Format(time.RFC3339))
	}

	return s.store.CreateMentorReview(ctx, in)
}

// MentorReviewOpensAt is when a module's review can first be sent: 48 hours before it closes (the weekend),
// but never before the module opens. There is no end: a missing review can be sent at any time later.
func MentorReviewOpensAt(opensAt, closesAt time.Time) time.Time {
	from := closesAt.Add(-MentorReviewOpensBeforeClose)
	if from.Before(opensAt) {
		return opensAt
	}
	return from
}

// isOnlyStudent: mentors, lecturers and admins are never asked, even when they also hold the student role.
func isOnlyStudent(roles []string) bool {
	return auth.HasRole(roles, "student") &&
		!auth.HasRole(roles, "mentor") && !auth.HasRole(roles, "lecturer") && !auth.HasRole(roles, "admin")
}

// ValidateMentorReview checks a submission against the form's rules and returns it cleaned up (trimmed text,
// empty optional text dropped). Every broken rule is reported, in a fixed order.
func ValidateMentorReview(sub MentorReviewSubmission) (store.MentorReviewInput, []FieldError) {
	var in store.MentorReviewInput
	var errs []FieldError
	add := func(field, format string, args ...any) {
		errs = append(errs, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
	}

	switch {
	case sub.ModuleID == nil:
		add("module_id", "module_id is required")
	case *sub.ModuleID <= 0:
		add("module_id", "module_id must be a positive id")
	default:
		in.ModuleID = *sub.ModuleID
	}

	reason := trimmed(sub.NoSessionReason)
	otherTopic := trimmed(sub.OtherTopic)

	if sub.HadSession == nil {
		add("had_session", "had_session is required (true or false)")
	} else if *sub.HadSession {
		in.HadSession = true
		if reason != "" {
			add("no_session_reason", "no_session_reason only applies when had_session is false")
		}
		in.Topics = validateTopics(sub.Topics, add)
		hasOther := contains(in.Topics, "other")
		switch {
		case hasOther && otherTopic == "":
			add("other_topic", "other_topic is required when the topics include \"other\"")
		case hasOther && utf8.RuneCountInString(otherTopic) > MentorReviewMaxOtherTopic:
			add("other_topic", "other_topic must be at most %d characters", MentorReviewMaxOtherTopic)
		case hasOther:
			in.OtherTopic = &otherTopic
		case otherTopic != "":
			add("other_topic", "other_topic only applies when the topics include \"other\"")
		}
		in.Ratings = validateRatings(sub.Ratings, add)
	} else {
		in.HadSession = false
		switch {
		case reason == "":
			add("no_session_reason", "no_session_reason is required when had_session is false")
		case !contains(MentorReviewNoSessionReasons, reason):
			add("no_session_reason", "no_session_reason must be one of %s", strings.Join(MentorReviewNoSessionReasons, ", "))
		default:
			in.NoSessionReason = &reason
		}
		if len(sub.Topics) > 0 {
			add("topics", "topics only apply when had_session is true")
		}
		if otherTopic != "" {
			add("other_topic", "other_topic only applies when had_session is true")
		}
		if len(sub.Ratings) > 0 {
			add("ratings", "ratings only apply when had_session is true")
		}
	}

	switch {
	case sub.Overall == nil:
		add("overall", "overall is required")
	case *sub.Overall < 1 || *sub.Overall > 5:
		add("overall", "overall must be between 1 and 5")
	default:
		in.Overall = *sub.Overall
	}

	if comment := trimmed(sub.Comment); comment != "" {
		if utf8.RuneCountInString(comment) > MentorReviewMaxComment {
			add("comment", "comment must be at most %d characters", MentorReviewMaxComment)
		} else {
			in.Comment = &comment
		}
	}

	return in, errs
}

func validateTopics(topics []string, add func(field, format string, args ...any)) []string {
	if len(topics) == 0 {
		add("topics", "pick at least one topic when had_session is true")
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, t := range topics {
		t = strings.TrimSpace(t)
		switch {
		case !contains(MentorReviewTopics, t):
			add("topics", "unknown topic %q: use %s", t, strings.Join(MentorReviewTopics, ", "))
		case seen[t]:
			add("topics", "topic %q is listed twice", t)
		default:
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func validateRatings(ratings map[string]int, add func(field, format string, args ...any)) *store.MentorReviewRatings {
	if len(ratings) == 0 {
		add("ratings", "ratings are required when had_session is true (%s)", strings.Join(MentorReviewStatements, ", "))
		return nil
	}
	// unknown keys first, sorted, so the order of errors does not depend on map order
	var unknown []string
	for k := range ratings {
		if !contains(MentorReviewStatements, k) {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		add("ratings."+k, "%q is not a rated statement: use %s", k, strings.Join(MentorReviewStatements, ", "))
	}

	ok := len(unknown) == 0
	score := func(key string) int {
		v, present := ratings[key]
		switch {
		case !present:
			add("ratings."+key, "ratings.%s is required", key)
			ok = false
		case v < 1 || v > 5:
			add("ratings."+key, "ratings.%s must be between 1 and 5", key)
			ok = false
		}
		return v
	}
	r := store.MentorReviewRatings{
		Clarity:    score("clarity"),
		Engagement: score("engagement"),
		Comfort:    score("comfort"),
		Time:       score("time"),
	}
	if !ok {
		return nil
	}
	return &r
}

func trimmed(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
