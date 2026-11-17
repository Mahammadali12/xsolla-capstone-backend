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

`q`, `limit` (1–100, default 20), `offset`. Returns `[{id,name,email}]` for active students. Each student with at least one graded week also has the average of
their week grades: `grade_average` (red 1, yellow 2, green 3, one decimal), `grade_level` (the level it rounds to;
.5 rounds up, so 2.5 is green) and `graded_weeks`. The scale is `service.GradeScale` (`internal/service/grade_scale.go`),
the one place to change for another scale. The admin's student list shows `grade_level`.

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

## AI feedback summaries

Both routes are admin-only and generate a summary on demand. The backend reads the feedback itself; the client never
sends records, comments, or prompt text, and never receives them back.

### `POST /analytics/students/{studentId}/ai-summary`

Summarizes the written feedback the student received in the period:

- `enrollments.grade_feedback` — the mentor's week feedback that comes with a green/yellow/red grade;
- `submissions.feedback` — the grader's feedback on graded assignment submissions.

The grade value (green/yellow/red) is **not** sent to the model, so the summary cannot turn into a standing or risk
assessment.

### `POST /analytics/mentors/{mentorId}/ai-summary`

Summarizes the reviews students submitted about the mentor: comments, the four dimension ratings, `overall`,
`had_session`, `no_session_reason`, and `topics`. The backend also computes aggregates (response count and average
per dimension, overall average, session/no-session counts, reason and topic frequencies) and sends them with the
normalized comments.

Request body (both routes; `from`/`to` are optional inclusive school dates, `force_refresh` bypasses the cache):

```json
{"from": "2026-09-01", "to": "2026-11-16", "force_refresh": false}
```

When omitted, the period is the same 12-week default as the charts, and the same 366-day limit applies.

Response (both routes):

```json
{
  "summary": "Plain-text summary, a few short paragraphs.",
  "source_count": 5,
  "generated_at": "2026-11-16T08:30:00Z",
  "cached": false,
  "period": {"from": "2026-09-01", "to": "2026-11-16"}
}
```

- `summary` is plain text only: no HTML, Markdown, code blocks, JSON, links, counts, names, or quotations invented by
  the model. The server rejects provider output that is empty, too long, or contains markup.
- `source_count` is the number of feedback records used (week feedback and submission feedback for a student, reviews
  for a mentor). With no usable records the route returns `200`, a fixed "not enough feedback" message,
  `source_count: 0`, and `generated_at: null`; the provider is not called.
- The model gets no student or mentor names, emails, user ids, grader ids, team ids, or raw quiz data.

### Cache and regeneration

Generated summaries are cached in `analytics_ai_summaries` (migration 027). The cache key is the subject, the period,
the prompt version, and a SHA-256 fingerprint of the normalized records, so new or edited feedback invalidates the old
summary automatically; `force_refresh: true` regenerates it anyway. Concurrent identical requests share one provider
call (`singleflight`). Raw comments and the prompt payload are never stored.

### Prompts and providers

The two system instructions are versioned: `student-feedback-v1` and `mentor-reviews-v1`. Bumping a version
invalidates every cached summary of that type. Feedback is passed as delimited untrusted data, and the instruction
tells the model to ignore directions inside it.

The provider is Gemini, with the same `GEMINI_API_KEY` as quiz generation but its own model: summaries are short,
rule-based prose, so they use the cheaper, faster Flash-Lite (`GEMINI_SUMMARY_MODEL`), while quizzes use `GEMINI_MODEL`
(Flash, `gemini-3.6-flash`). After a failure the other models are tried in turn (`gemini-3.6-flash`, `gemini-3.8-flash`,
`gemini-3.5-flash-lite`). Each is a different model: `gemini-3.7-flash` and `gemini-3.5-flash` are deprecated and Google routes
them to 3.8 and 3.6, and `gemini-3.1-flash-lite` is listed to shut down on 2027-05-07, so none of them is used.
Check the names against the deprecations page of the Gemini API before changing them. Optional settings:

| Variable | Default | Meaning |
|---|---|---|
| `GEMINI_SUMMARY_MODEL` | `gemini-3.5-flash-lite` | The model that writes summaries first. |
| `AI_SUMMARY_TIMEOUT_SECONDS` | `30` | The most one provider call may take, all models and retries together. A dead AI answers `503` after this long. |
| `AI_SUMMARY_MAX_INPUT_CHARS` | `60000` | Input budget; larger sets are summarized in chronological chunks and combined. |
| `AI_SUMMARY_MAX_OUTPUT_CHARS` | `4000` | Longest accepted summary; longer answers are rejected. |

If a single record alone exceeds the input budget the request fails with `502` rather than dropping it.

## Errors

- Bad or missing-format dates, `from > to`, span over 366 days, bad `limit`/`offset`/ids: `400`.
- Unknown student/mentor, or a user without that role: `404`.
- Non-admin: `403`; no token: `401`.
- Empty data is a `200` with zero counts and empty arrays, never an error.
- AI summary routes: provider not configured, timed out, or answered 429/5xx: `503`; unusable provider output or an
  input that cannot be chunked: `502`; other failures: `500`. A failed generation is never replaced by an invented
  fallback summary.

## Known limitations

- Weekly standing is historical because each module is a separate week with its own enrollment grade. Reusing one
  module for a second cohort would break that assumption.
- Mentor standing charts use the current team roster (`standing_attribution: "current_roster"`). Past weeks are not
  attributed to the mentor the student had then; feedback averages do use the mentor recorded on each review.
- Assignment eligibility uses current enrollment, not enrollment history.
- `enrollments.grade_feedback` and `submissions.feedback` hold only the latest text, so an AI summary can only
  describe the currently persisted feedback, not older overwritten versions. `graded_at` is NULL for weeks graded
  before migration 025, so those weeks cannot be period-filtered and are not summarized.

## Demo data

`go run ./cmd/seed mock` fills the database with twelve weeks of analytics: three mentor teams of eight students,
graded enrollments with week feedback, tasks and graded submissions with feedback, quizzes, and mentor reviews. The
feedback and review comments are composed from fragment pools, so the rows read like real notes rather than one
sentence repeated: roughly 240 week notes, 370 task notes and 240 reviews, with a comment on about half of the
reviews. It replaces only the rows it created, so real data survives, and it can be run again. The connection comes
from `DATABASE_URL`.

It signs in as `analytics.admin@xsolla.school` / `analytics123`; the seeded students and mentors use the same
password. Weeks 6 and 12 have no grades, week 5 has no quiz attempts, and one mentor only has no-session reviews, so
the empty states are visible without touching real data.

A seeded student has about 25 feedback records and each mentor about 80 reviews, which fits well inside the default
`AI_SUMMARY_MAX_INPUT_CHARS`. To exercise the chunk-and-combine path against this data, start the server with a small
budget, for example `AI_SUMMARY_MAX_INPUT_CHARS=3000`.

The Bruno collection under `request/Capstone-request/analytics/` has the student and mentor selectors and one request
per summary route. Sign in as the seeded admin, copy the `access_token` into the collection's `accessToken`
variable, then put the ids from the selectors into the summary URLs. Without `GEMINI_API_KEY` the summary routes
answer `503`; the empty-period case (`from`/`to` in a period with no feedback) answers `200` with
`generated_at: null`.
