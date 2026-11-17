# Weekly mentor reviews

Every week (module) each student reviews the week with **their team's mentor**. This replaces the Google Form
"Mentor Session Feedback". The front-end (`xsolla-capstone-frontend`, page `/mentor-review`) already uses these
routes; its full requirements are in `backend_requirements/BACKEND_REQUIREMENTS.md` there.

Code: `internal/handlers/mentor_review.go`, `internal/service/mentor_review_service.go`,
`internal/store/mentor_review_store.go`, migration `021_mentor_reviews`.

## Who

Only users whose **only** role is `student`. Mentors, lecturers and admins get `403`, also when they hold the
student role as well (the front-end never asks them either). A student needs a team with a mentor to send one.

## GET /mentor-reviews/me

The weeks the signed-in student has reviewed, oldest first. Answers are not returned.

```json
[ { "module_id": 4, "submitted_at": "2026-11-15T10:12:00Z" } ]
```

`[]` when there are none. `401` without a token, `403` for staff.

## POST /mentor-reviews

With a session:

```json
{ "module_id": 4, "had_session": true,
  "topics": ["goals", "other"], "other_topic": "CV review",
  "ratings": { "clarity": 5, "engagement": 4, "comfort": 5, "time": 3 },
  "overall": 4, "comment": "More code reviews please" }
```

Without a session:

```json
{ "module_id": 4, "had_session": false, "no_session_reason": "not-scheduled", "overall": 2 }
```

`201` with `{ "module_id": 4, "submitted_at": "..." }`. The server sets the student from the token and copies the
**mentor and team the student has at that moment** onto the row, so the review stays with that mentor if the team's
mentor changes later. A review cannot be changed or deleted.

### Form rules (`422 validation_failed`, every broken rule listed in `details`)

| Field | Rule |
|---|---|
| `module_id` | required, positive |
| `had_session` | required, `true` or `false` |
| `no_session_reason` | required when `had_session` is false: `mentor-unavailable`, `student-unavailable`, `not-scheduled`, `other`; not allowed when true |
| `topics` | when `had_session` is true: at least one of `goals`, `homework`, `lecture`, `project`, `career`, `other`, no repeats; not allowed when false |
| `other_topic` | required when `topics` has `other`, at most 120 characters; not allowed otherwise |
| `ratings` | when `had_session` is true: all four of `clarity`, `engagement`, `comfort`, `time`, each 1 to 5, no other keys; not allowed when false |
| `overall` | required, 1 to 5 |
| `comment` | optional, at most 1000 characters |

Text is trimmed; empty optional text, `[]` and `{}` count as "not sent". A value of the wrong JSON type (`"4"` for
`overall`, `3.5` for a rating) is also a `422` naming the field.

### Every answer

| Status | `error` | When |
|---|---|---|
| 201 | | stored |
| 400 | `bad_request` | the body is not one JSON object |
| 401 | | no or bad token |
| 403 | `forbidden` | not only a student |
| 404 | `not_found` | unknown or deleted module, or the student is not enrolled in it |
| 409 | `already_reviewed` | this week is already reviewed (also when two tabs send at once). Checked before the rules below, so a re-send always gets 409 |
| 413 | `payload_too_large` | body over 16 KB |
| 422 | `validation_failed` | a form rule is broken (with `details`); the week is not open yet (message says when it opens); the student has no team; the team has no mentor |
| 500 | `server_error` | database error (logged) |

### When a week can be reviewed

From **Saturday 00:00, Baku time, of the last weekend before the module closes** (the moment the front-end starts
showing the form), but never before the module opens, and at any time after it closed (a missing review can always
be sent). A week that closes on Sunday night, Monday 00:00 or Monday 23:59 opens its review on the Saturday just
before. The 422 message says when it opens, in Baku time (`service.SchoolLocation`, UTC+4).

## Decisions taken (BACKEND_REQUIREMENTS.md section 8)

- D1: mentors do not see reviews about themselves; only admins can read them (routes below).
- D2: "Saturday" is counted in the school's time zone, Asia/Baku (UTC+4).
- D3: the mentor is the team's mentor when the review is sent; with no team or no mentor the review is refused (422).
- D5: there is no end to the window, so the front-end's lock can always be cleared.
- Built: the admin read routes for the analytics page (section 7), below.
- Not built yet: the server-side lock (D4, optional) and the Saturday reminder notification.

## Reading the reviews (admin only)

Registered with `RequireRole "admin"`: everyone else gets `403` (D1: mentors never see the reviews about them,
also not as averages). Ids in the query must be positive whole numbers (`400` naming the parameter); an empty value
counts as not sent.

### GET /mentor-reviews?module_id=&mentor_id=&team_id=

Every filter is optional. The reviews with every answer, by week, then mentor, then student; `[]` when none.

```json
[ { "id": 12, "module_id": 4, "module_title": "Week 4: Go",
    "student": { "id": 31, "name": "Anna", "email": "anna@example.com" },
    "mentor":  { "id": 7,  "name": "Mira", "email": "mira@example.com" },
    "team": { "id": 3, "name": "Team A" },
    "had_session": true, "no_session_reason": null, "topics": ["goals", "other"], "other_topic": "CV review",
    "ratings": { "clarity": 5, "engagement": 4, "comfort": 5, "time": 3 },
    "overall": 4, "comment": "More code reviews please", "submitted_at": "2026-11-15T10:12:00Z" } ]
```

`mentor` is the mentor the review is about (kept on the row when the team's mentor changes). `team` is `null` when
that team was deleted; `ratings` is `null` and `topics` is `[]` for a week without a session.

### GET /mentor-reviews/summary?from_module=&to_module=

One entry per week and mentor, for the weeks from `from_module` to `to_module` by opening date (both optional, both
included; unknown week `404`; `from_module` opening after `to_module` `400`).

```json
[ { "module_id": 4, "module_title": "Week 4: Go", "mentor": { "id": 7, "name": "Mira", "email": "mira@example.com" },
    "reviews": 5, "expected": 6, "sessions_held": 4, "no_session": 1,
    "averages": { "clarity": 4.5, "engagement": 4.25, "comfort": 4.75, "time": 3.5, "overall": 4.2 },
    "topics": { "goals": 3, "homework": 2, "lecture": 0, "project": 1, "career": 0, "other": 1 },
    "no_session_reasons": { "mentor-unavailable": 0, "student-unavailable": 0, "not-scheduled": 1, "other": 0 } } ]
```

- `reviews`, `sessions_held`, `no_session`, `topics`, `no_session_reasons`: from the reviews about that mentor.
- `averages`: the four ratings over the reviews with a session, `overall` over all reviews; two decimals; `null`
  when there is nothing to average. Every topic and reason key is always present.
- `expected`: how many students the mentor's team has **today** that are asked for the week (see "Who is expected"
  below). Teams are not kept per week, so after a mentor change the old mentor keeps their reviews with
  `expected: 0` and the new mentor shows the students as expected.

### GET /mentor-reviews/missing?module_id=

`module_id` is required (`400`; unknown week `404`). The students who are expected for the week and have not sent
its review, by team then name; `[]` when everyone has.

```json
[ { "student": { "id": 32, "name": "Bek", "email": "bek@example.com" },
    "team": { "id": 3, "name": "Team A" }, "mentor": { "id": 7, "name": "Mira", "email": "mira@example.com" } } ]
```

### Who is expected

A student who, today, is in a team with a mentor, is enrolled in the week, has not left (`left_at`) or been deleted,
has an active `student` role and no active mentor, lecturer or admin role. This is the same group the front-end asks.
It does not check whether the week's review is open yet: for a running week the list shows who still has to review.

## Database

`mentor_reviews`: one row per `(module_id, student_id)` (unique). Besides the service's validation, CHECK constraints
refuse contradicting rows (a session without ratings, no session without a reason, unknown topics, `other` without
its text, scores outside 1 to 5, empty comments).

## Tests

- `go test ./internal/service/ ./internal/handlers/`: form rules, roles, review window, every status code, the
  summary math and the admin routes.
- `TEST_DATABASE_URL=... go test ./internal/store/ -run MentorReview`: insert, duplicate, no team, no mentor,
  mentor change, table checks, the admin queries and who is expected. Use a scratch database; the outbox test renames a table while it runs.
