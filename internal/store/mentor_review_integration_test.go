package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

// reviewFixture is one student in a team with a mentor, enrolled in one module.
type reviewFixture struct {
	db        *sql.DB
	store     *StoreImpl
	studentID int64
	mentorID  int64
	teamID    int64
	moduleID  int64
}

func newReviewFixture(t *testing.T) reviewFixture {
	t.Helper()
	database := testDatabase(t)
	tag := time.Now().UnixNano()
	f := reviewFixture{db: database, store: NewStore(database)}
	// users first: their cleanups run last, after the module and team that point at them are gone
	adminID := insertTestUser(t, database, fmt.Sprintf("review-admin-%d@example.com", tag), "Admin")
	f.mentorID = insertTestUser(t, database, fmt.Sprintf("review-mentor-%d@example.com", tag), "Mentor")
	f.studentID = insertTestUser(t, database, fmt.Sprintf("review-student-%d@example.com", tag), "Student")

	opens := time.Now().Add(-7 * 24 * time.Hour)
	mustQuery(t, database, &f.moduleID,
		`INSERT INTO modules (title, created_by, opens_at, closes_at) VALUES ('Week 1', $1, $2, $3) RETURNING id`,
		adminID, opens, opens.Add(7*24*time.Hour))
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM modules WHERE id = $1`, f.moduleID) })

	mustQuery(t, database, &f.teamID, `INSERT INTO teams (name, mentor_id) VALUES ('Team A', $1) RETURNING id`, f.mentorID)
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM teams WHERE id = $1`, f.teamID) })
	mustExec(t, database, `INSERT INTO team_members (team_id, student_id) VALUES ($1, $2)`, f.teamID, f.studentID)
	return f
}

func mustQuery(t *testing.T, db *sql.DB, dest any, query string, args ...any) {
	t.Helper()
	if err := db.QueryRow(query, args...).Scan(dest); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func sessionReview(moduleID, studentID int64) MentorReviewInput {
	other, comment := "CV review", "More code reviews please"
	return MentorReviewInput{
		ModuleID: moduleID, StudentID: studentID, HadSession: true,
		Topics: []string{"goals", "other"}, OtherTopic: &other,
		Ratings: &MentorReviewRatings{Clarity: 5, Engagement: 4, Comfort: 5, Time: 3},
		Overall: 4, Comment: &comment,
	}
}

func TestMentorReviewStoreRoundTrip(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	if got, err := f.store.ListReviewedModules(ctx, f.studentID); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("before: %v, %v; want an empty, non-nil list", got, err)
	}

	r, err := f.store.CreateMentorReview(ctx, sessionReview(f.moduleID, f.studentID))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if r.MentorID != f.mentorID || r.TeamID == nil || *r.TeamID != f.teamID || r.SubmittedAt.IsZero() {
		t.Errorf("stored %+v, want mentor %d and team %d from the student's team", r, f.mentorID, f.teamID)
	}

	var topics, otherTopic, comment string
	var clarity, timeRating, overall int
	if err := f.db.QueryRow(`SELECT array_to_string(topics, ','), other_topic, comment, rating_clarity, rating_time, overall
		FROM mentor_reviews WHERE id = $1`, r.ID).Scan(&topics, &otherTopic, &comment, &clarity, &timeRating, &overall); err != nil {
		t.Fatal(err)
	}
	if topics != "goals,other" || otherTopic != "CV review" || comment != "More code reviews please" || clarity != 5 || timeRating != 3 || overall != 4 {
		t.Errorf("row: topics %q other %q comment %q clarity %d time %d overall %d", topics, otherTopic, comment, clarity, timeRating, overall)
	}

	if _, err := f.store.CreateMentorReview(ctx, sessionReview(f.moduleID, f.studentID)); !errors.Is(err, ErrMentorReviewExists) {
		t.Errorf("second review of the week: %v, want ErrMentorReviewExists", err)
	}
	if ok, err := f.store.HasMentorReview(ctx, f.moduleID, f.studentID); !ok || err != nil {
		t.Errorf("HasMentorReview = %v, %v; want true", ok, err)
	}
	got, err := f.store.ListReviewedModules(ctx, f.studentID)
	if err != nil || len(got) != 1 || got[0].ModuleID != f.moduleID || !got[0].SubmittedAt.Equal(r.SubmittedAt) {
		t.Errorf("after: %+v, %v", got, err)
	}

	// The team gets another mentor: the review stays with the mentor it was about.
	newMentor := insertTestUser(t, f.db, fmt.Sprintf("review-mentor2-%d@example.com", time.Now().UnixNano()), "Mentor 2")
	mustExec(t, f.db, `UPDATE teams SET mentor_id = $1 WHERE id = $2`, newMentor, f.teamID)
	var kept int64
	mustQuery(t, f.db, &kept, `SELECT mentor_id FROM mentor_reviews WHERE id = $1`, r.ID)
	if kept != f.mentorID {
		t.Errorf("after the mentor change the review points at %d, want the old mentor %d", kept, f.mentorID)
	}
}

func TestMentorReviewStoreWithoutSession(t *testing.T) {
	f := newReviewFixture(t)
	reason := "mentor-unavailable"
	r, err := f.store.CreateMentorReview(context.Background(), MentorReviewInput{
		ModuleID: f.moduleID, StudentID: f.studentID, HadSession: false, NoSessionReason: &reason, Overall: 2,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var topicsEmpty, ratingsNull bool
	var gotReason string
	if err := f.db.QueryRow(`SELECT cardinality(topics) = 0, rating_clarity IS NULL, no_session_reason FROM mentor_reviews WHERE id = $1`, r.ID).
		Scan(&topicsEmpty, &ratingsNull, &gotReason); err != nil {
		t.Fatal(err)
	}
	if !topicsEmpty || !ratingsNull || gotReason != reason {
		t.Errorf("row: topics empty %v, ratings null %v, reason %q", topicsEmpty, ratingsNull, gotReason)
	}
}

func TestMentorReviewStoreNeedsATeamWithAMentor(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	mustExec(t, f.db, `UPDATE teams SET mentor_id = NULL WHERE id = $1`, f.teamID)
	if _, err := f.store.CreateMentorReview(ctx, sessionReview(f.moduleID, f.studentID)); !errors.Is(err, ErrMentorReviewNoMentor) {
		t.Errorf("team without mentor: %v, want ErrMentorReviewNoMentor", err)
	}

	mustExec(t, f.db, `DELETE FROM team_members WHERE student_id = $1`, f.studentID)
	if _, err := f.store.CreateMentorReview(ctx, sessionReview(f.moduleID, f.studentID)); !errors.Is(err, ErrMentorReviewNoTeam) {
		t.Errorf("no team: %v, want ErrMentorReviewNoTeam", err)
	}
	if ok, _ := f.store.HasMentorReview(ctx, f.moduleID, f.studentID); ok {
		t.Error("a review was stored without a mentor")
	}
}

// The table refuses contradicting answers even if a caller skips the service's validation.
func TestMentorReviewTableChecks(t *testing.T) {
	f := newReviewFixture(t)
	bad := map[string]string{
		"session without ratings":     `INSERT INTO mentor_reviews (module_id, student_id, mentor_id, had_session, topics, overall) VALUES ($1, $2, $3, true, '{goals}', 3)`,
		"no session without a reason": `INSERT INTO mentor_reviews (module_id, student_id, mentor_id, had_session, overall) VALUES ($1, $2, $3, false, 3)`,
		"unknown topic":               `INSERT INTO mentor_reviews (module_id, student_id, mentor_id, had_session, topics, rating_clarity, rating_engagement, rating_comfort, rating_time, overall) VALUES ($1, $2, $3, true, '{gossip}', 1, 1, 1, 1, 3)`,
		"other without its text":      `INSERT INTO mentor_reviews (module_id, student_id, mentor_id, had_session, topics, rating_clarity, rating_engagement, rating_comfort, rating_time, overall) VALUES ($1, $2, $3, true, '{other}', 1, 1, 1, 1, 3)`,
		"overall 6":                   `INSERT INTO mentor_reviews (module_id, student_id, mentor_id, had_session, no_session_reason, overall) VALUES ($1, $2, $3, false, 'other', 6)`,
		"empty comment":               `INSERT INTO mentor_reviews (module_id, student_id, mentor_id, had_session, no_session_reason, overall, comment) VALUES ($1, $2, $3, false, 'other', 3, '')`,
	}
	for name, query := range bad {
		if _, err := f.db.Exec(query, f.moduleID, f.studentID, f.mentorID); err == nil {
			t.Errorf("%s: the table accepted it", name)
			_, _ = f.db.Exec(`DELETE FROM mentor_reviews WHERE student_id = $1`, f.studentID)
		}
	}
}

// ---- admin read queries ----

func TestMentorReviewAdminQueries(t *testing.T) {
	f := newReviewFixture(t) // student S1 in team A with mentor M1, enrolled in week W1
	ctx := context.Background()
	tag := time.Now().UnixNano()
	user := func(name string, roles ...string) int64 {
		id := insertTestUser(t, f.db, fmt.Sprintf("admin-q-%s-%d@example.com", name, tag), name)
		for _, r := range roles {
			mustExec(t, f.db, `INSERT INTO user_roles (user_id, role) VALUES ($1, $2)`, id, r)
		}
		return id
	}
	team := func(name string, mentor *int64) int64 {
		var id int64
		mustQuery(t, f.db, &id, `INSERT INTO teams (name, mentor_id) VALUES ($1, $2) RETURNING id`, name, mentor)
		t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM teams WHERE id = $1`, id) })
		return id
	}
	join := func(teamID, studentID int64, enrol bool) {
		mustExec(t, f.db, `INSERT INTO team_members (team_id, student_id) VALUES ($1, $2)`, teamID, studentID)
		if enrol {
			mustExec(t, f.db, `INSERT INTO enrollments (module_id, user_id) VALUES ($1, $2)`, f.moduleID, studentID)
		}
	}
	mustExec(t, f.db, `INSERT INTO user_roles (user_id, role) VALUES ($1, 'student')`, f.studentID)
	mustExec(t, f.db, `INSERT INTO enrollments (module_id, user_id) VALUES ($1, $2)`, f.moduleID, f.studentID)

	s2 := user("S2", "student")
	join(f.teamID, s2, true) // expected, no review
	m2 := user("M2", "mentor")
	teamB := team("Team B", &m2)
	s3 := user("S3", "student")
	join(teamB, s3, true) // expected, reviews M2
	left := user("Left", "student")
	join(f.teamID, left, true)
	mustExec(t, f.db, `UPDATE users SET left_at = NOW() WHERE id = $1`, left) // left the school: not expected
	staff := user("Staff", "student", "mentor")
	join(f.teamID, staff, true) // also staff: never asked
	expired := user("Expired", "student")
	mustExec(t, f.db, `UPDATE user_roles SET expires_at = NOW() - interval '1 day' WHERE user_id = $1`, expired)
	join(f.teamID, expired, true) // student role ended: not expected
	noMentor := user("NoMentor", "student")
	join(team("Team C", nil), noMentor, true) // team without mentor: not expected
	notEnrolled := user("NotEnrolled", "student")
	join(f.teamID, notEnrolled, false) // not in this week: not expected

	if _, err := f.store.CreateMentorReview(ctx, sessionReview(f.moduleID, f.studentID)); err != nil {
		t.Fatal(err)
	}
	reason := "mentor-unavailable"
	if _, err := f.store.CreateMentorReview(ctx, MentorReviewInput{ModuleID: f.moduleID, StudentID: s3, NoSessionReason: &reason, Overall: 2}); err != nil {
		t.Fatal(err)
	}

	expected, err := f.store.ListExpectedReviewers(ctx, &f.moduleID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, e := range expected {
		got[e.StudentID] = e.Reviewed
	}
	want := map[int64]bool{f.studentID: true, s2: false, s3: true}
	if len(got) != len(want) {
		t.Errorf("expected reviewers %v, want exactly %v", got, want)
	}
	for id, reviewed := range want {
		if r, ok := got[id]; !ok || r != reviewed {
			t.Errorf("student %d: in list %v, reviewed %v; want reviewed %v", id, ok, r, reviewed)
		}
	}

	all, err := f.store.ListMentorReviews(ctx, MentorReviewFilter{ModuleID: &f.moduleID})
	if err != nil || len(all) != 2 {
		t.Fatalf("all reviews of the week: %d, %v", len(all), err)
	}
	byMentor, _ := f.store.ListMentorReviews(ctx, MentorReviewFilter{MentorID: &f.mentorID})
	if len(byMentor) != 1 || byMentor[0].StudentID != f.studentID || byMentor[0].MentorName != "Mentor" ||
		byMentor[0].TeamName == nil || *byMentor[0].TeamName != "Team A" || byMentor[0].Ratings == nil || byMentor[0].Ratings.Time != 3 ||
		len(byMentor[0].Topics) != 2 || byMentor[0].ModuleTitle != "Week 1" {
		t.Errorf("by mentor: %+v", byMentor)
	}
	byTeam, _ := f.store.ListMentorReviews(ctx, MentorReviewFilter{TeamID: &teamB})
	if len(byTeam) != 1 || byTeam[0].StudentID != s3 || byTeam[0].Ratings != nil || byTeam[0].Topics == nil || *byTeam[0].NoSessionReason != reason {
		t.Errorf("by team: %+v", byTeam)
	}
	later := time.Now().Add(time.Hour)
	if none, _ := f.store.ListMentorReviews(ctx, MentorReviewFilter{ModuleID: &f.moduleID, OpensFrom: &later}); len(none) != 0 {
		t.Errorf("weeks opening after now: %d reviews, want 0", len(none))
	}
}
