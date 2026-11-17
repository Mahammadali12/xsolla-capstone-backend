package service

import (
	"context"
	"testing"

	"xsolla-capstone-api/internal/store"
)

func TestAverageGrade(t *testing.T) {
	cases := []struct {
		name    string
		grades  []string
		avg     float64
		level   string
		counted int
	}{
		{"five greens and a red", []string{"green", "green", "green", "green", "green", "red"}, 2.7, "green", 6},
		{"2.2 rounds down to yellow", []string{"green", "green", "yellow", "red", "yellow"}, 2.2, "yellow", 5},
		{"exactly 2.5 rounds up to green", []string{"green", "yellow"}, 2.5, "green", 2},
		{"1.5 rounds up to yellow", []string{"red", "yellow"}, 1.5, "yellow", 2},
		{"all red", []string{"red", "red"}, 1, "red", 2},
		{"unknown levels are skipped", []string{"green", "blue", ""}, 3, "green", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := AverageGrade(c.grades)
			if got == nil || got.Average != c.avg || got.Level != c.level || got.GradedWeeks != c.counted {
				t.Errorf("got %+v, want %v %s over %d", got, c.avg, c.level, c.counted)
			}
		})
	}
	if AverageGrade(nil) != nil || AverageGrade([]string{"blue"}) != nil {
		t.Error("no known grade must give nil")
	}
}

// the base fake answers no grades; gaStore gives some
func (f *fakeAnalyticsStore) ListStudentWeekGrades(context.Context, []int64) (map[int64][]string, error) {
	return nil, f.err
}

type gaStore struct {
	*fakeAnalyticsStore
	grades map[int64][]string
	asked  []int64
}

func (g *gaStore) ListStudentWeekGrades(_ context.Context, ids []int64) (map[int64][]string, error) {
	g.asked = ids
	return g.grades, nil
}

func TestSearchStudentsAddsTheAverage(t *testing.T) {
	st := &gaStore{
		fakeAnalyticsStore: &fakeAnalyticsStore{people: []store.AnalyticsPerson{{ID: 1, Name: "Anna"}, {ID: 2, Name: "Bek"}}},
		grades:             map[int64][]string{1: {"green", "green", "green", "green", "green", "red"}},
	}
	people, err := NewAnalyticsService(st).SearchStudents(context.Background(), "", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.asked) != 2 {
		t.Errorf("asked for %v, want both students in one call", st.asked)
	}
	if a := people[0]; a.GradeAverage == nil || *a.GradeAverage != 2.7 || a.GradeLevel != "green" || a.GradedWeeks != 6 {
		t.Errorf("Anna: %+v", a)
	}
	if b := people[1]; b.GradeAverage != nil || b.GradeLevel != "" {
		t.Errorf("Bek has no grade yet: %+v", b)
	}
}
