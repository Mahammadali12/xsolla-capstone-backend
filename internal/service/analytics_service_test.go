package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"xsolla-capstone-api/internal/store"
)

type fakeAnalyticsStore struct {
	weeks    []store.ModuleStanding
	handIns  []store.AssignmentHandIns
	quizzes  []store.ModuleQuizAccuracy
	feedback store.MentorFeedback
	people   []store.AnalyticsPerson
	person   *store.AnalyticsPerson
	err      error

	lastMentorID *int64
	lastModuleID *int64
	lastRole     string
	lastQ        string
	lastLimit    int
	lastOffset   int
	lastFrom     time.Time
	lastTo       time.Time
	lastCutoff   time.Time
}

func (f *fakeAnalyticsStore) WeeklyStanding(_ context.Context, mentorID *int64, from, to time.Time) ([]store.ModuleStanding, error) {
	f.lastMentorID, f.lastFrom, f.lastTo = mentorID, from, to
	return f.weeks, f.err
}

func (f *fakeAnalyticsStore) StudentAssignmentHandIns(_ context.Context, _ int64, moduleID *int64) ([]store.AssignmentHandIns, error) {
	f.lastModuleID = moduleID
	return f.handIns, f.err
}

func (f *fakeAnalyticsStore) ListBookedSlots(context.Context, int64, time.Time, time.Time) ([]store.BookedSlot, error) {
	return nil, f.err
}

func (f *fakeAnalyticsStore) StudentQuizAccuracy(_ context.Context, _ int64, _, _ time.Time, moduleID *int64) ([]store.ModuleQuizAccuracy, error) {
	f.lastModuleID = moduleID
	return f.quizzes, f.err
}

func (f *fakeAnalyticsStore) MentorFeedback(_ context.Context, _ int64, from, to time.Time) (store.MentorFeedback, error) {
	f.lastFrom, f.lastTo = from, to
	return f.feedback, f.err
}

func (f *fakeAnalyticsStore) SearchAnalyticsPeople(_ context.Context, role, q string, limit, offset int) ([]store.AnalyticsPerson, error) {
	f.lastRole, f.lastQ, f.lastLimit, f.lastOffset = role, q, limit, offset
	return f.people, f.err
}

func (f *fakeAnalyticsStore) GetAnalyticsPerson(_ context.Context, _ int64, role string) (*store.AnalyticsPerson, error) {
	f.lastRole = role
	return f.person, f.err
}

func (f *fakeAnalyticsStore) GetModuleByID(context.Context, int64) (*store.Module, error) {
	return &store.Module{}, f.err
}

// fixed analytics clock: 18 November 2026, 16:00 in Baku.
func analyticsNow() time.Time {
	return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
}

func TestAnalyticsResolvePeriod(t *testing.T) {
	svc := NewAnalyticsService(&fakeAnalyticsStore{})
	svc.now = analyticsNow

	t.Run("defaults to the last 12 weeks ending today", func(t *testing.T) {
		p, err := svc.ResolvePeriod(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := p.To.Format("2006-01-02"), "2026-11-18"; got != want {
			t.Errorf("to = %s, want %s", got, want)
		}
		if got, want := p.From.Format("2006-01-02"), "2026-08-26"; got != want {
			t.Errorf("from = %s, want %s", got, want)
		}
	})

	t.Run("uses the given dates", func(t *testing.T) {
		from := time.Date(2026, 9, 1, 0, 0, 0, 0, SchoolLocation)
		to := time.Date(2026, 11, 14, 0, 0, 0, 0, SchoolLocation)
		p, err := svc.ResolvePeriod(&from, &to)
		if err != nil {
			t.Fatal(err)
		}
		if !p.From.Equal(from) || !p.To.Equal(to) {
			t.Errorf("period = %v..%v, want %v..%v", p.From, p.To, from, to)
		}
		if !p.Upper().Equal(to.AddDate(0, 0, 1)) {
			t.Errorf("upper = %v, want the next midnight", p.Upper())
		}
	})

	from := time.Date(2026, 11, 15, 0, 0, 0, 0, SchoolLocation)
	to := time.Date(2026, 11, 14, 0, 0, 0, 0, SchoolLocation)
	if _, err := svc.ResolvePeriod(&from, &to); !errors.Is(err, ErrAnalyticsPeriodInvalid) {
		t.Errorf("from after to: %v, want ErrAnalyticsPeriodInvalid", err)
	}

	from = time.Date(2025, 1, 1, 0, 0, 0, 0, SchoolLocation)
	if _, err := svc.ResolvePeriod(&from, &to); !errors.Is(err, ErrAnalyticsPeriodTooLong) {
		t.Errorf("367 days: %v, want ErrAnalyticsPeriodTooLong", err)
	}
}

func TestAnalyticsStudentReport(t *testing.T) {
	fs := &fakeAnalyticsStore{
		person: &store.AnalyticsPerson{ID: 7, Name: "Student One"},
		// a week that opened on Monday 19 October: no 1:1 booked, so the work is due on Monday 26 October at 08:00
		handIns: func() []store.AssignmentHandIns {
			opens := time.Date(2026, 10, 19, 0, 0, 0, 0, SchoolLocation)
			item := func(id int64, at ...time.Time) store.AssignmentHandIns {
				return store.AssignmentHandIns{ContentItemID: id, ModuleID: 1, ModuleOpensAt: opens, HandedInAt: at}
			}
			onTime := time.Date(2026, 10, 25, 20, 0, 0, 0, SchoolLocation)
			late := time.Date(2026, 10, 26, 10, 0, 0, 0, SchoolLocation)
			// a change on Wednesday, after the weekend, does not make on-time work late
			after := time.Date(2026, 10, 28, 10, 0, 0, 0, SchoolLocation)
			return []store.AssignmentHandIns{
				item(1, onTime), item(2, onTime), item(3, onTime, after), item(4, onTime), item(5, late), item(6), item(7),
			}
		}(),
		quizzes: []store.ModuleQuizAccuracy{
			{ModuleID: 1, ModuleTitle: "Week 1", ScoreTotal: 16, PossibleTotal: 20, AttemptCount: 2},
			{ModuleID: 2, ModuleTitle: "Week 2", ScoreTotal: 0, PossibleTotal: 0, AttemptCount: 1},
		},
	}
	svc := NewAnalyticsService(fs)
	svc.now = analyticsNow

	to := time.Date(2026, 11, 14, 0, 0, 0, 0, SchoolLocation)
	report, err := svc.Student(context.Background(), 7, nil, &to, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Student.Name != "Student One" || report.Status != (store.AssignmentStatus{OnTime: 4, Late: 1, Missing: 2, Eligible: 7}) {
		t.Errorf("report = %+v", report)
	}
	if got := report.Quizzes[0].Accuracy; got == nil || *got != 80 {
		t.Errorf("accuracy = %v, want 80", got)
	}
	if got := report.Quizzes[1].Accuracy; got != nil {
		t.Errorf("zero-total accuracy = %v, want null", got)
	}
	// The period ends in the past, so only deadlines before its end count: the Monday 26 October 08:00 deadline is
	// outside a period that ends on Sunday 25 October.
	early := time.Date(2026, 10, 25, 0, 0, 0, 0, SchoolLocation)
	report, err = svc.Student(context.Background(), 7, nil, &early, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status.Eligible != 0 {
		t.Errorf("assignments due after the period counted: %+v", report.Status)
	}
}

func TestAnalyticsStudentCutoffIsNowForFuturePeriods(t *testing.T) {
	// a week that opens in two weeks has its deadline in the future: it does not count yet
	future := store.AssignmentHandIns{ContentItemID: 1, ModuleID: 1, ModuleOpensAt: analyticsNow().AddDate(0, 0, 14)}
	fs := &fakeAnalyticsStore{person: &store.AnalyticsPerson{ID: 7, Name: "Student One"}, handIns: []store.AssignmentHandIns{future}}
	svc := NewAnalyticsService(fs)
	svc.now = analyticsNow

	report, err := svc.Student(context.Background(), 7, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Default to is today; midnight after it is tomorrow, so the cutoff is now.
	if report.Status.Eligible != 0 {
		t.Errorf("a deadline in the future counted: %+v", report.Status)
	}
}

func TestAnalyticsStudentModuleIsValidated(t *testing.T) {
	fs := &fakeAnalyticsStore{person: &store.AnalyticsPerson{ID: 7, Name: "Student One"}, err: store.ErrModuleNotFound}
	svc := NewAnalyticsService(fs)

	moduleID := int64(99)
	if _, err := svc.Student(context.Background(), 7, nil, nil, &moduleID); !errors.Is(err, store.ErrModuleNotFound) {
		t.Errorf("unknown module: %v, want ErrModuleNotFound", err)
	}
}

func TestAnalyticsMentorReport(t *testing.T) {
	clarity, engagement := 4.444, 3.0
	fs := &fakeAnalyticsStore{
		person: &store.AnalyticsPerson{ID: 3, Name: "Mentor One"},
		feedback: store.MentorFeedback{
			Clarity:    store.DimensionAverage{Average: &clarity, Count: 5},
			Engagement: store.DimensionAverage{Average: &engagement, Count: 2},
			Comfort:    store.DimensionAverage{},
			Time:       store.DimensionAverage{Average: &engagement, Count: 2},
		},
		weeks: []store.ModuleStanding{{ModuleID: 1, ModuleTitle: "Week 1", Green: 3, Yellow: 1, Red: 1, Total: 5}},
	}
	svc := NewAnalyticsService(fs)
	svc.now = analyticsNow

	report, err := svc.Mentor(context.Background(), 3, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Attribution != StandingAttributionCurrentRoster {
		t.Errorf("attribution = %q", report.Attribution)
	}
	wantDimensions := []string{"clarity", "engagement", "comfort", "time"}
	if len(report.Feedback) != len(wantDimensions) {
		t.Fatalf("feedback has %d dimensions, want 4", len(report.Feedback))
	}
	for i, want := range wantDimensions {
		if report.Feedback[i].Dimension != want {
			t.Errorf("dimension %d = %q, want %q", i, report.Feedback[i].Dimension, want)
		}
	}
	if got := report.Feedback[0].Average; got == nil || *got != 4.44 {
		t.Errorf("clarity = %v, want 4.44", got)
	}
	if report.Feedback[2].Average != nil || report.Feedback[2].Count != 0 {
		t.Errorf("comfort = %+v, want null with zero count", report.Feedback[2])
	}
	if fs.lastMentorID == nil || *fs.lastMentorID != 3 {
		t.Errorf("weekly standing did not filter by the mentor: %v", fs.lastMentorID)
	}
}

func TestAnalyticsOverviewAndSearch(t *testing.T) {
	fs := &fakeAnalyticsStore{
		weeks:  []store.ModuleStanding{{ModuleID: 1, ModuleTitle: "Week 1"}},
		people: []store.AnalyticsPerson{{ID: 7, Name: "Student One", Email: "s1@example.com"}},
	}
	svc := NewAnalyticsService(fs)
	svc.now = analyticsNow

	report, err := svc.Overview(context.Background(), nil, nil)
	if err != nil || len(report.Weeks) != 1 {
		t.Fatalf("overview: %+v, %v", report, err)
	}
	if fs.lastMentorID != nil {
		t.Errorf("overview must not filter by mentor")
	}

	if _, err := svc.SearchStudents(context.Background(), "  One ", 10, 5); err != nil {
		t.Fatal(err)
	}
	if fs.lastRole != "student" || fs.lastQ != "One" || fs.lastLimit != 10 || fs.lastOffset != 5 {
		t.Errorf("student search = %q %q %d %d", fs.lastRole, fs.lastQ, fs.lastLimit, fs.lastOffset)
	}
	if _, err := svc.SearchMentors(context.Background(), "", 20, 0); err != nil {
		t.Fatal(err)
	}
	if fs.lastRole != "mentor" {
		t.Errorf("mentor search role = %q", fs.lastRole)
	}
}

func TestAnalyticsPercentRounding(t *testing.T) {
	if got := analyticsPercent(1, 3); got == nil || *got != 33.33 {
		t.Errorf("1/3 = %v, want 33.33", got)
	}
	if got := analyticsPercent(0, 10); got == nil || *got != 0 {
		t.Errorf("0/10 = %v, want 0", got)
	}
	if got := analyticsPercent(5, 0); got != nil {
		t.Errorf("5/0 = %v, want null", got)
	}
}
