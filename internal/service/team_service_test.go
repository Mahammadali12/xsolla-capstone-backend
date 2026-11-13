package service

import (
	"reflect"
	"testing"

	"xsolla-capstone-api/internal/store"
)

func TestRemovedMentors(t *testing.T) {
	one, two, three := int64(1), int64(2), int64(3)
	old := []store.Team{
		{ID: 1, MentorID: &one},
		{ID: 2, MentorID: &two},
		{ID: 3},
	}
	cases := []struct {
		name   string
		inputs []store.TeamInput
		want   []int64
	}{
		{"mentors removed", []store.TeamInput{{}}, []int64{1, 2}},
		{"mentors kept", []store.TeamInput{{MentorID: &one}, {MentorID: &two}}, []int64{}},
		{"mentor moved between teams", []store.TeamInput{{MentorID: &two}, {MentorID: &one}}, []int64{}},
		{"one mentor replaced", []store.TeamInput{{MentorID: &three}, {MentorID: &two}}, []int64{1}},
	}
	for _, tc := range cases {
		if got := removedMentors(old, tc.inputs); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
