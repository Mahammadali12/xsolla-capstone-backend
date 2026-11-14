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

func lfInt(v int) *int       { return &v }
func lfStr(v string) *string { return &v }
func lfID(v int64) *int64    { return &v }

// lfValid is a complete, valid form for lecture 5.
func lfValid() LectureFeedbackSubmission {
	return LectureFeedbackSubmission{EventID: lfID(5), Overall: lfInt(4), Clarity: lfInt(5), Practice: lfInt(3),
		Pace: lfStr("just_right"), Confidence: lfInt(2), Liked: lfStr("  Live coding.  "), Improve: lfStr("")}
}

func TestLectureFeedbackFormDefinition(t *testing.T) {
	var ids []string
	for _, q := range LectureFeedbackForm {
		ids = append(ids, q.ID)
		if q.Label == "" || (q.Type == "scale" && (q.MinLabel == "" || q.MaxLabel == "")) {
			t.Errorf("question %s is incomplete: %+v", q.ID, q)
		}
	}
	if got := strings.Join(ids, ","); got != "overall,clarity,practice,pace,confidence,liked,improve" {
		t.Errorf("questions %s", got)
	}
}

func TestValidateLectureFeedback(t *testing.T) {
	in, problems := ValidateLectureFeedback(lfValid())
	if len(problems) > 0 {
		t.Fatalf("valid form: %v", problems)
	}
	want := store.LectureFeedbackInput{EventID: 5, Overall: 4, Clarity: 5, Practice: 3, Pace: "just_right", Confidence: 2, Liked: lfStr("Live coding.")}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("cleaned form:\n got %+v\nwant %+v", in, want)
	}

	long := strings.Repeat("ə", 1001)
	cases := map[string]struct {
		edit func(*LectureFeedbackSubmission)
		want string
	}{
		"nothing sent":         {func(s *LectureFeedbackSubmission) { *s = LectureFeedbackSubmission{} }, "event_id,overall,clarity,practice,pace,confidence"},
		"event_id 0":           {func(s *LectureFeedbackSubmission) { s.EventID = lfID(0) }, "event_id"},
		"overall 0, clarity 6": {func(s *LectureFeedbackSubmission) { s.Overall, s.Clarity = lfInt(0), lfInt(6) }, "overall,clarity"},
		"unknown pace":         {func(s *LectureFeedbackSubmission) { s.Pace = lfStr("fast") }, "pace"},
		"text over 1000":       {func(s *LectureFeedbackSubmission) { s.Liked, s.Improve = &long, &long }, "liked,improve"},
		"text with a NUL":      {func(s *LectureFeedbackSubmission) { s.Improve = lfStr("a\x00b") }, "improve"},
		"1000 characters":      {func(s *LectureFeedbackSubmission) { s.Liked = lfStr(strings.Repeat("ə", 1000)) }, ""},
		"new lines are fine":   {func(s *LectureFeedbackSubmission) { s.Improve = lfStr("one\ntwo") }, ""},
		"no text at all":       {func(s *LectureFeedbackSubmission) { s.Liked, s.Improve = nil, nil }, ""},
	}
	for name, tc := range cases {
		sub := lfValid()
		tc.edit(&sub)
		_, problems := ValidateLectureFeedback(sub)
		var fields []string
		for _, p := range problems {
			fields = append(fields, p.Field)
		}
		if got := strings.Join(fields, ","); got != tc.want {
			t.Errorf("%s: fields %q, want %q", name, got, tc.want)
		}
	}
}

// lfStore is an in-memory lectureFeedbackStore.
type lfStore struct {
	lecture    *store.Lecture
	enrolled   bool
	sent       bool
	createErr  error
	created    []store.LectureFeedbackInput
	touched    bool
	gotNow     time.Time
	gotWindow  [2]time.Duration
	audience   []store.LectureWithAudience
	rows       []store.LectureFeedbackRow
	lecturers  map[int64][]store.LectureFeedbackPerson
	gotModules []int64
}

func (s *lfStore) GetLecture(_ context.Context, id int64) (*store.Lecture, error) {
	s.touched = true
	if s.lecture == nil || s.lecture.ID != id {
		return nil, store.ErrLectureNotFound
	}
	return s.lecture, nil
}
func (s *lfStore) IsUserEnrolled(context.Context, int64, int64) (bool, error) { return s.enrolled, nil }
func (s *lfStore) HasLectureFeedback(context.Context, int64, int64) (bool, error) {
	return s.sent, nil
}
func (s *lfStore) CreateLectureFeedback(_ context.Context, in store.LectureFeedbackInput) (*store.SentLectureFeedback, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	s.created = append(s.created, in)
	return &store.SentLectureFeedback{EventID: in.EventID, SubmittedAt: time.Now()}, nil
}
func (s *lfStore) ListSentLectureFeedback(context.Context, int64) ([]store.SentLectureFeedback, error) {
	s.touched = true
	return []store.SentLectureFeedback{}, nil
}
func (s *lfStore) ListPendingLectures(_ context.Context, _ int64, now time.Time, before, openFor time.Duration) ([]store.Lecture, error) {
	s.touched, s.gotNow, s.gotWindow = true, now, [2]time.Duration{before, openFor}
	return []store.Lecture{}, nil
}
func (s *lfStore) ListLectureFeedback(context.Context, store.LectureFeedbackFilter) ([]store.LectureFeedbackRow, error) {
	return s.rows, nil
}
func (s *lfStore) ListLecturesWithAudience(context.Context, store.LectureFeedbackFilter) ([]store.LectureWithAudience, error) {
	return s.audience, nil
}
func (s *lfStore) ListWeekLecturers(_ context.Context, ids []int64) (map[int64][]store.LectureFeedbackPerson, error) {
	s.gotModules = ids
	return s.lecturers, nil
}

// lfLecture ends at Thursday 12 November 2026, 21:00 Baku time.
func lfLecture(module *int64) *store.Lecture {
	end := time.Date(2026, 11, 12, 21, 0, 0, 0, lectureFeedbackZone)
	return &store.Lecture{ID: 5, Title: "Goroutines", StartsAt: end.Add(-2 * time.Hour), EndsAt: end, ModuleID: module}
}

func lfService(s *lfStore, now time.Time) *LectureFeedbackService {
	svc := NewLectureFeedbackService(s)
	svc.now = func() time.Time { return now }
	return svc
}

func TestLectureFeedbackRoles(t *testing.T) {
	for _, tc := range []struct {
		roles []string
		ok    bool
	}{
		{[]string{"student"}, true}, {nil, false}, {[]string{"mentor"}, false}, {[]string{"lecturer"}, false},
		{[]string{"admin"}, false}, {[]string{"student", "lecturer"}, false},
	} {
		s := &lfStore{lecture: lfLecture(nil)}
		svc := lfService(s, lfLecture(nil).EndsAt)
		_, errSubmit := svc.Submit(context.Background(), 1, tc.roles, lfValid())
		_, errSent := svc.Sent(context.Background(), 1, tc.roles)
		_, errPending := svc.Pending(context.Background(), 1, tc.roles)
		for name, err := range map[string]error{"submit": errSubmit, "sent": errSent, "pending": errPending} {
			if tc.ok && err != nil {
				t.Errorf("%v %s: %v", tc.roles, name, err)
			}
			if !tc.ok && !errors.Is(err, ErrLectureFeedbackForbidden) {
				t.Errorf("%v %s: %v, want forbidden", tc.roles, name, err)
			}
		}
		if !tc.ok && s.touched {
			t.Errorf("%v: the store was used before the role check", tc.roles)
		}
	}
}

func TestSubmitLectureFeedback(t *testing.T) {
	ctx := context.Background()
	students := []string{"student"}
	end := lfLecture(nil).EndsAt

	t.Run("invalid form never reaches the store", func(t *testing.T) {
		s := &lfStore{lecture: lfLecture(nil)}
		sub := lfValid()
		sub.Pace = nil
		var invalid *LectureFeedbackInvalidError
		if _, err := lfService(s, end).Submit(ctx, 1, students, sub); !errors.As(err, &invalid) || invalid.Problems[0].Field != "pace" || s.touched {
			t.Errorf("error %v (store used: %v)", err, s.touched)
		}
	})
	t.Run("not a lecture or unknown", func(t *testing.T) {
		if _, err := lfService(&lfStore{}, end).Submit(ctx, 1, students, lfValid()); !errors.Is(err, ErrLectureFeedbackNotFound) {
			t.Errorf("error %v", err)
		}
	})
	t.Run("lecture of a week the student is not in", func(t *testing.T) {
		s := &lfStore{lecture: lfLecture(lfID(3)), enrolled: false}
		if _, err := lfService(s, end).Submit(ctx, 1, students, lfValid()); !errors.Is(err, ErrLectureFeedbackNotFound) {
			t.Errorf("error %v", err)
		}
	})
	t.Run("already sent wins over a closed form", func(t *testing.T) {
		s := &lfStore{lecture: lfLecture(nil), sent: true}
		if _, err := lfService(s, end.Add(30*24*time.Hour)).Submit(ctx, 1, students, lfValid()); !errors.Is(err, ErrLectureFeedbackExists) {
			t.Errorf("error %v", err)
		}
	})
	t.Run("11 minutes before the end", func(t *testing.T) {
		_, err := lfService(&lfStore{lecture: lfLecture(nil)}, end.Add(-11*time.Minute)).Submit(ctx, 1, students, lfValid())
		if !errors.Is(err, ErrLectureFeedbackNotOpen) || !strings.Contains(err.Error(), "Thursday 12 November 2026 at 20:50 (Baku time)") {
			t.Errorf("error %v", err)
		}
	})
	t.Run("exactly 7 days after the end", func(t *testing.T) {
		_, err := lfService(&lfStore{lecture: lfLecture(nil)}, end.Add(7*24*time.Hour)).Submit(ctx, 1, students, lfValid())
		if !errors.Is(err, ErrLectureFeedbackClosed) || !strings.Contains(err.Error(), "Thursday 19 November 2026 at 21:00") {
			t.Errorf("error %v", err)
		}
	})
	for name, now := range map[string]time.Time{
		"10 minutes before the end": end.Add(-10 * time.Minute),
		"right after":               end.Add(time.Minute),
		"6 days later":              end.Add(6 * 24 * time.Hour),
	} {
		t.Run(name, func(t *testing.T) {
			s := &lfStore{lecture: lfLecture(lfID(3)), enrolled: true}
			if _, err := lfService(s, now).Submit(ctx, 9, students, lfValid()); err != nil || len(s.created) != 1 || s.created[0].StudentID != 9 {
				t.Errorf("error %v, created %+v", err, s.created)
			}
		})
	}
	t.Run("two sends at once", func(t *testing.T) {
		s := &lfStore{lecture: lfLecture(nil), createErr: store.ErrLectureFeedbackExists}
		if _, err := lfService(s, end).Submit(ctx, 1, students, lfValid()); !errors.Is(err, ErrLectureFeedbackExists) {
			t.Errorf("error %v", err)
		}
	})
	t.Run("pending uses the same window", func(t *testing.T) {
		s := &lfStore{}
		if _, err := lfService(s, end).Pending(ctx, 1, students); err != nil || !s.gotNow.Equal(end) ||
			s.gotWindow != [2]time.Duration{10 * time.Minute, 7 * 24 * time.Hour} {
			t.Errorf("pending: %v, now %v, window %v", err, s.gotNow, s.gotWindow)
		}
	})
}

func TestSummarizeLectureFeedback(t *testing.T) {
	week := int64(3)
	a, b := *lfLecture(&week), *lfLecture(nil)
	b.ID = 6
	row := func(l store.Lecture, overall, clarity, practice, confidence int, pace string, comment *string) store.LectureFeedbackRow {
		return store.LectureFeedbackRow{Lecture: l, Overall: overall, Clarity: clarity, Practice: practice, Confidence: confidence, Pace: pace, Improve: comment}
	}
	rows := []store.LectureFeedbackRow{
		row(a, 5, 4, 3, 2, "just_right", nil),
		row(a, 4, 4, 4, 4, "too_fast", lfStr("slower please")),
		row(a, 3, 5, 5, 3, "too_fast", nil),
		row(store.Lecture{ID: 99}, 1, 1, 1, 1, "too_slow", nil), // a lecture outside the list is ignored
	}
	lecturers := map[int64][]store.LectureFeedbackPerson{3: {{ID: 20, Name: "Leyla"}}}
	got := SummarizeLectureFeedback([]store.LectureWithAudience{{Lecture: a, Expected: 10}, {Lecture: b, Expected: 30}}, rows, lecturers)

	if len(got) != 2 || got[0].Lecture.ID != 5 || got[1].Lecture.ID != 6 {
		t.Fatalf("summaries: %+v", got)
	}
	s := got[0]
	if s.Responses != 3 || s.Expected != 10 || s.WithComments != 1 || len(s.Lecturers) != 1 || s.Lecturers[0].Name != "Leyla" {
		t.Errorf("lecture 5: %+v", s)
	}
	for k, want := range map[string]float64{"overall": 4, "clarity": 4.33, "practice": 4, "confidence": 3} {
		if s.Averages[k] == nil || *s.Averages[k] != want {
			t.Errorf("average %s = %v, want %v", k, s.Averages[k], want)
		}
	}
	if !reflect.DeepEqual(s.Pace, map[string]int{"too_slow": 0, "just_right": 1, "too_fast": 2}) {
		t.Errorf("pace %v", s.Pace)
	}
	e := got[1]
	if e.Responses != 0 || e.Expected != 30 || e.Averages["overall"] != nil || e.Lecturers == nil || len(e.Lecturers) != 0 || e.Pace["too_fast"] != 0 {
		t.Errorf("lecture without feedback: %+v", e)
	}
}

func TestLectureFeedbackListAsksLecturersOncePerWeek(t *testing.T) {
	week := int64(3)
	s := &lfStore{rows: []store.LectureFeedbackRow{{Lecture: *lfLecture(&week)}, {Lecture: *lfLecture(&week)}, {Lecture: *lfLecture(nil)}}}
	if _, _, err := NewLectureFeedbackService(s).List(context.Background(), store.LectureFeedbackFilter{}); err != nil || !reflect.DeepEqual(s.gotModules, []int64{3}) {
		t.Errorf("modules asked %v, %v", s.gotModules, err)
	}
}
