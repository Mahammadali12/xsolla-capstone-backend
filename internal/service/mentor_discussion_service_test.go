package service

import (
	"context"
	"testing"
	"time"

	"xsolla-capstone-api/internal/store"
)

type discussionStore struct {
	store.Store
	created [][]int64
}

func (f *discussionStore) GetMentorTeamDetail(ctx context.Context, mentorID int64) (*store.TeamDetail, error) {
	return &store.TeamDetail{Members: []store.TeamMember{{ID: 1}, {ID: 2}, {ID: 3}}}, nil
}

func (f *discussionStore) CreateMentorDiscussions(ctx context.Context, mentorID int64, studentIDs []int64, title, note string, startsAt, endsAt time.Time) ([]store.Discussion, error) {
	f.created = append(f.created, studentIDs)
	return []store.Discussion{{ID: 1}}, nil
}

func TestTalkWithStudents(t *testing.T) {
	mentor := []string{"mentor"}
	// a Wednesday: a discussion may be on any day, it is not a 1:1
	start := time.Now().AddDate(0, 0, 3).Truncate(time.Hour)
	end := start.Add(30 * time.Minute)

	cases := []struct {
		name    string
		roles   []string
		title   string
		start   time.Time
		end     time.Time
		ids     []int64
		team    bool
		wantErr bool
		invited []int64
	}{
		{"one student", mentor, "Project plan", start, end, []int64{2}, false, false, []int64{2}},
		{"the whole team", mentor, "Retro", start, end, nil, true, false, []int64{1, 2, 3}},
		{"the same student twice is one invite", mentor, "Retro", start, end, []int64{2, 2}, false, false, []int64{2}},
		{"a student of another team", mentor, "Retro", start, end, []int64{9}, false, true, nil},
		{"nobody chosen", mentor, "Retro", start, end, nil, false, true, nil},
		{"no title", mentor, "  ", start, end, []int64{1}, false, true, nil},
		{"too short", mentor, "Hi", start, start.Add(5 * time.Minute), []int64{1}, false, true, nil},
		{"in the past", mentor, "Hi", start.AddDate(0, -1, 0), end.AddDate(0, -1, 0), []int64{1}, false, true, nil},
		{"a student cannot set one up", []string{"student"}, "Hi", start, end, []int64{1}, false, true, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := &discussionStore{}
			_, err := NewDiscussionService(st).Create(context.Background(), 7, c.roles, c.title, "", c.start, c.end, c.ids, c.team)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, want error: %v", err, c.wantErr)
			}
			if c.wantErr {
				return
			}
			if len(st.created) != 1 || len(st.created[0]) != len(c.invited) {
				t.Fatalf("invited %v, want %v", st.created, c.invited)
			}
			for i, id := range c.invited {
				if st.created[0][i] != id {
					t.Fatalf("invited %v, want %v", st.created[0], c.invited)
				}
			}
		})
	}
}
