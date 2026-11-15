# Analytics (admin)

Read-only reporting over the existing LMS tables. Every route is admin-only (`RequireRole "admin"`); the
frontend's navigation is not the security boundary. No team analytics, exports, comments, or raw quiz answers.

## Decisions

| Point | Decision |
|---|---|
| Week | A module is one week. Weeks are filtered and ordered by `modules.opens_at`, then `modules.id`. |
| Standing | `enrollments.grade` (green/yellow/red). `total_count` counts only recorded grades, so it equals their sum. A week with no grades is returned with zero counts. |
| Period | `from`/`to` are inclusive `YYYY-MM-DD` dates in school time (Asia/Baku). Default: 12 weeks ending today. Maximum span: 366 days. |
| Assignment eligibility | Non-deleted assignment items with a deadline, in a module the student is currently enrolled in, deadline in the period and already passed. No deadline and future deadlines are excluded. |
| Submission status | Classification uses the first non-deleted submission before the cutoff (`min(end of period, now)`). A late resubmission never turns an on-time submission late. |
| Quiz accuracy | `sum(score) / sum(total) * 100` per module, across attempts. `null` when no points were possible; modules with no attempts are omitted. |
| Feedback period | `mentor_reviews.submitted_at` (not the module week). Only `had_session` reviews have ratings. |
| Feedback privacy | No minimum response count; every average comes with its own count. |
| Mentor standing attribution | `current_roster`: today's team membership is used for every week. The schema keeps no mentor assignment history. |

## Routes

All return the standard JSON error body (`{error,message,status,details}`) on failure.

### `GET /analytics/overview`

`from`, `to` optional.

```json
{
  "period": {"from": "2026-09-01", "to": "2026-11-14"},
  "generated_at": "2026-11-14T17:00:00Z",
  "weeks": [
    {"module_id": 4, "module_title": "Week 4", "opens_at": "2026-09-20T20:00:00Z",
     "green_count": 3, "yellow_count": 1, "red_count": 1, "total_count": 5}
  ]
}
```

### `GET /analytics/students`

`q`, `limit` (1–100, default 20), `offset`. Returns `[{id,name,email}]` for active students.

### `GET /analytics/students/{studentId}`

`from`, `to`, `module_id` optional.

```json
{
  "student": {"id": 10, "name": "Student Name"},
  "period": {"from": "2026-09-01", "to": "2026-11-14"},
  "generated_at": "2026-11-14T17:00:00Z",
  "assignment_status": {"on_time_count": 4, "late_count": 1, "missing_count": 2, "eligible_count": 7},
  "quiz_accuracy_by_module": [
    {"module_id": 4, "module_title": "Week 4", "opens_at": "2026-09-20T20:00:00Z",
     "score_total": 16, "possible_total": 20, "accuracy": 80, "attempt_count": 2}
  ]
}
```

### `GET /analytics/mentors`

Same selector contract as students, for active mentors.

### `GET /analytics/mentors/{mentorId}`

`from`, `to` optional.

```json
{
  "mentor": {"id": 7, "name": "Mentor Name"},
  "period": {"from": "2026-09-01", "to": "2026-11-14"},
  "generated_at": "2026-11-14T17:00:00Z",
  "feedback_by_dimension": [
    {"dimension": "clarity", "average": 4.44, "count": 5},
    {"dimension": "engagement", "average": 4.2, "count": 5},
    {"dimension": "comfort", "average": null, "count": 0},
    {"dimension": "time", "average": 4, "count": 5}
  ],
  "student_standing_by_week": [
    {"module_id": 4, "module_title": "Week 4", "opens_at": "2026-09-20T20:00:00Z",
     "green_count": 3, "yellow_count": 1, "red_count": 1, "total_count": 5}
  ],
  "standing_attribution": "current_roster"
}
```

## Errors

- Bad or missing-format dates, `from > to`, span over 366 days, bad `limit`/`offset`/ids: `400`.
- Unknown student/mentor, or a user without that role: `404`.
- Non-admin: `403`; no token: `401`.
- Empty data is a `200` with zero counts and empty arrays, never an error.

## Known limitations

- Weekly standing is historical because each module is a separate week with its own enrollment grade. Reusing one
  module for a second cohort would break that assumption.
- Mentor standing charts use the current team roster (`standing_attribution: "current_roster"`). Past weeks are not
  attributed to the mentor the student had then; feedback averages do use the mentor recorded on each review.
- Assignment eligibility uses current enrollment, not enrollment history.

## Demo data

`go run ./cmd/seed mock` fills the database with twelve weeks of analytics (three mentor teams, graded enrollments,
tasks and submissions, quizzes, mentor reviews). It replaces only the rows it created, so real data survives, and it
can be run again. The connection comes from `DATABASE_URL`.

It signs in as `analytics.admin@xsolla.school` / `analytics123`; the seeded students and mentors use the same
password. Weeks 6 and 12 have no grades, week 5 has no quiz attempts, and one mentor only has no-session reviews, so
the empty states are visible without touching real data.
