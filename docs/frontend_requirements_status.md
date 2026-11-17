# Status of the frontend's backend requirements

The frontend repo keeps its requests to the backend in `backend_requirements/`. Where each one stands on `dev`:

| File | Status |
| --- | --- |
| `BACKEND_REQ_WEEK_FEEDBACK.md` | **Done.** Week grade with required feedback, `graded_at` and `graded_by` (`docs/week_grade_feedback.md`). |
| `BACKEND_REQ_PROFILE.md` | **Done.** Profile, avatar, preferences, `avatar_url` on people (its checklist in the frontend repo was never ticked). |
| `BACKEND_INTEGRATION_CHECK.md` | **Done.** Bugs and missing pieces fixed (`docs/integration_fixes.md`, `docs/announcements.md`). |
| `backend-fixes.patch` | **Not needed.** The three bugs it fixed (task deadlines, link `url`, `.md`/`.zip` uploads) are fixed on `dev`. |
| `BACKEND_REQ_MENTOR_QUIZ_RESULTS.md` | **Done now:** `GET /teams/me/quiz-attempts` (below). |
| `BACKEND_REQUIREMENTS.md` | Required parts **done** (see below); the optional ones are listed at the end. |

## Built for these requirements

### `GET /teams/me/quiz-attempts?module_id=`

Every quiz attempt of the students in the caller's team, oldest first, in the same shape as a quiz's `attempt`:

```json
[ { "id": 3, "quiz_id": 7, "student_id": 101, "answers": [1, 0, 2], "score": 2, "total": 3,
    "submitted_at": "2026-11-12T10:15:00Z" } ]
```

- Mentor: their own team only (`[]` without a team). Admin: every student. Anyone else: `403`.
- `module_id` (optional) keeps one week's quizzes; a bad value gives `400`.
- A student who has not taken a quiz has no row; attempts of deleted quizzes are left out.

### A lecturer's rights follow the week

Changing a content item (`PATCH /content-items/{id}`) now needs the same right as adding or deleting one: an admin, or
a lecturer who teaches the week now (enrolled in it as a lecturer). Before, the item's author kept the right after the
week was given to another lecturer.

### Enrollments say who is enrolled

`GET /modules/{id}/enrollments` rows now carry `name`, `email` and `roles` (current roles only). An enrolled user with
the `lecturer` role teaches the week, so the frontend no longer needs `GET /users` to find a week's lecturers. A
student still gets only their own row.

## `BACKEND_REQUIREMENTS.md`, item by item

| Item | Status |
| --- | --- |
| Weekly mentor review: `GET /mentor-reviews/me`, `POST /mentor-reviews`, table, admin routes | Done (`docs/mentor_reviews.md`) |
| Change my own password (`POST /auth/password-reset`) | Done |
| Assign lecturers to weeks through enrollments; an enrolled lecturer is the week's teacher | Done (`IsUserTeacherOfModule`) |
| Only the week's lecturers (and admins) change its module, content, events, quizzes, roster and feedback | Done, now also for changing an existing content item |
| Lecturer rights end with their role (`expires_at`) | Done |
| Enrollments with `name` and role ("nice to have") | Done now |
| Quizzes: the week's lecturer manages them | Done |
| Quizzes: mentors and every lecturer can read a week's quizzes | Done |
| Quizzes: attempts only by students, once (`409`), not before the week opens, after it closes or after `due_at` | Done |

Still open, all marked optional in the request (say if you want any of them):

- **Lock** (423 `mentor_review_required` for a student who owes last week's review). The frontend locks the pages already.
- **Saturday reminder** notification for the mentor review (needs a scheduled job).
- **Admins creating quizzes** (today only lecturers can; the frontend hides the tools from admins). A decision, not a bug.
- **One-call module import** `POST /modules/import` (the frontend's one-by-one import works).
- `GET /modules?lecturer=me`, `lecturer_id` on modules, `module_ids` per lecturer on `GET /users`, and a notification
  when a week is given to a lecturer.
