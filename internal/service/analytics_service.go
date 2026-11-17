package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"xsolla-capstone-api/internal/store"
)

const (
	// AnalyticsDefaultWeeks is how far back the reporting period starts when from is not sent.
	AnalyticsDefaultWeeks = 12
	// AnalyticsMaxDays is the longest reporting period the API accepts.
	AnalyticsMaxDays = 366
	// StandingAttributionCurrentRoster: weeks are attributed with today's team membership, not the mentor of that
	// week (the schema keeps no mentor assignment history).
	StandingAttributionCurrentRoster = "current_roster"
)

var (
	// ErrAnalyticsPeriodInvalid is returned when from is after to.
	ErrAnalyticsPeriodInvalid = errors.New("from must not be after to")
	// ErrAnalyticsPeriodTooLong is returned when the period exceeds AnalyticsMaxDays.
	ErrAnalyticsPeriodTooLong = errors.New("the reporting period is too long")
)

// AnalyticsPeriod is an inclusive range of school dates.
type AnalyticsPeriod struct {
	From time.Time
	To   time.Time
}

// Upper is the exclusive timestamp bound: midnight after To, in school time.
func (p AnalyticsPeriod) Upper() time.Time {
	return p.To.AddDate(0, 0, 1)
}

// OverviewReport is the school-wide weekly standing chart.
type OverviewReport struct {
	Period      AnalyticsPeriod
	GeneratedAt time.Time
	Weeks       []store.ModuleStanding
}

// QuizAccuracy is one module's weighted accuracy for a student.
type QuizAccuracy struct {
	ModuleID      int64
	ModuleTitle   string
	OpensAt       time.Time
	ScoreTotal    int
	PossibleTotal int
	Accuracy      *float64
	AttemptCount  int
}

// StudentReport is one student's assignment and quiz analytics.
type StudentReport struct {
	Student     store.AnalyticsPerson
	Period      AnalyticsPeriod
	GeneratedAt time.Time
	Status      store.AssignmentStatus
	Quizzes     []QuizAccuracy
}

// FeedbackDimension is one rated dimension's rounded average and response count.
type FeedbackDimension struct {
	Dimension string
	Average   *float64
	Count     int
}

// MentorReport is one mentor's feedback averages and weekly standing chart.
type MentorReport struct {
	Mentor      store.AnalyticsPerson
	Period      AnalyticsPeriod
	GeneratedAt time.Time
	Feedback    []FeedbackDimension
	Weeks       []store.ModuleStanding
	Attribution string
}

// analyticsStore is the part of the store the analytics service needs.
type analyticsStore interface {
	WeeklyStanding(ctx context.Context, mentorID *int64, from, to time.Time) ([]store.ModuleStanding, error)
	StudentAssignmentStatus(ctx context.Context, studentID int64, from, cutoff time.Time, moduleID *int64) (store.AssignmentStatus, error)
	StudentQuizAccuracy(ctx context.Context, studentID int64, from, to time.Time, moduleID *int64) ([]store.ModuleQuizAccuracy, error)
	MentorFeedback(ctx context.Context, mentorID int64, from, to time.Time) (store.MentorFeedback, error)
	SearchAnalyticsPeople(ctx context.Context, role, q string, limit, offset int) ([]store.AnalyticsPerson, error)
	GetAnalyticsPerson(ctx context.Context, id int64, role string) (*store.AnalyticsPerson, error)
	GetModuleByID(ctx context.Context, id int64) (*store.Module, error)
}

// AnalyticsService is the read-only reporting layer.
type AnalyticsService struct {
	store analyticsStore
	now   func() time.Time
}

// NewAnalyticsService creates the service.
func NewAnalyticsService(s analyticsStore) *AnalyticsService {
	return &AnalyticsService{store: s, now: time.Now}
}

// ResolvePeriod applies the default period and validates the bounds. Dates are school dates.
func (s *AnalyticsService) ResolvePeriod(from, to *time.Time) (AnalyticsPeriod, error) {
	return resolveAnalyticsPeriod(s.now(), from, to)
}

// resolveAnalyticsPeriod is shared with the AI summary endpoints so both describe exactly the same window.
func resolveAnalyticsPeriod(now time.Time, from, to *time.Time) (AnalyticsPeriod, error) {
	toDay := schoolDate(now)
	if to != nil {
		toDay = schoolDate(*to)
	}
	fromDay := toDay.AddDate(0, 0, -7*AnalyticsDefaultWeeks)
	if from != nil {
		fromDay = schoolDate(*from)
	}
	if fromDay.After(toDay) {
		return AnalyticsPeriod{}, ErrAnalyticsPeriodInvalid
	}
	if toDay.AddDate(0, 0, -AnalyticsMaxDays).After(fromDay) {
		return AnalyticsPeriod{}, ErrAnalyticsPeriodTooLong
	}
	return AnalyticsPeriod{From: fromDay, To: toDay}, nil
}

// Overview returns the weekly standing distribution for every module in the period.
func (s *AnalyticsService) Overview(ctx context.Context, from, to *time.Time) (*OverviewReport, error) {
	p, err := s.ResolvePeriod(from, to)
	if err != nil {
		return nil, err
	}
	weeks, err := s.store.WeeklyStanding(ctx, nil, p.From, p.Upper())
	if err != nil {
		return nil, err
	}
	return &OverviewReport{Period: p, GeneratedAt: s.now().UTC(), Weeks: weeks}, nil
}

// Student returns one student's assignment status and quiz accuracy. moduleID optionally narrows both.
func (s *AnalyticsService) Student(ctx context.Context, studentID int64, from, to *time.Time, moduleID *int64) (*StudentReport, error) {
	p, err := s.ResolvePeriod(from, to)
	if err != nil {
		return nil, err
	}
	if moduleID != nil {
		if _, err := s.store.GetModuleByID(ctx, *moduleID); err != nil {
			return nil, err
		}
	}
	person, err := s.store.GetAnalyticsPerson(ctx, studentID, "student")
	if err != nil {
		return nil, err
	}

	// A deadline only counts once it has passed: cap the period at now.
	cutoff := p.Upper()
	if now := s.now(); now.Before(cutoff) {
		cutoff = now
	}
	status, err := s.store.StudentAssignmentStatus(ctx, studentID, p.From, cutoff, moduleID)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.StudentQuizAccuracy(ctx, studentID, p.From, p.Upper(), moduleID)
	if err != nil {
		return nil, err
	}
	quizzes := make([]QuizAccuracy, len(rows))
	for i, r := range rows {
		quizzes[i] = QuizAccuracy{
			ModuleID: r.ModuleID, ModuleTitle: r.ModuleTitle, OpensAt: r.OpensAt,
			ScoreTotal: r.ScoreTotal, PossibleTotal: r.PossibleTotal,
			Accuracy: analyticsPercent(r.ScoreTotal, r.PossibleTotal), AttemptCount: r.AttemptCount,
		}
	}
	return &StudentReport{Student: *person, Period: p, GeneratedAt: s.now().UTC(), Status: status, Quizzes: quizzes}, nil
}

// Mentor returns one mentor's feedback averages and their current students' standing per week.
func (s *AnalyticsService) Mentor(ctx context.Context, mentorID int64, from, to *time.Time) (*MentorReport, error) {
	p, err := s.ResolvePeriod(from, to)
	if err != nil {
		return nil, err
	}
	person, err := s.store.GetAnalyticsPerson(ctx, mentorID, "mentor")
	if err != nil {
		return nil, err
	}
	f, err := s.store.MentorFeedback(ctx, mentorID, p.From, p.Upper())
	if err != nil {
		return nil, err
	}
	weeks, err := s.store.WeeklyStanding(ctx, &mentorID, p.From, p.Upper())
	if err != nil {
		return nil, err
	}
	return &MentorReport{
		Mentor: *person, Period: p, GeneratedAt: s.now().UTC(),
		Feedback: []FeedbackDimension{
			{Dimension: "clarity", Average: roundAverage(f.Clarity.Average), Count: f.Clarity.Count},
			{Dimension: "engagement", Average: roundAverage(f.Engagement.Average), Count: f.Engagement.Count},
			{Dimension: "comfort", Average: roundAverage(f.Comfort.Average), Count: f.Comfort.Count},
			{Dimension: "time", Average: roundAverage(f.Time.Average), Count: f.Time.Count},
		},
		Weeks:       weeks,
		Attribution: StandingAttributionCurrentRoster,
	}, nil
}

// SearchStudents returns active students matching q.
func (s *AnalyticsService) SearchStudents(ctx context.Context, q string, limit, offset int) ([]store.AnalyticsPerson, error) {
	return s.store.SearchAnalyticsPeople(ctx, "student", strings.TrimSpace(q), limit, offset)
}

// SearchMentors returns active mentors matching q.
func (s *AnalyticsService) SearchMentors(ctx context.Context, q string, limit, offset int) ([]store.AnalyticsPerson, error) {
	return s.store.SearchAnalyticsPeople(ctx, "mentor", strings.TrimSpace(q), limit, offset)
}

func schoolDate(t time.Time) time.Time {
	l := t.In(SchoolLocation)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, SchoolLocation)
}

func analyticsPercent(score, possible int) *float64 {
	if possible == 0 {
		return nil
	}
	v := float64(score) / float64(possible) * 100
	return roundAverage(&v)
}

func roundAverage(v *float64) *float64 {
	if v == nil {
		return nil
	}
	r := math.Round(*v*100) / 100
	return &r
}
