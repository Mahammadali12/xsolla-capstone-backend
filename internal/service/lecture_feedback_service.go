package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/store"
)

// Lecture feedback: after each lecture (an event of kind "lecture") a student fills a short form about it. One
// form per student and lecture; only admins read them (lecturers do not).

const (
	// LectureFeedbackOpensBeforeEnd lets students fill the form in the last minutes of the lecture.
	LectureFeedbackOpensBeforeEnd = 10 * time.Minute
	// LectureFeedbackOpenFor is how long after the lecture's end the form stays open.
	LectureFeedbackOpenFor = 7 * 24 * time.Hour
	// LectureFeedbackMaxText is the longest answer to a text question, in characters.
	LectureFeedbackMaxText = 1000
)

// LectureFeedbackPaces are the answers to "How was the pace?".
var LectureFeedbackPaces = []string{"too_slow", "just_right", "too_fast"}

// lectureFeedbackZone is the school's time zone for the messages (Azerbaijan: UTC+4 all year).
var lectureFeedbackZone = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Baku"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Baku", 4*60*60)
}()

var (
	// ErrLectureFeedbackForbidden is returned to anyone who is not only a student.
	ErrLectureFeedbackForbidden = errors.New("only students give lecture feedback")
	// ErrLectureFeedbackNotFound is returned for an unknown lecture, or one of a week the student is not enrolled in.
	ErrLectureFeedbackNotFound = errors.New("lecture not found")
	// ErrLectureFeedbackExists is returned when the student already sent feedback for the lecture.
	ErrLectureFeedbackExists = store.ErrLectureFeedbackExists
	// ErrLectureFeedbackNotOpen is returned before the form opens.
	ErrLectureFeedbackNotOpen = errors.New("feedback for this lecture is not open yet")
	// ErrLectureFeedbackClosed is returned after the form closed.
	ErrLectureFeedbackClosed = errors.New("feedback for this lecture is closed")
)

// ---- the form ----

// LectureFeedbackQuestion is one question as the front-end draws it.
type LectureFeedbackQuestion struct {
	ID        string                  `json:"id"`
	Type      string                  `json:"type"` // "scale" (1-5), "choice" or "text"
	Label     string                  `json:"label"`
	Required  bool                    `json:"required"`
	MinLabel  string                  `json:"min_label,omitempty"`
	MaxLabel  string                  `json:"max_label,omitempty"`
	Options   []LectureFeedbackOption `json:"options,omitempty"`
	MaxLength int                     `json:"max_length,omitempty"`
}

// LectureFeedbackOption is one answer of a "choice" question.
type LectureFeedbackOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// LectureFeedbackForm is the form, in the order it is shown. The ids are the JSON fields of POST /lecture-feedback.
var LectureFeedbackForm = []LectureFeedbackQuestion{
	{ID: "overall", Type: "scale", Required: true, Label: "How would you rate this lecture overall?",
		MinLabel: "Very bad", MaxLabel: "Very good"},
	{ID: "clarity", Type: "scale", Required: true, Label: "How clear were the explanations?",
		MinLabel: "Very unclear", MaxLabel: "Very clear"},
	{ID: "practice", Type: "scale", Required: true, Label: "How useful were the examples, live coding and exercises?",
		MinLabel: "Not useful", MaxLabel: "Very useful"},
	{ID: "pace", Type: "choice", Required: true, Label: "How was the pace of the lecture?",
		Options: []LectureFeedbackOption{{"too_slow", "Too slow"}, {"just_right", "Just right"}, {"too_fast", "Too fast"}}},
	{ID: "confidence", Type: "scale", Required: true,
		Label:    "How confident do you feel about this topic now, including your ability to complete the home task?",
		MinLabel: "Not confident at all", MaxLabel: "Very confident"},
	{ID: "liked", Type: "text", Required: false, MaxLength: LectureFeedbackMaxText,
		Label: "What worked well? What should the lecturer keep doing?"},
	{ID: "improve", Type: "text", Required: false, MaxLength: LectureFeedbackMaxText,
		Label: "What was unclear, or what could be improved?"},
}

// ---- sending ----

// LectureFeedbackSubmission is the form as the client sent it; nil fields were not sent.
type LectureFeedbackSubmission struct {
	EventID    *int64
	Overall    *int
	Clarity    *int
	Practice   *int
	Confidence *int
	Pace       *string
	Liked      *string
	Improve    *string
}

// LectureFeedbackProblem is one broken rule of a sent form.
type LectureFeedbackProblem struct {
	Field   string
	Message string
}

// LectureFeedbackInvalidError lists every broken rule.
type LectureFeedbackInvalidError struct{ Problems []LectureFeedbackProblem }

func (e *LectureFeedbackInvalidError) Error() string {
	if len(e.Problems) == 0 {
		return "invalid lecture feedback"
	}
	return e.Problems[0].Message
}

type lectureFeedbackStore interface {
	GetLecture(ctx context.Context, eventID int64) (*store.Lecture, error)
	IsUserEnrolled(ctx context.Context, moduleID int64, userID int64) (bool, error)
	HasLectureFeedback(ctx context.Context, eventID, studentID int64) (bool, error)
	CreateLectureFeedback(ctx context.Context, in store.LectureFeedbackInput) (*store.SentLectureFeedback, error)
	ListSentLectureFeedback(ctx context.Context, studentID int64) ([]store.SentLectureFeedback, error)
	ListPendingLectures(ctx context.Context, studentID int64, now time.Time, opensBefore, openFor time.Duration) ([]store.Lecture, error)
	ListLectureFeedback(ctx context.Context, f store.LectureFeedbackFilter) ([]store.LectureFeedbackRow, error)
	ListLecturesWithAudience(ctx context.Context, f store.LectureFeedbackFilter) ([]store.LectureWithAudience, error)
	ListWeekLecturers(ctx context.Context, moduleIDs []int64) (map[int64][]store.LectureFeedbackPerson, error)
}

// LectureFeedbackService handles the lecture feedback forms.
type LectureFeedbackService struct {
	store lectureFeedbackStore
	now   func() time.Time
}

// NewLectureFeedbackService creates the service.
func NewLectureFeedbackService(s lectureFeedbackStore) *LectureFeedbackService {
	return &LectureFeedbackService{store: s, now: time.Now}
}

// LectureFeedbackWindow is when a lecture's form is open: from 10 minutes before its end to 7 days after.
func LectureFeedbackWindow(l store.Lecture) (opens, closes time.Time) {
	return l.EndsAt.Add(-LectureFeedbackOpensBeforeEnd), l.EndsAt.Add(LectureFeedbackOpenFor)
}

// Submit validates and stores one student's feedback on one lecture.
func (s *LectureFeedbackService) Submit(ctx context.Context, userID int64, roles []string, sub LectureFeedbackSubmission) (*store.SentLectureFeedback, error) {
	if !lectureFeedbackStudent(roles) {
		return nil, ErrLectureFeedbackForbidden
	}
	in, problems := ValidateLectureFeedback(sub)
	if len(problems) > 0 {
		return nil, &LectureFeedbackInvalidError{Problems: problems}
	}
	in.StudentID = userID

	lecture, err := s.store.GetLecture(ctx, in.EventID)
	if errors.Is(err, store.ErrLectureNotFound) {
		return nil, ErrLectureFeedbackNotFound
	}
	if err != nil {
		return nil, err
	}
	if lecture.ModuleID != nil {
		enrolled, err := s.store.IsUserEnrolled(ctx, *lecture.ModuleID, userID)
		if err != nil {
			return nil, err
		}
		if !enrolled {
			return nil, ErrLectureFeedbackNotFound
		}
	}
	// "already sent" wins over the window: a re-send always answers 409
	if sent, err := s.store.HasLectureFeedback(ctx, lecture.ID, userID); err != nil {
		return nil, err
	} else if sent {
		return nil, ErrLectureFeedbackExists
	}
	opens, closes := LectureFeedbackWindow(*lecture)
	switch now := s.now(); {
	case now.Before(opens):
		return nil, fmt.Errorf("%w: it opens on %s", ErrLectureFeedbackNotOpen, bakuTime(opens))
	case !now.Before(closes):
		return nil, fmt.Errorf("%w: it closed on %s", ErrLectureFeedbackClosed, bakuTime(closes))
	}
	return s.store.CreateLectureFeedback(ctx, in)
}

// Sent returns the lectures the student has given feedback on.
func (s *LectureFeedbackService) Sent(ctx context.Context, userID int64, roles []string) ([]store.SentLectureFeedback, error) {
	if !lectureFeedbackStudent(roles) {
		return nil, ErrLectureFeedbackForbidden
	}
	return s.store.ListSentLectureFeedback(ctx, userID)
}

// Pending returns the lectures whose form is open for the student now and not yet sent: the "rate this lecture"
// section the front-end shows.
func (s *LectureFeedbackService) Pending(ctx context.Context, userID int64, roles []string) ([]store.Lecture, error) {
	if !lectureFeedbackStudent(roles) {
		return nil, ErrLectureFeedbackForbidden
	}
	return s.store.ListPendingLectures(ctx, userID, s.now(), LectureFeedbackOpensBeforeEnd, LectureFeedbackOpenFor)
}

// LectureFeedbackZone is the time zone the dates in this feature are read and written in (Asia/Baku).
func LectureFeedbackZone() *time.Location { return lectureFeedbackZone }

func bakuTime(t time.Time) string {
	return t.In(lectureFeedbackZone).Format("Monday 2 January 2006 at 15:04 (Baku time)")
}

// lectureFeedbackStudent: staff never fill the form, also when they hold the student role as well.
func lectureFeedbackStudent(roles []string) bool {
	return auth.HasRole(roles, "student") &&
		!auth.HasRole(roles, "mentor") && !auth.HasRole(roles, "lecturer") && !auth.HasRole(roles, "admin")
}

// ValidateLectureFeedback checks a form against the rules and returns it cleaned up (text trimmed, empty text
// dropped). Every broken rule is reported, in the form's order.
func ValidateLectureFeedback(sub LectureFeedbackSubmission) (store.LectureFeedbackInput, []LectureFeedbackProblem) {
	var in store.LectureFeedbackInput
	var problems []LectureFeedbackProblem
	add := func(field, format string, args ...any) {
		problems = append(problems, LectureFeedbackProblem{Field: field, Message: fmt.Sprintf(format, args...)})
	}

	switch {
	case sub.EventID == nil:
		add("event_id", "event_id is required")
	case *sub.EventID <= 0:
		add("event_id", "event_id must be a positive id")
	default:
		in.EventID = *sub.EventID
	}
	scale := func(field string, v *int, dst *int) {
		switch {
		case v == nil:
			add(field, "%s is required", field)
		case *v < 1 || *v > 5:
			add(field, "%s must be between 1 and 5", field)
		default:
			*dst = *v
		}
	}
	scale("overall", sub.Overall, &in.Overall)
	scale("clarity", sub.Clarity, &in.Clarity)
	scale("practice", sub.Practice, &in.Practice)
	switch {
	case sub.Pace == nil:
		add("pace", "pace is required")
	case !slices.Contains(LectureFeedbackPaces, strings.TrimSpace(*sub.Pace)):
		add("pace", "pace must be one of %s", strings.Join(LectureFeedbackPaces, ", "))
	default:
		in.Pace = strings.TrimSpace(*sub.Pace)
	}
	scale("confidence", sub.Confidence, &in.Confidence)
	text := func(field string, v *string) *string {
		if v == nil {
			return nil
		}
		t := strings.TrimSpace(*v)
		switch {
		case t == "":
			return nil
		case utf8.RuneCountInString(t) > LectureFeedbackMaxText:
			add(field, "%s must be at most %d characters", field, LectureFeedbackMaxText)
		case strings.IndexFunc(t, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0:
			add(field, "%s contains characters that are not allowed", field)
		default:
			return &t
		}
		return nil
	}
	in.Liked = text("liked", sub.Liked)
	in.Improve = text("improve", sub.Improve)
	return in, problems
}

// ---- reading (admin only) ----

// LectureFeedbackSummary is the numbers of one lecture.
type LectureFeedbackSummary struct {
	Lecture   store.Lecture
	Lecturers []store.LectureFeedbackPerson
	// Responses is how many students sent the form; Expected how many may send it (as enrolments are today).
	Responses int
	Expected  int
	// Averages has overall, clarity, practice and confidence, two decimals; nil without responses.
	Averages map[string]*float64
	Pace     map[string]int
	// WithComments is how many responses have an answer to at least one text question.
	WithComments int
}

// List returns the feedback with every answer and the lecturers of each lecture's week.
func (s *LectureFeedbackService) List(ctx context.Context, f store.LectureFeedbackFilter) ([]store.LectureFeedbackRow, map[int64][]store.LectureFeedbackPerson, error) {
	rows, err := s.store.ListLectureFeedback(ctx, f)
	if err != nil {
		return nil, nil, err
	}
	var modules []int64
	for _, r := range rows {
		if r.Lecture.ModuleID != nil {
			modules = append(modules, *r.Lecture.ModuleID)
		}
	}
	lecturers, err := s.store.ListWeekLecturers(ctx, uniqueIDs(modules))
	return rows, lecturers, err
}

// Summary returns per lecture (also lectures without feedback) the response count and the averages.
func (s *LectureFeedbackService) Summary(ctx context.Context, f store.LectureFeedbackFilter) ([]LectureFeedbackSummary, error) {
	lectures, err := s.store.ListLecturesWithAudience(ctx, f)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListLectureFeedback(ctx, f)
	if err != nil {
		return nil, err
	}
	var modules []int64
	for _, l := range lectures {
		if l.Lecture.ModuleID != nil {
			modules = append(modules, *l.Lecture.ModuleID)
		}
	}
	lecturers, err := s.store.ListWeekLecturers(ctx, uniqueIDs(modules))
	if err != nil {
		return nil, err
	}
	return SummarizeLectureFeedback(lectures, rows, lecturers), nil
}

// SummarizeLectureFeedback adds up the feedback per lecture, in the order of the lectures.
func SummarizeLectureFeedback(lectures []store.LectureWithAudience, rows []store.LectureFeedbackRow, lecturers map[int64][]store.LectureFeedbackPerson) []LectureFeedbackSummary {
	type sums struct{ overall, clarity, practice, confidence int }
	totals := map[int64]*sums{}
	out := make([]LectureFeedbackSummary, len(lectures))
	index := map[int64]int{}
	for i, l := range lectures {
		out[i] = LectureFeedbackSummary{Lecture: l.Lecture, Expected: l.Expected, Lecturers: []store.LectureFeedbackPerson{},
			Pace: map[string]int{}}
		for _, p := range LectureFeedbackPaces {
			out[i].Pace[p] = 0
		}
		if l.Lecture.ModuleID != nil && lecturers[*l.Lecture.ModuleID] != nil {
			out[i].Lecturers = lecturers[*l.Lecture.ModuleID]
		}
		index[l.Lecture.ID] = i
		totals[l.Lecture.ID] = &sums{}
	}
	for _, r := range rows {
		i, ok := index[r.Lecture.ID]
		if !ok {
			continue
		}
		s, t := &out[i], totals[r.Lecture.ID]
		s.Responses++
		s.Pace[r.Pace]++
		t.overall += r.Overall
		t.clarity += r.Clarity
		t.practice += r.Practice
		t.confidence += r.Confidence
		if r.Liked != nil || r.Improve != nil {
			s.WithComments++
		}
	}
	for i := range out {
		t, n := totals[out[i].Lecture.ID], out[i].Responses
		out[i].Averages = map[string]*float64{
			"overall": lectureAverage(t.overall, n), "clarity": lectureAverage(t.clarity, n),
			"practice": lectureAverage(t.practice, n), "confidence": lectureAverage(t.confidence, n),
		}
	}
	return out
}

func lectureAverage(sum, n int) *float64 {
	if n == 0 {
		return nil
	}
	v := math.Round(float64(sum)/float64(n)*100) / 100
	return &v
}

func uniqueIDs(ids []int64) []int64 {
	slices.Sort(ids)
	return slices.Compact(ids)
}
