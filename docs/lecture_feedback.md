# Lecture feedback

After each lecture (an event with `kind: "lecture"`) students fill a short form about it. **Only admins read the
answers**; lecturers, mentors and students cannot. One form per student and lecture.

Code: `internal/handlers/lecture_feedback.go`, `internal/service/lecture_feedback_service.go`,
`internal/store/lecture_feedback_store.go`, migration `024_lecture_feedback`.

## The form

`GET /lecture-feedback/form` (anyone signed in) returns the questions in order, so the front-end can draw the form
from the server. The `id` of each question is its field in `POST /lecture-feedback`.

| # | `id` | Question | Answer | Required |
|---|---|---|---|---|
| 1 | `overall` | How would you rate this lecture overall? | 1 (Very bad) to 5 (Very good) | yes |
| 2 | `clarity` | How clear were the explanations? | 1 (Very unclear) to 5 (Very clear) | yes |
| 3 | `practice` | How useful were the examples, live coding and exercises? | 1 (Not useful) to 5 (Very useful) | yes |
| 4 | `pace` | How was the pace of the lecture? | `too_slow`, `just_right`, `too_fast` | yes |
| 5 | `confidence` | How confident do you feel about this topic now, including your ability to complete the home task? | 1 (Not confident at all) to 5 (Very confident) | yes |
| 6 | `liked` | What worked well? What should the lecturer keep doing? | text, up to 1000 characters | no |
| 7 | `improve` | What was unclear, or what could be improved? | text, up to 1000 characters | no |

The answer also has `opens_minutes_before_end: 10` and `open_days_after_end: 7`.

## When and who

- **Open** from 10 minutes before the lecture ends (so it can be filled in class) until 7 days after it ends.
  Before: `422` "not open yet: it opens on Thursday 12 November 2026 at 20:50 (Baku time)"; after: `422` "closed".
- **Only students** whose only role is `student` (`403` for staff, also with an extra student role).
- A lecture of a week (`module_id`) is only for students enrolled in that week (`404` otherwise). A lecture without
  a week is for every student.
- Deleted events and events that are not lectures answer `404`.

## Student routes

### GET /lecture-feedback/pending

The lectures whose form is open for the caller right now and not yet sent: the "rate this lecture" section.

```json
[ { "event_id": 41, "title": "Goroutines", "starts_at": "2026-11-12T15:00:00Z", "ends_at": "2026-11-12T17:00:00Z",
    "module_id": 3, "module_title": "Week 3: Concurrency", "closes_at": "2026-11-19T17:00:00Z" } ]
```

### GET /lecture-feedback/me

The lectures the caller has given feedback on: `[ { "event_id": 41, "submitted_at": "..." } ]`, `[]` when none.

### POST /lecture-feedback

```json
{ "event_id": 41, "overall": 4, "clarity": 5, "practice": 3, "pace": "too_fast", "confidence": 2,
  "liked": "The live coding", "improve": "A bit slower please" }
```

| Status | `error` | When |
|---|---|---|
| 201 | | stored: `{ "event_id": 41, "submitted_at": "..." }` |
| 400 | `bad_request` | the body is not one JSON object |
| 403 | `forbidden` | not only a student |
| 404 | `not_found` | unknown, deleted or not a lecture; or a lecture of a week the student is not in |
| 409 | `already_sent` | already sent for this lecture (checked before the window, so a re-send always gets 409) |
| 413 | `payload_too_large` | body over 16 KB |
| 422 | `validation_failed` | a rule is broken (every broken field in `details`), a value of the wrong type, or the form is not open / closed |

Text is trimmed; empty text counts as not answered. Forms cannot be changed or deleted.

## Admin routes

Both take the same optional filters: `event_id`, `module_id`, and `from` / `to` as days (`YYYY-MM-DD`, Baku time,
both days included) on the lecture's start. A broken value is `400` naming the parameter.

### GET /lecture-feedback

Every form with all answers, by lecture start then student name.

```json
[ { "id": 7, "lecture": { "event_id": 41, "title": "Goroutines", "starts_at": "...", "ends_at": "...",
      "module_id": 3, "module_title": "Week 3: Concurrency" },
    "lecturers": [ { "id": 20, "name": "Leyla", "email": "leyla@example.com" } ],
    "student": { "id": 31, "name": "Anna", "email": "anna@example.com" },
    "overall": 4, "clarity": 5, "practice": 3, "pace": "too_fast", "confidence": 2,
    "liked": "The live coding", "improve": "A bit slower please", "submitted_at": "..." } ]
```

`lecturers` are the lecturers assigned to the lecture's week (enrolled with an active lecturer role); events have no
"created by", so this is how a lecture is tied to its lecturer. `[]` for a lecture without a week.

### GET /lecture-feedback/summary

One entry per lecture in the filter, also lectures nobody answered yet.

```json
[ { "lecture": { "event_id": 41, "title": "Goroutines", "...": "..." }, "lecturers": [ ... ],
    "responses": 14, "expected": 18,
    "averages": { "overall": 4.21, "clarity": 4.07, "practice": 3.86, "confidence": 3.5 },
    "pace": { "too_slow": 1, "just_right": 9, "too_fast": 4 }, "with_comments": 6 } ]
```

- `expected`: students who may answer, as enrolments are today (active, student role, no staff role; enrolled in the
  week when the lecture has one, otherwise every student).
- `averages`: two decimals, `null` without responses. `with_comments`: answers with at least one text answer.

## Tests

- `go test ./internal/service/ ./internal/handlers/`: the form, the rules, roles, the window edges, every status
  code, filters, the summary math.
- `TEST_DATABASE_URL=... go test ./internal/store/ -run LectureFeedback`: the queries (lecture lookup, duplicates,
  open forms, who is expected, lecturers of a week). Use a scratch database.
