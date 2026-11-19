package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xsolla-capstone-api/internal/store"
)

// replaceTeamsSpy records whether ReplaceTeams ran; the other methods are never called here.
type replaceTeamsSpy struct {
	TeamService
	called bool
	inputs []store.TeamInput
}

func (s *replaceTeamsSpy) ReplaceTeams(_ context.Context, inputs []store.TeamInput) ([]store.Team, error) {
	s.called, s.inputs = true, inputs
	return []store.Team{}, nil
}

func TestReplaceTeamsNeedsTheTeamsField(t *testing.T) {
	for _, tc := range []struct {
		body       string
		wantStatus int
		wantCalled bool
	}{
		{`{}`, 422, false},
		{`null`, 422, false},
		{`{"teams": null}`, 422, false},
		{`{"team": []}`, 422, false},
		{`{"teams": [`, 400, false},
		{`{"teams": []}`, 200, true},
		{`{"teams": [{"mentor_id": 2, "student_ids": [3, 4]}]}`, 200, true},
	} {
		spy := &replaceTeamsSpy{}
		rec := httptest.NewRecorder()
		NewTeamHandlers(spy).ReplaceTeams(rec, httptest.NewRequest(http.MethodPut, "/teams", strings.NewReader(tc.body)))
		if rec.Code != tc.wantStatus || spy.called != tc.wantCalled {
			t.Errorf("%s: status %d, replaced %v; want %d, %v", tc.body, rec.Code, spy.called, tc.wantStatus, tc.wantCalled)
		}
	}
}
