# Fixes from the frontend's integration check

The frontend's `backend_requirements/BACKEND_INTEGRATION_CHECK.md` (written on 11 Nov against an old `master`) listed
problems. Most were already fixed on `dev`; these were still open and are fixed here.

| # | Problem | Fix |
| --- | --- | --- |
| 1 | Students could read every classmate's week grade from `GET /modules/{id}/enrollments` | A student now gets **only their own** enrollment row. Admins, lecturers and mentors still get every row (mentors need them to find a week's lecturers). |
| 2 | A lecturer could not change or delete an event they made without a week (404) | Events store `created_by` (migration `029_events_created_by`). The creator can always change and delete their own event; the week's teachers and admins can too, as before. Event responses (`POST`, `PATCH`, `GET /events`) include `created_by` (left out for events made before this). |
| 3 | No way to delete one team | `DELETE /teams/{id}` (admin): `204`, `404` if there is no such team. The team's member list goes; the students and the mentor stay; past mentor reviews keep their text (their team link is cleared). |
| 4 | `GET .../submissions/me` returned only the latest hand-in | It now also returns `history`: the earlier hand-ins of that task, newest first, each with its own `files`. |
| 5 | Admin announcements had no backend | `POST /notifications/broadcast` and `GET /notifications/broadcasts` (admin). Delivered as an in-app notification and an email through the notification service. See `announcements.md`. |

Not changed:

- **Lecturer of a week** (`lecturer_id`, check item 2.2): not needed. The frontend assigns lecturers by enrolling them
  in the week (`POST /modules/{id}/enrollments`), and the backend already treats an enrolled lecturer as the week's
  teacher (`IsUserTeacherOfModule`).
- CORS still allows every origin, and the first sign-up on an empty database still becomes admin.
