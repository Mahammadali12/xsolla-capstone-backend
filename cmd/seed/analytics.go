package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

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

	// Three teams of four students, one per mentor.
	teamIDs := make([]int64, len(mentors))
	for i, mentor := range mentors {
		if err := db.QueryRowContext(ctx, `INSERT INTO teams (name, mentor_id) VALUES ($1, $2) RETURNING id`,
			fmt.Sprintf("%s %c", seedTeamName, 'A'+rune(i)), mentor.id).Scan(&teamIDs[i]); err != nil {
			return fmt.Errorf("create team %d: %w", i, err)
		}
	}
	for i, student := range students {
		if _, err := db.ExecContext(ctx, `INSERT INTO team_members (team_id, student_id) VALUES ($1, $2)`,
			teamIDs[i/4], student.id); err != nil {
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

	// Enrollments carry the weekly standing. Weeks 6 and 12 are left ungraded, so the charts show two empty weeks.
	grades := []string{"green", "green", "yellow", "green", "red", "green", "yellow"}
	for si, student := range students {
		for wi, moduleID := range moduleIDs {
			var grade any
			if wi != 5 && wi != 11 {
				grade = grades[(si*3+wi*2)%len(grades)]
			}
			if _, err := db.ExecContext(ctx,
				`INSERT INTO enrollments (module_id, user_id, grade) VALUES ($1, $2, $3)`,
				moduleID, student.id, grade); err != nil {
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
					`INSERT INTO submissions (content_item_id, student_id, text_body, submitted_at) VALUES ($1, $2, 'Seeded work', $3)`,
					itemID, student.id, submittedAt); err != nil {
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

	// Reviews over the first eleven weeks. Mentor C has only "no session" reviews, so all four dimensions read
	// "No responses"; the other two have rated sessions with counts that differ by week.
	for si, student := range students {
		mentor := mentors[si/4]
		for wi, moduleID := range moduleIDs[:11] {
			if (si+wi)%4 == 3 {
				continue
			}
			submittedAt := opensAt[wi].AddDate(0, 0, 4)
			var err error
			if si >= 8 {
				_, err = db.ExecContext(ctx,
					`INSERT INTO mentor_reviews
					 (module_id, student_id, mentor_id, team_id, had_session, no_session_reason, topics, overall, submitted_at)
					 VALUES ($1, $2, $3, $4, false, 'not-scheduled', '{}', 3, $5)`,
					moduleID, student.id, mentor.id, teamIDs[si/4], submittedAt)
			} else {
				_, err = db.ExecContext(ctx,
					`INSERT INTO mentor_reviews
					 (module_id, student_id, mentor_id, team_id, had_session, topics,
					  rating_clarity, rating_engagement, rating_comfort, rating_time, overall, submitted_at)
					 VALUES ($1, $2, $3, $4, true, '{goals}', $5, $6, $7, $8, 4, $9)`,
					moduleID, student.id, mentor.id, teamIDs[si/4],
					3+(si+wi)%3, 3+(si*2+wi)%3, 4-(si+wi)%2, 3+(si*2+wi*2)%3, submittedAt)
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
