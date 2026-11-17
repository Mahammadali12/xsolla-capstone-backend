package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xsolla-capstone-api/internal/store"
)

type summaryFakeStore struct {
	person     *store.AnalyticsPerson
	personErr  error
	student    []store.StudentFeedbackRecord
	mentor     []store.MentorReviewRecord
	historyErr error
	cache      map[string]store.SummaryCacheRow
	cacheErr   error
	saveErr    error
	saves      int

	lastStudentFrom time.Time
	lastStudentTo   time.Time
	lastRole        string
}

func (f *summaryFakeStore) GetAnalyticsPerson(_ context.Context, id int64, role string) (*store.AnalyticsPerson, error) {
	f.lastRole = role
	if f.personErr != nil {
		return nil, f.personErr
	}
	if f.person != nil {
		return f.person, nil
	}
	return &store.AnalyticsPerson{ID: id, Name: "Someone"}, nil
}

func (f *summaryFakeStore) StudentFeedbackHistory(_ context.Context, _ int64, from, upper time.Time) ([]store.StudentFeedbackRecord, error) {
	f.lastStudentFrom, f.lastStudentTo = from, upper
	return f.student, f.historyErr
}

func (f *summaryFakeStore) MentorReviewHistory(context.Context, int64, time.Time, time.Time) ([]store.MentorReviewRecord, error) {
	return f.mentor, f.historyErr
}

func (f *summaryFakeStore) GetSummaryCache(_ context.Context, key store.SummaryCacheKey) (*store.SummaryCacheRow, error) {
	if f.cacheErr != nil {
		return nil, f.cacheErr
	}
	row, ok := f.cache[summaryCacheKeyString(key)]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func (f *summaryFakeStore) SaveSummaryCache(_ context.Context, key store.SummaryCacheKey, row store.SummaryCacheRow) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	if f.cache == nil {
		f.cache = map[string]store.SummaryCacheRow{}
	}
	f.cache[summaryCacheKeyString(key)] = row
	f.saves++
	return nil
}

func summaryCacheKeyString(k store.SummaryCacheKey) string {
	return fmt.Sprintf("%s|%d|%s|%s|%s|%s", k.SubjectType, k.SubjectID, k.PeriodFrom, k.PeriodTo, k.Fingerprint, k.PromptVersion)
}

const fakeSummaryText = "Students repeatedly mention clear explanations and practical examples."

type fakeSummaryProvider struct {
	text    string
	err     error
	calls   int
	systems []string
	data    []string
}

func (f *fakeSummaryProvider) Summarize(_ context.Context, systemInstruction, data string) (SummaryOutput, error) {
	f.calls++
	f.systems = append(f.systems, systemInstruction)
	f.data = append(f.data, data)
	if f.err != nil {
		return SummaryOutput{}, f.err
	}
	text := f.text
	if text == "" {
		text = fakeSummaryText
	}
	return SummaryOutput{Text: text, Provider: "fake", Model: "fake-1"}, nil
}

func newSummaryTestService(st *summaryFakeStore, ai SummaryProvider) *AnalyticsSummaryService {
	svc := NewAnalyticsSummaryService(st, ai, 60000, 4000)
	svc.now = func() time.Time { return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) }
	return svc
}

func summaryDay(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 10, 0, 0, 0, time.UTC)
}

func studentFeedbackRows() []store.StudentFeedbackRecord {
	return []store.StudentFeedbackRecord{
		{Source: "week_grade", ModuleTitle: "Week 4", Feedback: "Steady progress; watch the deadline next week.",
			GradedAt: summaryDay(2026, 10, 20)},
		{Source: "assignment", ModuleTitle: "Week 4", Assignment: "REST API implementation",
			Feedback: "The implementation is clear, but error handling is incomplete.",
			GradedAt: summaryDay(2026, 10, 22)},
	}
}

func TestStudentSummaryUsesBothFeedbackSources(t *testing.T) {
	st := &summaryFakeStore{student: studentFeedbackRows()}
	ai := &fakeSummaryProvider{}
	svc := newSummaryTestService(st, ai)

	result, err := svc.Student(context.Background(), 10, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != fakeSummaryText || result.SourceCount != 2 || result.Cached || result.GeneratedAt == nil {
		t.Fatalf("result: %+v", result)
	}
	if st.lastRole != "student" {
		t.Errorf("role checked: %q", st.lastRole)
	}
	if ai.calls != 1 {
		t.Fatalf("provider calls: %d", ai.calls)
	}

	data := ai.data[0]
	for _, want := range []string{
		"<feedback_records>", `"source":"week_grade"`, `"source":"assignment"`,
		"Steady progress", "REST API implementation", `"feedback_date":"2026-10-22"`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("payload missing %q:\n%s", want, data)
		}
	}
	for _, forbidden := range []string{"Someone", "student_id", "graded_by", "@"} {
		if strings.Contains(data, forbidden) {
			t.Errorf("payload leaks %q:\n%s", forbidden, data)
		}
	}
	if !strings.Contains(ai.systems[0], "untrusted data") {
		t.Error("system instruction must treat feedback as untrusted data")
	}
}

func TestStudentSummaryEmptyDoesNotCallProvider(t *testing.T) {
	st := &summaryFakeStore{}
	ai := &fakeSummaryProvider{}
	svc := newSummaryTestService(st, ai)

	result, err := svc.Student(context.Background(), 10, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != StudentSummaryNoFeedback || result.SourceCount != 0 || result.GeneratedAt != nil || result.Cached {
		t.Fatalf("result: %+v", result)
	}
	if ai.calls != 0 {
		t.Fatalf("provider calls: %d", ai.calls)
	}
}

func TestStudentSummarySkipsBlankFeedback(t *testing.T) {
	rows := studentFeedbackRows()
	rows[1].Feedback = "   \n\t "
	st := &summaryFakeStore{student: rows}
	ai := &fakeSummaryProvider{}
	svc := newSummaryTestService(st, ai)

	result, err := svc.Student(context.Background(), 10, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceCount != 1 || ai.calls != 1 {
		t.Fatalf("source count %d, provider calls %d", result.SourceCount, ai.calls)
	}
}

func TestMentorSummaryAggregates(t *testing.T) {
	comment := "The explanations were clear and the code review was useful."
	reason := "not-scheduled"
	other := "CV review"
	clarity, engagement, timeRating := 5, 4, 3
	st := &summaryFakeStore{mentor: []store.MentorReviewRecord{
		{ModuleTitle: "Week 4", SubmittedAt: summaryDay(2026, 10, 22), HadSession: true,
			Topics: []string{"goals", "project"}, OtherTopic: &other,
			Clarity: &clarity, Engagement: &engagement, Time: &timeRating, Overall: 4, Comment: &comment},
		{ModuleTitle: "Week 5", SubmittedAt: summaryDay(2026, 11, 1), HadSession: false,
			NoSessionReason: &reason, Topics: []string{}, Overall: 2},
	}}
	ai := &fakeSummaryProvider{}
	svc := newSummaryTestService(st, ai)

	result, err := svc.Mentor(context.Background(), 7, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceCount != 2 || ai.calls != 1 {
		t.Fatalf("source count %d, provider calls %d", result.SourceCount, ai.calls)
	}
	if st.lastRole != "mentor" {
		t.Errorf("role checked: %q", st.lastRole)
	}

	data := ai.data[0]
	for _, want := range []string{
		`"clarity":{"count":1,"average":5}`,
		`"engagement":{"count":1,"average":4}`,
		`"comfort":{"count":0,"average":null}`,
		`"time_management":{"count":1,"average":3}`,
		`"overall":{"count":2,"average":3}`,
		`"sessions_reported":1`,
		`"no_sessions_reported":1`,
		`"not-scheduled":1`,
		`"topics":{"goals":1,"project":1}`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("payload missing %q:\n%s", want, data)
		}
	}
	if strings.Contains(data, "student_id") || strings.Contains(data, "Someone") {
		t.Errorf("payload leaks identifiers:\n%s", data)
	}
	if !strings.Contains(ai.systems[0], "had_session") || !strings.Contains(ai.systems[0], "students reported") {
		t.Error("mentor instruction must describe sessions as student-reported")
	}
}

func TestMentorSummaryEmptyDoesNotCallProvider(t *testing.T) {
	ai := &fakeSummaryProvider{}
	svc := newSummaryTestService(&summaryFakeStore{}, ai)

	result, err := svc.Mentor(context.Background(), 7, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != MentorSummaryNoFeedback || ai.calls != 0 {
		t.Fatalf("result %+v, provider calls %d", result, ai.calls)
	}
}

func TestSummaryCacheReuseAndForceRefresh(t *testing.T) {
	st := &summaryFakeStore{student: studentFeedbackRows()}
	ai := &fakeSummaryProvider{}
	svc := newSummaryTestService(st, ai)

	if _, err := svc.Student(context.Background(), 10, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	cached, err := svc.Student(context.Background(), 10, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !cached.Cached || cached.GeneratedAt == nil || ai.calls != 1 || st.saves != 1 {
		t.Fatalf("cache reuse: result %+v, calls %d, saves %d", cached, ai.calls, st.saves)
	}

	forced, err := svc.Student(context.Background(), 10, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if forced.Cached || ai.calls != 2 {
		t.Fatalf("force refresh: result %+v, calls %d", forced, ai.calls)
	}
}

func TestSummaryFingerprintChangesWhenFeedbackChanges(t *testing.T) {
	st := &summaryFakeStore{student: studentFeedbackRows()}
	ai := &fakeSummaryProvider{}
	svc := newSummaryTestService(st, ai)

	if _, err := svc.Student(context.Background(), 10, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	st.student[1].Feedback = "The implementation is clear and error handling is now complete."
	if _, err := svc.Student(context.Background(), 10, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if ai.calls != 2 {
		t.Fatalf("edited feedback must miss the cache, provider calls: %d", ai.calls)
	}
}

func TestSummaryPeriodIsPassedToTheStore(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, SchoolLocation)
	to := time.Date(2026, 11, 16, 0, 0, 0, 0, SchoolLocation)
	st := &summaryFakeStore{student: studentFeedbackRows()}
	svc := newSummaryTestService(st, &fakeSummaryProvider{})

	result, err := svc.Student(context.Background(), 10, &from, &to, false)
	if err != nil {
		t.Fatal(err)
	}
	if !st.lastStudentFrom.Equal(from) || !st.lastStudentTo.Equal(to.AddDate(0, 0, 1)) {
		t.Errorf("store window: [%v, %v)", st.lastStudentFrom, st.lastStudentTo)
	}
	if got := result.Period.From.Format("2006-01-02"); got != "2026-09-01" {
		t.Errorf("period from: %s", got)
	}
}

func TestSummaryErrors(t *testing.T) {
	t.Run("unknown student", func(t *testing.T) {
		st := &summaryFakeStore{personErr: store.ErrUserNotFound}
		ai := &fakeSummaryProvider{}
		svc := newSummaryTestService(st, ai)
		if _, err := svc.Student(context.Background(), 10, nil, nil, false); !errors.Is(err, store.ErrUserNotFound) {
			t.Fatalf("error: %v", err)
		}
		if ai.calls != 0 {
			t.Errorf("provider calls: %d", ai.calls)
		}
	})

	t.Run("provider unavailable", func(t *testing.T) {
		st := &summaryFakeStore{student: studentFeedbackRows()}
		ai := &fakeSummaryProvider{err: fmt.Errorf("%w: status 503", ErrSummaryProviderUnavailable)}
		svc := newSummaryTestService(st, ai)
		if _, err := svc.Student(context.Background(), 10, nil, nil, false); !errors.Is(err, ErrSummaryProviderUnavailable) {
			t.Fatalf("error: %v", err)
		}
		if st.saves != 0 {
			t.Errorf("a failed generation must not be cached, saves: %d", st.saves)
		}
	})

	t.Run("unusable output", func(t *testing.T) {
		for _, text := range []string{"<p>Hello</p>", `{"summary":"x"}`, "```\nhi\n```", "   "} {
			st := &summaryFakeStore{student: studentFeedbackRows()}
			ai := &fakeSummaryProvider{text: text}
			svc := newSummaryTestService(st, ai)
			if _, err := svc.Student(context.Background(), 10, nil, nil, false); !errors.Is(err, ErrSummaryOutputInvalid) {
				t.Errorf("text %q: %v", text, err)
			}
			if st.saves != 0 {
				t.Errorf("text %q was cached", text)
			}
		}
	})
}

func TestSummaryChunkingKeepsEveryRecord(t *testing.T) {
	rows := []store.StudentFeedbackRecord{}
	for i := 0; i < 5; i++ {
		rows = append(rows, store.StudentFeedbackRecord{
			Source: "assignment", ModuleTitle: fmt.Sprintf("Week %d", i+1), Assignment: "Task",
			Feedback: fmt.Sprintf("marker-%d %s", i, strings.Repeat("detailed feedback ", 20)),
			GradedAt: summaryDay(2026, 10, 20).AddDate(0, 0, i),
		})
	}
	st := &summaryFakeStore{student: rows}
	ai := &fakeSummaryProvider{}
	svc := NewAnalyticsSummaryService(st, ai, 2000, 4000)
	svc.now = func() time.Time { return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) }

	result, err := svc.Student(context.Background(), 10, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceCount != 5 || ai.calls < 3 {
		t.Fatalf("source count %d, provider calls %d", result.SourceCount, ai.calls)
	}
	if !strings.Contains(ai.data[ai.calls-1], `"total_records":5`) {
		t.Errorf("final call must combine all records:\n%s", ai.data[ai.calls-1])
	}

	all := strings.Join(ai.data, "\n")
	for i := 0; i < 5; i++ {
		if !strings.Contains(all, fmt.Sprintf("marker-%d", i)) {
			t.Errorf("record %d was dropped from the chunks", i)
		}
	}
}

type blockingSummaryProvider struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (p *blockingSummaryProvider) Summarize(context.Context, string, string) (SummaryOutput, error) {
	if p.calls.Add(1) == 1 {
		close(p.started)
		<-p.release
	}
	return SummaryOutput{Text: fakeSummaryText, Provider: "fake", Model: "fake-1"}, nil
}

func TestConcurrentIdenticalRequestsShareOneGeneration(t *testing.T) {
	st := &summaryFakeStore{student: studentFeedbackRows()}
	ai := &blockingSummaryProvider{started: make(chan struct{}), release: make(chan struct{})}
	svc := newSummaryTestService(st, ai)

	done := make(chan error, 1)
	go func() {
		_, err := svc.Student(context.Background(), 10, nil, nil, false)
		done <- err
	}()
	<-ai.started

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.Student(context.Background(), 10, nil, nil, false)
		}()
	}
	close(ai.release)
	wg.Wait()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := ai.calls.Load(); got != 1 {
		t.Fatalf("provider calls: %d, want 1", got)
	}
}
