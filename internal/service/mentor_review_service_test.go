package service

import (
	"context"
	"errors"
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
	opens := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)   // Monday
	closes := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC) // next Monday
	if got, want := MentorReviewOpensAt(opens, closes), time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("week: opens at %v, want Saturday %v", got, want)
	}
	shortCloses := opens.Add(36 * time.Hour)
	if got := MentorReviewOpensAt(opens, shortCloses); !got.Equal(opens) {
		t.Errorf("module shorter than 48h: opens at %v, want the module's start %v", got, opens)
	}
}

// fakeReviewStore is an in-memory mentorReviewStore.
type fakeReviewStore struct {
	module      *store.Module
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
	if f.module == nil || f.module.ID != id {
		return nil, store.ErrModuleNotFound
	}
	return f.module, nil
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
	weekOpens  = time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	weekCloses = time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	saturday   = time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
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
		if !errors.Is(err, ErrMentorReviewNotOpen) || !strings.Contains(err.Error(), "2026-11-14T00:00:00Z") {
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
