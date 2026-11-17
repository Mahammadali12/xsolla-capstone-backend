package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"xsolla-capstone-api/internal/store"
)

func ptr[T any](v T) *T { return &v }

// withSession is a valid review of a week with a 1:1 session.
func withSession() MentorReviewSubmission {
	return MentorReviewSubmission{
		ModuleID:   ptr(int64(4)),
		HadSession: ptr(true),
		Topics:     []string{"goals", "other"},
		OtherTopic: ptr("  CV review "),
		Ratings:    map[string]int{"clarity": 5, "engagement": 4, "comfort": 5, "time": 3},
		Overall:    ptr(4),
		Comment:    ptr(" More code reviews please "),
	}
}

// withoutSession is a valid review of a week without a session.
func withoutSession() MentorReviewSubmission {
	return MentorReviewSubmission{
		ModuleID:        ptr(int64(4)),
		HadSession:      ptr(false),
		NoSessionReason: ptr("not-scheduled"),
		Overall:         ptr(2),
	}
}

func fields(errs []FieldError) []string {
	out := []string{}
	for _, e := range errs {
		out = append(out, e.Field)
	}
	return out
}

func TestValidateMentorReviewAcceptsBothShapes(t *testing.T) {
	in, errs := ValidateMentorReview(withSession())
	if len(errs) > 0 {
		t.Fatalf("with session: unexpected errors %v", errs)
	}
	want := store.MentorReviewInput{
		ModuleID:   4,
		HadSession: true,
		Topics:     []string{"goals", "other"},
		OtherTopic: ptr("CV review"),
		Ratings:    &store.MentorReviewRatings{Clarity: 5, Engagement: 4, Comfort: 5, Time: 3},
		Overall:    4,
		Comment:    ptr("More code reviews please"),
	}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("with session:\n got %+v\nwant %+v", in, want)
	}

	in, errs = ValidateMentorReview(withoutSession())
	if len(errs) > 0 {
		t.Fatalf("without session: unexpected errors %v", errs)
	}
	want = store.MentorReviewInput{ModuleID: 4, NoSessionReason: ptr("not-scheduled"), Overall: 2}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("without session:\n got %+v\nwant %+v", in, want)
	}
}

func TestValidateMentorReviewRules(t *testing.T) {
	long := func(n int) *string { return ptr(strings.Repeat("ə", n)) } // 2 bytes per character: limits count characters

	cases := []struct {
		name  string
		edit  func(*MentorReviewSubmission)
		base  func() MentorReviewSubmission
		wantF []string // fields with errors, in order; empty = valid
	}{
		{"empty body", func(s *MentorReviewSubmission) { *s = MentorReviewSubmission{} }, withSession, []string{"module_id", "had_session", "overall"}},
		{"module_id zero", func(s *MentorReviewSubmission) { s.ModuleID = ptr(int64(0)) }, withSession, []string{"module_id"}},
		{"module_id negative", func(s *MentorReviewSubmission) { s.ModuleID = ptr(int64(-3)) }, withSession, []string{"module_id"}},
		{"overall 0", func(s *MentorReviewSubmission) { s.Overall = ptr(0) }, withSession, []string{"overall"}},
		{"overall 6", func(s *MentorReviewSubmission) { s.Overall = ptr(6) }, withoutSession, []string{"overall"}},
		{"overall 1 and 5 are fine", func(s *MentorReviewSubmission) { s.Overall = ptr(1) }, withSession, nil},
		{"comment only spaces is dropped", func(s *MentorReviewSubmission) { s.Comment = ptr("   ") }, withSession, nil},
		{"comment 1000 characters", func(s *MentorReviewSubmission) { s.Comment = long(1000) }, withSession, nil},
		{"comment 1001 characters", func(s *MentorReviewSubmission) { s.Comment = long(1001) }, withoutSession, []string{"comment"}},

		// with a session
		{"session: reason given", func(s *MentorReviewSubmission) { s.NoSessionReason = ptr("other") }, withSession, []string{"no_session_reason"}},
		{"session: empty reason is ignored", func(s *MentorReviewSubmission) { s.NoSessionReason = ptr(" ") }, withSession, nil},
		{"session: no topics", func(s *MentorReviewSubmission) { s.Topics = nil; s.OtherTopic = nil }, withSession, []string{"topics"}},
		{"session: empty topics", func(s *MentorReviewSubmission) { s.Topics = []string{}; s.OtherTopic = nil }, withSession, []string{"topics"}},
		{"session: unknown topic", func(s *MentorReviewSubmission) { s.Topics = []string{"goals", "gossip"}; s.OtherTopic = nil }, withSession, []string{"topics"}},
		{"session: topic twice", func(s *MentorReviewSubmission) { s.Topics = []string{"goals", "goals"}; s.OtherTopic = nil }, withSession, []string{"topics"}},
		{"session: other without text", func(s *MentorReviewSubmission) { s.OtherTopic = nil }, withSession, []string{"other_topic"}},
		{"session: other with blank text", func(s *MentorReviewSubmission) { s.OtherTopic = ptr("  ") }, withSession, []string{"other_topic"}},
		{"session: other text 120 characters", func(s *MentorReviewSubmission) { s.OtherTopic = long(120) }, withSession, nil},
		{"session: other text 121 characters", func(s *MentorReviewSubmission) { s.OtherTopic = long(121) }, withSession, []string{"other_topic"}},
		{"session: text without other", func(s *MentorReviewSubmission) { s.Topics = []string{"career"} }, withSession, []string{"other_topic"}},
		{"session: no ratings", func(s *MentorReviewSubmission) { s.Ratings = nil }, withSession, []string{"ratings"}},
		{"session: empty ratings", func(s *MentorReviewSubmission) { s.Ratings = map[string]int{} }, withSession, []string{"ratings"}},
		{"session: rating missing", func(s *MentorReviewSubmission) { delete(s.Ratings, "time") }, withSession, []string{"ratings.time"}},
		{"session: rating 0 and 6", func(s *MentorReviewSubmission) { s.Ratings["clarity"] = 0; s.Ratings["comfort"] = 6 }, withSession, []string{"ratings.clarity", "ratings.comfort"}},
		{"session: unknown rating", func(s *MentorReviewSubmission) { s.Ratings["speed"] = 3; s.Ratings["accuracy"] = 1 }, withSession, []string{"ratings.accuracy", "ratings.speed"}},

		// without a session
		{"no session: no reason", func(s *MentorReviewSubmission) { s.NoSessionReason = nil }, withoutSession, []string{"no_session_reason"}},
		{"no session: unknown reason", func(s *MentorReviewSubmission) { s.NoSessionReason = ptr("sick") }, withoutSession, []string{"no_session_reason"}},
		{"no session: every reason works", func(s *MentorReviewSubmission) { s.NoSessionReason = ptr("mentor-unavailable") }, withoutSession, nil},
		{"no session: topics given", func(s *MentorReviewSubmission) { s.Topics = []string{"goals"} }, withoutSession, []string{"topics"}},
		{"no session: other text given", func(s *MentorReviewSubmission) { s.OtherTopic = ptr("CV") }, withoutSession, []string{"other_topic"}},
		{"no session: ratings given", func(s *MentorReviewSubmission) { s.Ratings = map[string]int{"clarity": 3} }, withoutSession, []string{"ratings"}},
		{"no session: empty extras are ignored", func(s *MentorReviewSubmission) {
			s.Topics, s.Ratings, s.OtherTopic = []string{}, map[string]int{}, ptr("")
		}, withoutSession, nil},
		{"no session: everything wrong at once", func(s *MentorReviewSubmission) {
			s.NoSessionReason, s.Topics, s.Ratings, s.Overall = nil, []string{"goals"}, map[string]int{"time": 2}, ptr(9)
		}, withoutSession, []string{"no_session_reason", "topics", "ratings", "overall"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := tc.base()
			tc.edit(&sub)
			_, errs := ValidateMentorReview(sub)
			want := tc.wantF
			if want == nil {
				want = []string{}
			}
			if got := fields(errs); !reflect.DeepEqual(got, want) {
				t.Errorf("fields with errors = %v, want %v (%v)", got, want, errs)
			}
		})
	}
}

func TestMentorReviewOpensAt(t *testing.T) {
	baku := func(day, hour, minute int) time.Time {
		return time.Date(2026, 11, day, hour, minute, 0, 0, SchoolLocation)
	}
	satMidnight := baku(14, 0, 0) // Saturday 14 November, 00:00 in Baku = Friday 20:00 UTC
	cases := []struct {
		name          string
		opens, closes time.Time
		want          time.Time
	}{
		{"closes Monday 23:59 (the week in the bug report)", baku(10, 0, 0), baku(16, 23, 59), satMidnight},
		{"closes Monday 00:00", baku(9, 0, 0), baku(16, 0, 0), satMidnight},
		{"closes Sunday 23:59", baku(9, 0, 0), baku(15, 23, 59), satMidnight},
		{"closes Saturday 18:00", baku(9, 0, 0), baku(14, 18, 0), satMidnight},
		{"closes Wednesday noon: the weekend before", baku(9, 0, 0), baku(18, 12, 0), satMidnight},
		{"closing time given in UTC", baku(9, 0, 0), time.Date(2026, 11, 16, 19, 59, 0, 0, time.UTC), satMidnight},
		{"week opens on Sunday: never before it opens", baku(15, 9, 0), baku(16, 23, 59), baku(15, 9, 0)},
	}
	for _, tc := range cases {
		if got := MentorReviewOpensAt(tc.opens, tc.closes); !got.Equal(tc.want) {
			t.Errorf("%s: opens at %v, want %v", tc.name, got.In(SchoolLocation), tc.want)
		}
	}
	if _, offset := satMidnight.Zone(); offset != 4*60*60 {
		t.Errorf("school time zone offset %d, want UTC+4", offset)
	}
}

// fakeReviewStore is an in-memory mentorReviewStore.
type fakeReviewStore struct {
	module      *store.Module
	module2     *store.Module
	expected    []store.ExpectedReviewer
	gotFilter   store.MentorReviewFilter
	moduleErr   error
	enrolled    bool
	exists      bool
	createErr   error
	reviewed    []store.ReviewedModule
	created     []store.MentorReviewInput
	storeCalled bool
}

func (f *fakeReviewStore) GetModuleByID(_ context.Context, id int64) (*store.Module, error) {
	f.storeCalled = true
	if f.moduleErr != nil {
		return nil, f.moduleErr
	}
	if f.module2 != nil && f.module2.ID == id {
		return f.module2, nil
	}
	if f.module == nil || f.module.ID != id {
		return nil, store.ErrModuleNotFound
	}
	return f.module, nil
}
func (f *fakeReviewStore) ListMentorReviews(_ context.Context, filter store.MentorReviewFilter) ([]store.MentorReviewRow, error) {
	f.gotFilter = filter
	return []store.MentorReviewRow{}, nil
}
func (f *fakeReviewStore) ListExpectedReviewers(context.Context, *int64, *time.Time, *time.Time) ([]store.ExpectedReviewer, error) {
	return f.expected, nil
}
func (f *fakeReviewStore) IsUserEnrolled(context.Context, int64, int64) (bool, error) {
	return f.enrolled, nil
}
func (f *fakeReviewStore) HasMentorReview(context.Context, int64, int64) (bool, error) {
	return f.exists, nil
}
func (f *fakeReviewStore) CreateMentorReview(_ context.Context, in store.MentorReviewInput) (*store.MentorReview, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, in)
	return &store.MentorReview{ID: 1, ModuleID: in.ModuleID, StudentID: in.StudentID, MentorID: 77, SubmittedAt: time.Now()}, nil
}
func (f *fakeReviewStore) ListReviewedModules(context.Context, int64) ([]store.ReviewedModule, error) {
	f.storeCalled = true
	return f.reviewed, nil
}

var (
	// a week like the real ones: Monday 00:00 to the next Monday 23:59, Baku time
	weekOpens  = time.Date(2026, 11, 10, 0, 0, 0, 0, SchoolLocation)
	weekCloses = time.Date(2026, 11, 16, 23, 59, 0, 0, SchoolLocation)
	saturday   = time.Date(2026, 11, 14, 0, 0, 0, 0, SchoolLocation)
)

func newReviewService(f *fakeReviewStore, now time.Time) *MentorReviewService {
	s := NewMentorReviewService(f)
	s.now = func() time.Time { return now }
	return s
}

func openWeek() *fakeReviewStore {
	return &fakeReviewStore{module: &store.Module{ID: 4, OpensAt: weekOpens, ClosesAt: weekCloses}, enrolled: true}
}

func TestSubmitMentorReviewRoles(t *testing.T) {
	cases := []struct {
		roles   []string
		allowed bool
	}{
		{[]string{"student"}, true},
		{nil, false},
		{[]string{"mentor"}, false},
		{[]string{"lecturer"}, false},
		{[]string{"admin"}, false},
		{[]string{"student", "mentor"}, false},
		{[]string{"lecturer", "student"}, false},
		{[]string{"student", "admin"}, false},
	}
	for _, tc := range cases {
		f := openWeek()
		_, err := newReviewService(f, saturday).Submit(context.Background(), 10, tc.roles, withSession())
		if tc.allowed && err != nil {
			t.Errorf("roles %v: unexpected error %v", tc.roles, err)
		}
		if !tc.allowed {
			if !errors.Is(err, ErrMentorReviewForbidden) {
				t.Errorf("roles %v: error %v, want forbidden", tc.roles, err)
			}
			if f.storeCalled || len(f.created) > 0 {
				t.Errorf("roles %v: the store was used before the role check", tc.roles)
			}
		}
		_, err = newReviewService(openWeek(), saturday).ListMine(context.Background(), 10, tc.roles)
		if tc.allowed != (err == nil) {
			t.Errorf("ListMine roles %v: error %v", tc.roles, err)
		}
	}
}

func TestSubmitMentorReviewFlow(t *testing.T) {
	student := []string{"student"}
	ctx := context.Background()

	t.Run("invalid form never reaches the store", func(t *testing.T) {
		f := openWeek()
		sub := withSession()
		sub.Overall = nil
		_, err := newReviewService(f, saturday).Submit(ctx, 10, student, sub)
		var invalid *MentorReviewValidationError
		if !errors.As(err, &invalid) || invalid.Fields[0].Field != "overall" {
			t.Fatalf("error %v, want a validation error on overall", err)
		}
		if f.storeCalled {
			t.Error("the store was used for an invalid form")
		}
	})

	t.Run("unknown module", func(t *testing.T) {
		f := openWeek()
		sub := withSession()
		sub.ModuleID = ptr(int64(99))
		if _, err := newReviewService(f, saturday).Submit(ctx, 10, student, sub); !errors.Is(err, ErrMentorReviewModuleNotFound) {
			t.Errorf("error %v, want module not found", err)
		}
	})

	t.Run("store failure is passed on", func(t *testing.T) {
		boom := errors.New("connection reset")
		f := openWeek()
		f.moduleErr = boom
		if _, err := newReviewService(f, saturday).Submit(ctx, 10, student, withSession()); !errors.Is(err, boom) {
			t.Errorf("error %v, want %v", err, boom)
		}
	})

	t.Run("not enrolled looks like an unknown module", func(t *testing.T) {
		f := openWeek()
		f.enrolled = false
		if _, err := newReviewService(f, saturday).Submit(ctx, 10, student, withSession()); !errors.Is(err, ErrMentorReviewModuleNotFound) {
			t.Errorf("error %v, want module not found", err)
		}
	})

	t.Run("already reviewed wins over a closed window", func(t *testing.T) {
		f := openWeek()
		f.exists = true
		if _, err := newReviewService(f, weekOpens).Submit(ctx, 10, student, withSession()); !errors.Is(err, ErrMentorReviewExists) {
			t.Errorf("error %v, want already reviewed", err)
		}
	})

	t.Run("one second before Saturday", func(t *testing.T) {
		_, err := newReviewService(openWeek(), saturday.Add(-time.Second)).Submit(ctx, 10, student, withSession())
		if !errors.Is(err, ErrMentorReviewNotOpen) || !strings.Contains(err.Error(), "Saturday 14 November 2026 at 00:00 (Baku time)") {
			t.Errorf("error %v, want not open with the opening time", err)
		}
	})

	for name, now := range map[string]time.Time{
		"exactly Saturday 00:00": saturday,
		"Sunday evening":         weekCloses.Add(-time.Hour),
		"after the week closed":  weekCloses.Add(time.Minute),
		"weeks later":            weekCloses.Add(30 * 24 * time.Hour),
	} {
		t.Run(name, func(t *testing.T) {
			f := openWeek()
			if _, err := newReviewService(f, now).Submit(ctx, 10, student, withSession()); err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if len(f.created) != 1 || f.created[0].StudentID != 10 || f.created[0].ModuleID != 4 {
				t.Errorf("stored %+v, want one review by student 10 for module 4", f.created)
			}
		})
	}

	for _, storeErr := range []error{store.ErrMentorReviewNoTeam, store.ErrMentorReviewNoMentor, store.ErrMentorReviewExists} {
		t.Run("store says "+storeErr.Error(), func(t *testing.T) {
			f := openWeek()
			f.createErr = storeErr
			if _, err := newReviewService(f, saturday).Submit(ctx, 10, student, withSession()); !errors.Is(err, storeErr) {
				t.Errorf("error %v, want %v", err, storeErr)
			}
		})
	}
}

func TestListMineReturnsTheStudentsWeeks(t *testing.T) {
	f := openWeek()
	f.reviewed = []store.ReviewedModule{{ModuleID: 3, SubmittedAt: weekOpens}, {ModuleID: 4, SubmittedAt: saturday}}
	got, err := newReviewService(f, saturday).ListMine(context.Background(), 10, []string{"student"})
	if err != nil || !reflect.DeepEqual(got, f.reviewed) {
		t.Errorf("got %v, %v; want %v", got, err, f.reviewed)
	}
}

// ---- admin read: summary math, range rules, missing list ----

func reviewRow(module, mentor int64, hadSession bool, overall int, topics []string, reason string, r *store.MentorReviewRatings) store.MentorReviewRow {
	row := store.MentorReviewRow{ModuleID: module, ModuleTitle: fmt.Sprintf("Week %d", module),
		ModuleOpensAt: weekOpens.AddDate(0, 0, 7*int(module)), MentorID: mentor, MentorName: fmt.Sprintf("Mentor %d", mentor),
		HadSession: hadSession, Overall: overall, Topics: topics, Ratings: r}
	if reason != "" {
		row.NoSessionReason = &reason
	}
	return row
}

func expectedRow(module, mentor int64, reviewed bool) store.ExpectedReviewer {
	return store.ExpectedReviewer{ModuleID: module, ModuleTitle: fmt.Sprintf("Week %d", module),
		ModuleOpensAt: weekOpens.AddDate(0, 0, 7*int(module)), MentorID: mentor, MentorName: fmt.Sprintf("Mentor %d", mentor), Reviewed: reviewed}
}

func TestSummarizeMentorReviews(t *testing.T) {
	r := func(c, e, co, ti int) *store.MentorReviewRatings {
		return &store.MentorReviewRatings{Clarity: c, Engagement: e, Comfort: co, Time: ti}
	}
	rows := []store.MentorReviewRow{
		reviewRow(2, 7, true, 5, []string{"goals", "other"}, "", r(5, 4, 5, 3)),
		reviewRow(2, 7, true, 4, []string{"goals"}, "", r(4, 4, 4, 4)),
		reviewRow(2, 7, false, 2, nil, "not-scheduled", nil),
		reviewRow(1, 7, true, 3, []string{"career"}, "", r(3, 3, 3, 3)),
	}
	expected := []store.ExpectedReviewer{
		expectedRow(2, 7, true), expectedRow(2, 7, true), expectedRow(2, 7, true), expectedRow(2, 7, false),
		expectedRow(2, 9, false), expectedRow(2, 9, false), // mentor 9: a team, no reviews yet
	}
	got := SummarizeMentorReviews(rows, expected)
	if len(got) != 3 {
		t.Fatalf("got %d summaries, want 3 (week 1/mentor 7, week 2/mentor 7, week 2/mentor 9): %+v", len(got), got)
	}
	if got[0].ModuleID != 1 || got[1].ModuleID != 2 || got[1].MentorID != 7 || got[2].MentorID != 9 {
		t.Errorf("order: %d/%d, %d/%d, %d/%d", got[0].ModuleID, got[0].MentorID, got[1].ModuleID, got[1].MentorID, got[2].ModuleID, got[2].MentorID)
	}

	w2 := got[1]
	if w2.Reviews != 3 || w2.Expected != 4 || w2.SessionsHeld != 2 || w2.NoSession != 1 {
		t.Errorf("week 2 counts: reviews %d expected %d held %d none %d", w2.Reviews, w2.Expected, w2.SessionsHeld, w2.NoSession)
	}
	wantAvg := map[string]float64{"clarity": 4.5, "engagement": 4, "comfort": 4.5, "time": 3.5, "overall": 3.67}
	for k, want := range wantAvg {
		if w2.Averages[k] == nil || *w2.Averages[k] != want {
			t.Errorf("week 2 average %s = %v, want %v", k, w2.Averages[k], want)
		}
	}
	if w2.Topics["goals"] != 2 || w2.Topics["other"] != 1 || w2.Topics["career"] != 0 || len(w2.Topics) != 6 {
		t.Errorf("week 2 topics: %v", w2.Topics)
	}
	if w2.NoSessionReasons["not-scheduled"] != 1 || w2.NoSessionReasons["other"] != 0 || len(w2.NoSessionReasons) != 4 {
		t.Errorf("week 2 reasons: %v", w2.NoSessionReasons)
	}

	none := got[2]
	if none.Reviews != 0 || none.Expected != 2 || none.Averages["overall"] != nil || none.Averages["clarity"] != nil {
		t.Errorf("mentor without reviews: %+v", none)
	}

	onlyNoSession := SummarizeMentorReviews([]store.MentorReviewRow{reviewRow(3, 7, false, 1, nil, "mentor-unavailable", nil)}, nil)
	if a := onlyNoSession[0].Averages; a["clarity"] != nil || a["overall"] == nil || *a["overall"] != 1 {
		t.Errorf("no session at all: ratings must be null, overall 1: %v", a)
	}
	if out := SummarizeMentorReviews(nil, nil); out == nil || len(out) != 0 {
		t.Errorf("nothing: %v, want an empty list", out)
	}
}

func TestMentorReviewSummaryRangeAndMissing(t *testing.T) {
	ctx := context.Background()
	f := openWeek() // module 4
	f.expected = []store.ExpectedReviewer{expectedRow(4, 7, true), expectedRow(4, 7, false), expectedRow(4, 9, false)}
	svc := newReviewService(f, saturday)

	if _, err := svc.Summary(ctx, ptr(int64(99)), nil); !errors.Is(err, ErrMentorReviewModuleNotFound) {
		t.Errorf("unknown from_module: %v", err)
	}
	if _, err := svc.Summary(ctx, nil, ptr(int64(99))); !errors.Is(err, ErrMentorReviewModuleNotFound) {
		t.Errorf("unknown to_module: %v", err)
	}
	f.module2 = &store.Module{ID: 5, OpensAt: weekOpens.AddDate(0, 0, 7)}
	if _, err := svc.Summary(ctx, ptr(int64(5)), ptr(int64(4))); !errors.Is(err, ErrMentorReviewRange) {
		t.Errorf("from after to: %v, want the range error", err)
	}
	if _, err := svc.Summary(ctx, ptr(int64(4)), ptr(int64(5))); err != nil {
		t.Errorf("from before to: %v", err)
	}
	if f.gotFilter.OpensFrom == nil || !f.gotFilter.OpensFrom.Equal(weekOpens) || f.gotFilter.OpensTo == nil || !f.gotFilter.OpensTo.Equal(weekOpens.AddDate(0, 0, 7)) {
		t.Errorf("the weeks' opening dates must bound the query: %+v", f.gotFilter)
	}
	if _, err := svc.Summary(ctx, ptr(int64(4)), ptr(int64(4))); err != nil {
		t.Errorf("one week: %v", err)
	}

	missing, err := svc.Missing(ctx, 4)
	if err != nil || len(missing) != 2 || missing[0].Reviewed || missing[1].MentorID != 9 {
		t.Errorf("missing: %+v, %v", missing, err)
	}
	if _, err := svc.Missing(ctx, 99); !errors.Is(err, ErrMentorReviewModuleNotFound) {
		t.Errorf("missing for an unknown week: %v", err)
	}
	f.expected = nil
	if m, _ := svc.Missing(ctx, 4); m == nil || len(m) != 0 {
		t.Errorf("nobody missing: %v, want an empty list", m)
	}
}
