# Week grade feedback

A mentor grades each student's week (a module) as `red`, `yellow` or `green`. Every grade now comes with the mentor's
**module feedback**: a few words on the week. It is stored on the enrollment, next to the grade.

## Data

Migration `025_enrollment_grade_feedback` adds two columns to `enrollments`:

| Column | Type | Meaning |
|---|---|---|
| `grade_feedback` | `text NULL`, 1 to 1000 characters | the mentor's feedback that came with the grade |
| `graded_at` | `timestamptz NULL` | when the grade was last written |

Both are `NULL` for a week graded before this existed.

## Giving a grade

`PATCH /modules/{moduleId}/enrollments/{userId}/grade` (the student's mentor, or an admin)

```json
{ "grade": "yellow", "feedback": "Routing app was late. Please tell me early if you are stuck." }
```

- `feedback` is required. It is trimmed, then must be 1 to 1000 characters (letters, not bytes).
  - Missing, empty or only spaces: `422 validation_failed`, message `feedback is required`, `details: [{ "field": "feedback" }]`.
  - Longer than 1000: `422 validation_failed`, message `feedback must be at most 1000 characters`.
- A new grade replaces the old grade **and** the old feedback, and sets `graded_at` to now.
- The rules that were already there stay the same: a bad colour gives `422`, someone who is not the student's mentor
  (or an admin) gets `403`, and a student who is not enrolled in the week gives `404`.

The answer is the enrollment, with the two new fields:

```json
{ "id": 12, "module_id": 4, "user_id": 31, "grade": "yellow",
  "feedback": "Routing app was late. Please tell me early if you are stuck.",
  "graded_at": "2026-11-16T10:12:00Z", "created_at": "2026-11-01T00:00:00Z" }
```

## Reading it

`GET /modules/{moduleId}/enrollments` returns `feedback` and `graded_at` on each enrollment.

- Staff (admin, mentor, lecturer) see every student's feedback.
- A student gets **only their own** enrollment (grade and feedback); other students' rows are not sent.
- `feedback`, `graded_at` and `grade` are left out of the JSON when they are not set.
