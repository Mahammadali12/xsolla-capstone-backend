package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"

	"xsolla-capstone-api/internal/auth"
)

// Seed accounts are marked by their e-mail so a second run can reuse them and replace only the analytics data.
const (
	seedPassword = "analytics123"
	seedWeekName = "Analytics Week"
	seedTeamName = "Analytics Team"
)

type seedUser struct {
	id    int64
	name  string
	email string
}

var (
	seedMentorNames  = []string{"Rashad Aliyev", "Aysel Mammadova", "Elvin Huseynov"}
	seedStudentNames = []string{
		"Leyla Hasanova", "Murad Guliyev", "Nigar Ismayilova", "Tural Karimov",
		"Sam Student", "Aynur Babayeva", "Kamran Rzayev", "Sabina Valiyeva",
		"Orkhan Mustafayev", "Gunel Ahmadova", "Ali Abbasov", "Banu Hajiyeva",
		"Rauf Mammadov", "Zahra Huseynova", "Elchin Quliyev", "Lala Rahimova",
		"Nijat Suleymanov", "Aysu Karimova", "Farid Nabiyev", "Turkan Jafarova",
		"Ramin Abdullayev", "Sevinj Orujova", "Ilgar Bayramov", "Narmin Safarova",
	}
	seedWeekTitles = []string{
		"Foundations", "Go Basics", "HTTP and APIs", "Databases", "React", "Testing",
		"Deployment", "Observability", "Security", "Teamwork", "Delivery", "Demo Day",
	}
)

// seedAnalytics fills the database with 12 weeks of demo analytics: three mentor teams, graded enrollments, tasks and
// submissions, quizzes and attempts, and written mentor reviews. It replaces only its own rows, so real data survives.
func seedAnalytics(ctx context.Context, db *sql.DB) error {
	hash, err := auth.Hash(seedPassword)
	if err != nil {
		return err
	}
	person := func(email, name string, roles ...string) (seedUser, error) {
		id, err := ensureSeedUser(ctx, db, email, name, hash, roles)
		return seedUser{id: id, name: name, email: email}, err
	}

	admin, err := person("analytics.admin@xsolla.school", "Analytics Admin", "admin")
	if err != nil {
		return err
	}
	if _, err := person("analytics.lecturer@xsolla.school", "Analytics Lecturer", "lecturer"); err != nil {
		return err
	}

	mentors := make([]seedUser, len(seedMentorNames))
	for i, name := range seedMentorNames {
		mentors[i], err = person(fmt.Sprintf("analytics.mentor%d@xsolla.school", i+1), name, "mentor")
		if err != nil {
			return err
		}
	}
	students := make([]seedUser, len(seedStudentNames))
	for i, name := range seedStudentNames {
		students[i], err = person(fmt.Sprintf("analytics.student%02d@xsolla.school", i+1), name, "student")
		if err != nil {
			return err
		}
	}

	// Rows from an earlier run go first; cascades take their enrollments, items, submissions, quizzes and reviews.
	// Seeded modules are the ones this seed account created, so real modules are never touched.
	if _, err := db.ExecContext(ctx, `DELETE FROM modules WHERE created_by = $1`, admin.id); err != nil {
		return fmt.Errorf("clear seeded modules: %w", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM teams WHERE name LIKE $1`, seedTeamName+" %"); err != nil {
		return fmt.Errorf("clear seeded teams: %w", err)
	}

	// Three teams of eight students, one per mentor.
	teamSize := len(students) / len(mentors)
	teamIDs := make([]int64, len(mentors))
	for i, mentor := range mentors {
		if err := db.QueryRowContext(ctx, `INSERT INTO teams (name, mentor_id) VALUES ($1, $2) RETURNING id`,
			fmt.Sprintf("%s %c", seedTeamName, 'A'+rune(i)), mentor.id).Scan(&teamIDs[i]); err != nil {
			return fmt.Errorf("create team %d: %w", i, err)
		}
	}
	for i, student := range students {
		if _, err := db.ExecContext(ctx, `INSERT INTO team_members (team_id, student_id) VALUES ($1, $2)`,
			teamIDs[i/teamSize], student.id); err != nil {
			return fmt.Errorf("place student %d: %w", i, err)
		}
	}

	// Twelve weekly modules. The newest opens this week; the latest school date is today.
	today := time.Now().UTC()
	base := time.Date(today.Year(), today.Month(), today.Day(), 8, 0, 0, 0, time.UTC).AddDate(0, 0, -7*11)
	moduleIDs := make([]int64, len(seedWeekTitles))
	opensAt := make([]time.Time, len(seedWeekTitles))
	for i, title := range seedWeekTitles {
		opensAt[i] = base.AddDate(0, 0, 7*i)
		if err := db.QueryRowContext(ctx,
			`INSERT INTO modules (title, description, created_by, opens_at, closes_at)
			 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			fmt.Sprintf("Week %d · %s", i+1, title),
			"Demo week for the admin analytics screens.",
			admin.id, opensAt[i], opensAt[i].AddDate(0, 0, 7)).Scan(&moduleIDs[i]); err != nil {
			return fmt.Errorf("create module %d: %w", i, err)
		}
	}

	// Enrollments carry the weekly standing and the mentor's week feedback. Weeks 6 and 12 are left ungraded, so
	// the charts show two empty weeks and the student AI summary has weeks without feedback.
	grades := []string{"green", "green", "yellow", "green", "red", "green", "yellow"}
	for si, student := range students {
		for wi, moduleID := range moduleIDs {
			var grade, feedback, gradedAt any
			if wi != 5 && wi != 11 {
				grade = grades[(si*3+wi*2)%len(grades)]
				feedback = weekNote(si, wi)
				gradedAt = opensAt[wi].AddDate(0, 0, 6)
			}
			if _, err := db.ExecContext(ctx,
				`INSERT INTO enrollments (module_id, user_id, grade, grade_feedback, graded_at)
				 VALUES ($1, $2, $3, $4, $5)`,
				moduleID, student.id, grade, feedback, gradedAt); err != nil {
				return fmt.Errorf("enroll student %d in week %d: %w", si, wi, err)
			}
		}
	}

	// Assignment status: two deadlines per week, a mix of on time, late and missing. The current week's deadlines are
	// still ahead, so the backend leaves them out; that is the point of the "not yet due" rule.
	for wi, moduleID := range moduleIDs {
		for ai := 0; ai < 2; ai++ {
			dueAt := opensAt[wi].AddDate(0, 0, 3+ai*2)
			itemID := int64(0)
			if err := db.QueryRowContext(ctx,
				`INSERT INTO content_items (module_id, type, title, description, position, created_by)
				 VALUES ($1, 'assignment', $2, '', $3, $4) RETURNING id`,
				moduleID, fmt.Sprintf("%s %d task %d", seedWeekName, wi+1, ai+1), ai, admin.id).Scan(&itemID); err != nil {
				return fmt.Errorf("create task week %d: %w", wi, err)
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO assignments (content_item_id, due_at) VALUES ($1, $2)`, itemID, dueAt); err != nil {
				return fmt.Errorf("deadline week %d: %w", wi, err)
			}
			for si, student := range students {
				var submittedAt time.Time
				switch (si + wi + ai) % 7 {
				case 0:
					continue // missing
				case 1:
					submittedAt = dueAt.AddDate(0, 0, 1) // late
				default:
					submittedAt = dueAt.AddDate(0, 0, -1) // on time
				}
				if _, err := db.ExecContext(ctx,
					`INSERT INTO submissions (content_item_id, student_id, text_body, submitted_at, feedback, graded_by, graded_at)
					 VALUES ($1, $2, 'Seeded work', $3, $4, $5, $6)`,
					itemID, student.id, submittedAt, submissionNote(si, wi, ai), admin.id,
					submittedAt.AddDate(0, 0, 2)); err != nil {
					return fmt.Errorf("submission week %d student %d: %w", wi, si, err)
				}
			}
		}
	}

	// One quiz per week. Week 5 has no attempts at all, so it is absent from the accuracy chart rather than 0%.
	for wi, moduleID := range moduleIDs {
		quizID := int64(0)
		if err := db.QueryRowContext(ctx,
			`INSERT INTO quizzes (module_id, title, description, due_at, created_by)
			 VALUES ($1, $2, '', $3, $4) RETURNING id`,
			moduleID, fmt.Sprintf("%s %d quiz", seedWeekName, wi+1), opensAt[wi].AddDate(0, 0, 6), admin.id).Scan(&quizID); err != nil {
			return fmt.Errorf("create quiz week %d: %w", wi, err)
		}
		if wi == 4 {
			continue
		}
		for si, student := range students {
			if (si+wi)%5 == 0 {
				continue // this student skipped the quiz
			}
			score := 10 - (si+wi)%4
			if _, err := db.ExecContext(ctx,
				`INSERT INTO quiz_attempts (quiz_id, student_id, answers, score, total) VALUES ($1, $2, '[]'::jsonb, $3, 10)`,
				quizID, student.id, score); err != nil {
				return fmt.Errorf("quiz attempt week %d student %d: %w", wi, si, err)
			}
		}
	}

	// Reviews over the first eleven weeks; the last week is still running, so nobody reviews it yet. Mentor C has
	// only "no session" reviews with varying reasons, so every dimension reads "No responses" for that team, while
	// the other two teams have rated sessions, several topics and a comment on most reviews.
	for si, student := range students {
		mentor := mentors[si/teamSize]
		teamID := teamIDs[si/teamSize]
		noSessionTeam := si >= 2*teamSize
		for wi, moduleID := range moduleIDs[:11] {
			if (si+wi*3)%10 == 0 {
				continue // a few students skip a week
			}
			submittedAt := opensAt[wi].AddDate(0, 0, 4)
			overall := 2 + (si+wi)%4
			comment := reviewComment(si, wi, noSessionTeam)
			var err error
			if noSessionTeam {
				_, err = db.ExecContext(ctx,
					`INSERT INTO mentor_reviews
					 (module_id, student_id, mentor_id, team_id, had_session, no_session_reason, overall, comment, submitted_at)
					 VALUES ($1, $2, $3, $4, false, $5, $6, $7, $8)`,
					moduleID, student.id, mentor.id, teamID,
					noSessionReasons[(si+wi)%len(noSessionReasons)], overall, comment, submittedAt)
			} else {
				topics := reviewTopics(si, wi)
				var otherTopic any
				if topics[0] == "other" {
					otherTopic = otherTopics[(si+wi)%len(otherTopics)]
				}
				_, err = db.ExecContext(ctx,
					`INSERT INTO mentor_reviews
					 (module_id, student_id, mentor_id, team_id, had_session, topics, other_topic,
					  rating_clarity, rating_engagement, rating_comfort, rating_time, overall, comment, submitted_at)
					 VALUES ($1, $2, $3, $4, true, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
					moduleID, student.id, mentor.id, teamID, pq.Array(topics), otherTopic,
					3+(si+wi)%3, 3+(si*2+wi)%3, 4-(si+wi)%2, 3+(si*2+wi*2)%3, overall, comment, submittedAt)
			}
			if err != nil {
				return fmt.Errorf("review week %d student %d: %w", wi, si, err)
			}
		}
	}

	fmt.Printf("seeded %d weeks, %d students, %d mentors\n", len(moduleIDs), len(students), len(mentors))
	fmt.Printf("admin sign-in: analytics.admin@xsolla.school / %s\n", seedPassword)
	return nil
}

// ensureSeedUser finds or creates a seed account and makes sure it is active with exactly the given roles.
func ensureSeedUser(ctx context.Context, db *sql.DB, email, name, hash string, roles []string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx,
		`SELECT id FROM users WHERE email = $1 ORDER BY (deleted_at IS NULL) DESC, id LIMIT 1`, email).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err := db.QueryRowContext(ctx,
			`INSERT INTO users (email, password_hash, name) VALUES ($1, $2, $3) RETURNING id`,
			email, hash, name).Scan(&id); err != nil {
			return 0, fmt.Errorf("create %s: %w", email, err)
		}
	case err != nil:
		return 0, fmt.Errorf("find %s: %w", email, err)
	default:
		if _, err := db.ExecContext(ctx,
			`UPDATE users SET name = $2, password_hash = $3, left_at = NULL, deleted_at = NULL WHERE id = $1`,
			id, name, hash); err != nil {
			return 0, fmt.Errorf("revive %s: %w", email, err)
		}
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = $1`, id); err != nil {
		return 0, fmt.Errorf("reset roles of %s: %w", email, err)
	}
	for _, role := range roles {
		if _, err := db.ExecContext(ctx, `INSERT INTO user_roles (user_id, role) VALUES ($1, $2)`, id, role); err != nil {
			return 0, fmt.Errorf("give %s the %s role: %w", email, role, err)
		}
	}
	return id, nil
}

// ---- prose pools -----------------------------------------------------------------------------------------------
//
// The seed composes feedback from fragments instead of repeating a handful of sentences, so the AI summaries have
// real themes to find. Every pick is deterministic from the student and week, so a re-run produces the same data.

var (
	weekStrengths = []string{
		"Strong week: the tasks were finished on time and the code reads well.",
		"The design notes were clear and the implementation matched them.",
		"Good debugging this week; the fixes were small and targeted.",
		"The pull requests were easy to review and well described.",
		"Solid understanding of the lecture material, and it showed in the exercises.",
		"The database work was careful, with sensible indexes and queries.",
		"Steady progress: the API endpoints are consistent and predictable.",
		"Nice recovery after the shaky start; the final version is clean.",
	}
	weekConcerns = []string{
		"Error handling is still thin: most failures are silently ignored.",
		"Tests cover the happy path only.",
		"The commit history is hard to follow because the messages are vague.",
		"Some functions are long enough that the logic is hard to follow.",
		"Edge cases are skipped, especially empty input and slow responses.",
		"The work was rushed at the end, and it shows in the naming.",
		"Indentation and naming are inconsistent between files.",
		"The explanation of trade-offs is missing, so the choices look arbitrary.",
	}
	weekNextSteps = []string{
		"Next week, add tests for the failure paths before opening the pull request.",
		"Next time, split the work into smaller commits and describe each one.",
		"Try to write error messages for the person who will read them.",
		"Walk through one edge case on paper before writing the code.",
		"Ask for a review early instead of at the end of the week.",
		"Write a short README section explaining the design decisions.",
	}

	taskStrengths = []string{
		"The solution works and the structure is easy to follow.",
		"Nicely scoped: the function does one thing and does it well.",
		"The naming is clear enough that the code needs almost no comments.",
		"Good use of the standard library instead of a new dependency.",
		"The output is correct and the formatting matches the spec.",
		"There is a clear separation between the handler and the business logic.",
	}
	taskFixes = []string{
		"Add tests for the invalid-input cases.",
		"The error path returns a generic message; make it specific.",
		"Extract the repeated block into a helper.",
		"Guard against a missing record before dereferencing it.",
		"Explain the trade-off in the README.",
		"Handle the empty list without panicking.",
	}

	sessionComments = []string{
		"The explanations were clear and I left with a concrete plan for the week.",
		"Good session: we walked through the project and the feedback was specific.",
		"Very approachable, and the code review pointed at real problems instead of style.",
		"The mentor answered my questions about the database schema patiently.",
		"Helpful session on the deployment steps; everything worked afterwards.",
		"Clear structure and honest feedback about what to improve.",
		"The mentor made time for an extra meeting before the deadline.",
		"The advice on splitting work into smaller tasks helped the whole team.",
		"The session was useful, though it started late and we lost about fifteen minutes.",
		"Clear advice on the project, but the meeting was too short to cover the tests.",
		"Good technical feedback; the next steps could have been written down.",
		"The mentor knows the material well, but the sessions are often scheduled last minute.",
		"Helpful review of the code, although the comments came after the deadline.",
		"The explanation was clear, but I would have liked a concrete example.",
		"The mentor is responsive in chat, but the live sessions feel rushed.",
		"Useful discussion about the goals, but we did not get to the homework questions.",
		"The session felt rushed and my questions were answered briefly.",
		"I asked for help with the database task and did not get a clear answer.",
		"Two sessions were cancelled without a replacement, which hurt the project.",
		"The feedback was very general and did not say what to change.",
		"The mentor mostly repeated the lecture slides.",
		"It is hard to book time; the answers in chat arrive a day later.",
		"The code review missed the failing test that blocked our submission.",
		"The mentor seemed unprepared for the questions about deployment.",
	}
	noSessionComments = []string{
		"No session this week; I did not need one.",
		"The session was not scheduled, so I worked through the material alone.",
		"We could not find a time that worked for both of us.",
		"I had to cancel because of the deadline, and we did not reschedule.",
	}
	noSessionReasons = []string{"not-scheduled", "student-unavailable", "mentor-unavailable", "other"}
	sessionTopicSets = [][]string{
		{"goals"},
		{"project"},
		{"homework", "lecture"},
		{"career"},
		{"goals", "project"},
		{"homework"},
	}
	otherTopics = []string{"CV review", "University applications", "Interview practice", "Time planning"}
)

// weekNote is one week's grade feedback: a strength, a concern and a next step.
func weekNote(student, week int) string {
	return weekStrengths[(student*3+week)%len(weekStrengths)] + " " +
		weekConcerns[(student+week*2)%len(weekConcerns)] + " " +
		weekNextSteps[(student*5+week)%len(weekNextSteps)]
}

// submissionNote gives most seeded submissions grader feedback and leaves some without, so the student AI
// summary has both graded and feedback-less records to work with.
func submissionNote(student, week, task int) any {
	if (student+week+task)%4 == 0 {
		return nil
	}
	return taskStrengths[(student*2+week+task)%len(taskStrengths)] + " " +
		taskFixes[(student+week*3+task*2)%len(taskFixes)]
}

// reviewTopics picks the topics of a session review. Every ninth review uses the free-text "other" topic.
func reviewTopics(student, week int) []string {
	if (student+week)%9 == 0 {
		return []string{"other"}
	}
	return sessionTopicSets[(student*2+week)%len(sessionTopicSets)]
}

// reviewComment leaves roughly two reviews in five without a comment, which keeps the summaries honest about
// how many students wrote something.
func reviewComment(student, week int, noSession bool) any {
	if noSession {
		if (student+week)%4 != 1 {
			return nil
		}
		return noSessionComments[(student+week)%len(noSessionComments)]
	}
	if (student+week*2)%5 >= 3 {
		return nil
	}
	return sessionComments[(student*7+week*3)%len(sessionComments)]
}
