# Xsolla School LMS — API Contract

Single source of truth for request/response payloads between the Go backend (this repo) and the React frontend (separate repo). The frontend consumes this API over HTTP; both repos should treat this document as the shared spec.

Base URL: configured via frontend env `VITE_API_URL` (e.g. `http://localhost:8080` in dev). All timestamps ISO 8601 (`2026-11-04T13:45:00Z`).

**CORS** — The backend sets `Access-Control-Allow-Origin` to the frontend origin (backend env `CORS_ORIGIN`). The frontend sends `Authorization: Bearer <token>` (not cookies), so credentials are not required. Do not use `*` in production; list the frontend domain explicitly.

Every error response has this shape:

```json
{ "error": "code", "message": "human readable", "status": 400, "details": [...] }
```

- `error` — short error code (e.g. `bad_request`, `invalid_credentials`, `conflict`, `not_found`, `unauthorized`, `forbidden`, `server_error`, `validation_failed`)
- `message` — human-readable description
- `status` — HTTP status code as an integer
- `details` — optional array of `{ field, message }` for field-level validation errors (omitted when empty)

Error codes: `400` bad body, `401` invalid credentials, `403` forbidden, `404` missing, `409` conflict (duplicate), `413` file too large, `415` unsupported file type, `422` validation failed, `500` server error.

Auth header: `Authorization: Bearer <access_token>` on every request except signup/login.

---

## Auth

### POST /auth/signup

Body:
```json
{ "email": "alice@x.com", "name": "Alice", "password": "secret123" }
```

Success 201:
```json
{
  "user": { "id": "1", "email": "alice@x.com", "name": "Alice", "roles": ["student"], "created_at": "2026-11-04T13:45:00Z" },
  "access_token": "<jwt>",
  "refresh_token": "<jwt>"
}
```

Errors: 400 (bad body), 409 (email already exists).

Role defaults to `student`. First registered user becomes `admin`.

---

### POST /auth/login

Body:
```json
{ "login": "alice@x.com", "password": "secret123" }
```

Success 200: same shape as signup.

Errors: 401 (invalid credentials — generic, does not reveal whether email exists).

---

### POST /auth/refresh

Body:
```json
{ "refresh_token": "<jwt>" }
```

Success 200:
```json
{ "access_token": "<jwt>", "refresh_token": "<jwt>" }
```

Errors: 401 (invalid or expired refresh token).

---

### POST /auth/logout

Body:
```json
{ "refresh_token": "<jwt>" }
```
(or send the refresh token in the `Authorization: Bearer` header)

Success 204: empty body.

Errors: 400 (missing token).

---

## Users

### GET /users/me

Auth: required.

Success 200:
```json
{ "id": "1", "email": "alice@x.com", "name": "Alice", "roles": ["student"], "created_at": "2026-11-04T13:45:00Z" }
```

---

### GET /users?search=alice

Auth: admin required.

Query: `search` (substring match on email or name).

Success 200:
```json
[ { "id": "1", "email": "alice@x.com", "name": "Alice", "roles": ["student"] } ]
```

Errors: 403.

Needed by lecturers/admins to look up `user_id` before enrolling someone.

---

## Modules

### POST /modules

Auth: required (lecturer or admin).

Body:
```json
{ "title": "CS101", "description": "Intro to CS" }
```

Success 201:
```json
{ "id": "10", "title": "CS101", "description": "Intro to CS", "created_by": "1", "created_at": "2026-11-04T13:45:00Z" }
```

Lecturer creating a module is auto-enrolled as lecturer of that module.

Errors: 403.

---

### GET /modules

Auth: required.

Success 200:
```json
[ { "id": "10", "title": "CS101", "description": "Intro to CS", "created_by": "1", "created_at": "2026-11-04T13:45:00Z" } ]
```

- Student: enrolled modules only.
- Teacher: enrolled + teaching.
- Admin: all non-deleted modules.

---

### GET /modules/:id

Auth: required.

Success 200:
```json
{ "id": "10", "title": "CS101", "description": "Intro to CS", "created_by": "1", "created_at": "2026-11-04T13:45:00Z" }
```

Errors: 404, 403 (not enrolled / not lecturer of this module).

---

### PATCH /modules/:id

Auth: required (module `created_by` or admin).

Body (partial):
```json
{ "title": "CS101 Updated" }
```

Success 200: updated module row.

Errors: 403, 404.

---

### DELETE /modules/:id

Auth: required (module `created_by` or admin).

Success 204: empty body.

Errors: 403, 404.

Soft delete: sets `deleted_at`. Storage keys of linked files are collected before the DB cascade so files can be removed from disk.

---

## Enrollments

### POST /modules/:moduleId/enrollments

Auth: required.

Body:
```json
{ "user_id": "5" }
```
- Student: `user_id` must equal own id (or omit body — self-enroll).
- Teacher/admin: any `user_id`.

Success 201:
```json
{ "id": "100", "module_id": "10", "user_id": "5", "created_at": "2026-11-04T13:45:00Z" }
```

Errors: 403 (student enrolling another), 409 (already enrolled — `UNIQUE(module_id, user_id)`).

---

### DELETE /modules/:moduleId/enrollments/:userId

Auth: required.

Success 204: empty body.

Errors: 403, 404.

---

### GET /modules/:moduleId/enrollments

Auth: required (enrolled / lecturer / admin of module).

Success 200:
```json
[
  { "id": "100", "module_id": "10", "user_id": "5", "user": { "id": "5", "name": "Bob", "email": "bob@x.com" }, "created_at": "2026-11-04T13:45:00Z" }
]
```

---

## Content Items (files, assignments — one route set)

No separate `/assignments/*` routes. An assignment is `content_items.type = 'assignment'`. A link is `content_items.type = 'link'`.
Section type has been removed — content items are flat (no nesting).

### GET /modules/:moduleId/content-items

Auth: required (enrolled / lecturer / admin of module).

Success 200 — flat list:
```json
[
  {
    "id": "200",
    "type": "file",
    "title": "syllabus.pdf",
    "position": 1,
    "is_visible": true,
    "file_id": "300"
  },
  {
    "id": "202",
    "type": "assignment",
    "title": "Homework 1",
    "position": 2,
    "is_visible": true,
    "due_at": "2026-11-10T23:59:00Z",
    "max_points": 100
  },
  {
    "id": "203",
    "type": "link",
    "title": "External Resource",
    "position": 3,
    "is_visible": true,
    "url": "https://example.com/resource"
  }
]
```

Students/lecturers see only `is_visible=true` and non-deleted items. Admin sees all. `position` gives order.

---

### POST /modules/:moduleId/content-items

Auth: required (lecturer or admin enrolled in module).

Body:
```json
{
  "parent_id": null,
  "type": "assignment",
  "title": "Homework 1",
  "description": "Do the thing",
  "position": 1,
  "is_visible": true,
  "file_id": null,
  "url": null,
  "due_at": "2026-11-10T23:59:00Z",
  "max_points": 100
}
```

Rules:
- `parent_id` must be null (sections removed — no nesting).
- `type = 'file'`: `file_id` required, must reference a file uploaded by the caller.
- `type = 'assignment'`: `due_at` and `max_points` required.
- `type = 'link'`: `url` required.
- `type = 'section'` is not supported (400).
- `position` defaults to last+1.

Success 201: created item row (includes `due_at`, `max_points` if assignment, `url` if link).

Errors: 400 (bad type / missing fields), 403.

---

### GET /content-items/:id

Auth: required.

Success 200: item row plus `due_at` and `max_points` if `type = 'assignment'`.

Errors: 403 (not visible and not authorized), 404.

---

### PATCH /content-items/:id

Auth: required (lecturer/admin of module or item `created_by`).

Body (partial): any of `title`, `description`, `position`, `is_visible`, `file_id`, `url`, `due_at`, `max_points`.

Success 200: updated item row.

Errors: 400, 403, 404.

---

### DELETE /content-items/:id

Auth: required (lecturer/admin of module).

Success 204: empty body.

Errors: 403, 404.

Soft delete: sets `deleted_at`. CASCADE soft-deletes the `assignments` row. Submissions are preserved (graded work stays).

---

## Files

### POST /files

Auth: required (any authenticated user).

Body: `multipart/form-data`, one file field.

Success 201:
```json
{ "id": "300", "original_name": "syllabus.pdf", "storage_key": "users/1/uuid-123.pdf", "mime_type": "application/pdf", "size_bytes": 1024000, "created_at": "2026-11-04T13:45:00Z" }
```

Server-side constraints:
- Max size (e.g. 50 MB → 413).
- Type allowlist: `pdf`, `pptx`, `md`, `zip` → 415 otherwise.
- MIME type derived server-side; client MIME is never trusted.
- `storage_key` generated by backend (`users/<user_id>/<uuid>.<ext>`); client never supplies it.
- `original_name` sanitized before any header/response.

Errors: 400 (no file), 413, 415.

---

### GET /files/:id

Auth: required + link-rule access (see below).

Success 200:
```json
{ "id": "300", "original_name": "syllabus.pdf", "storage_key": "users/1/uuid-123.pdf", "mime_type": "application/pdf", "size_bytes": 1024000, "created_at": "2026-11-04T13:45:00Z" }
```

Errors: 403, 404.

---

### GET /files/:id/download

Auth: required + link-rule access.

Success 200: file stream with `Content-Disposition: attachment`.

Errors: 403, 404.

**File read rule** — a file is readable if:
1. linked to a visible, non-deleted content item in a module you're enrolled in, **or**
2. linked to a submission you own, **or**
3. linked to a submission you may grade (lecturer/admin enrolled in the module).

---

### DELETE /files/:id

Auth: required (file `uploaded_by` or admin).

Success 204: empty body.

Errors: 403, 404, 409 (file still referenced by a content item or submission — hard delete refused).

---

## Submissions

Single attempt per student per assignment. Resubmit updates the same row.

### POST /modules/:moduleId/content-items/:assignmentId/submissions

Auth: required (student enrolled in the module).

Body:
```json
{ "text_body": "Here is my answer", "file_ids": ["300", "301"] }
```

Success 201:
```json
{
  "id": "400",
  "content_item_id": "202",
  "student_id": "1",
  "text_body": "Here is my answer",
  "submitted_at": "2026-11-05T10:00:00Z",
  "grade": null,
  "feedback": null,
  "graded_by": null,
  "graded_at": null,
  "files": [ { "id": "300", "original_name": "syllabus.pdf" } ]
}
```

Rules:
- `student_id` is the authenticated user.
- Multiple attempts are allowed — each POST inserts a new submission row (new `id`, new `submitted_at`).
- File ownership: student may only attach `file_ids` they uploaded.
- Allowed only while ungraded and before `due_at` (after that: 422).

Errors: 400 (file not owned by student), 403, 422 (after deadline or already graded).

---

### GET /content-items/:assignmentId/submissions/me

Auth: required (student).

Success 200: single submission object (same shape as above) or 404 if none submitted.

---

### GET /modules/:moduleId/content-items/:assignmentId/submissions

Auth: required (lecturer or admin enrolled in module).

Success 200 — full enrolled roster via LEFT JOIN (missing work shown):
```json
[
  { "id": "400", "student": { "id": "1", "name": "Alice" }, "text_body": "...", "submitted_at": "2026-11-05T10:00:00Z", "grade": 85, "feedback": "Good job", "files": [...] },
  { "id": null, "student": { "id": "2", "name": "Bob" }, "text_body": null, "submitted_at": null, "grade": null, "feedback": null, "files": null }
]
```

---

### POST /submissions/:id/grade

Auth: required (lecturer/admin enrolled in the module owning this submission's assignment).

Body:
```json
{ "grade": 85, "feedback": "Good job" }
```

Success 200:
```json
{ "id": "400", "grade": 85, "feedback": "Good job", "graded_by": "1", "graded_at": "2026-11-06T09:00:00Z" }
```

Rule: `0 <= grade <= max_points` (enforced in application code).

Errors: 403 (not lecturer of this module), 404, 422 (grade out of bounds).

---

### GET /submissions/:id/files

Auth: required (submission owner or grader).

Success 200:
```json
[ { "id": "300", "original_name": "syllabus.pdf", "download_url": "/files/300/download" } ]
```

---

## Calendar Events

### GET /events?from=&to=

Auth: required.

Query: `from` and `to` as ISO 8601 timestamps defining the date range.

Success 200:
```json
[
  { "id": "100", "title": "Weekly Lecture", "kind": "lecture", "starts_at": "2026-11-09T10:00:00Z", "ends_at": "2026-11-09T12:00:00Z", "all_day": false, "location": "Main hall", "description": "Weekly lecture for the current module.", "module_id": "200" },
  { "id": "101", "title": "Off-campus day", "kind": "off-campus", "starts_at": "2026-11-20T00:00:00Z", "ends_at": "2026-11-20T23:59:00Z", "all_day": true, "description": "No classes - the school is closed." }
]
```

Students see only events from modules that have opened (module lock enforced server-side). Teacher/admin see all events for courses they teach.

Errors: 403.

---

### POST /events

Auth: required (lecturer or admin only).

Body:
```json
{ "title": "Weekly Lecture", "kind": "lecture", "starts_at": "2026-11-09T10:00:00Z", "ends_at": "2026-11-09T12:00:00Z", "location": "Main hall", "description": "...", "module_id": "200" }
```

Rules:
- `kind` must be `lecture`, `meeting`, or `off-campus`.
- `starts_at` must be before `ends_at`.
- `module_id`, when provided, must reference a module the caller teaches.

Success 201: created event row.

Errors: 400 (invalid dates / missing fields), 403 (not lecturer/admin), 404 (module not found).

---

### PATCH /events/:id

Auth: required (lecturer or admin only — creator of the event or admin).

Body (partial): any of `title`, `kind`, `starts_at`, `ends_at`, `all_day`, `location`, `description`, `module_id`.

Success 200: updated event row.

Errors: 400 (invalid dates / cycle), 403, 404.

---

### DELETE /events/:id

Auth: required (lecturer or admin only — creator of the event or admin).

Success 204: empty body.

Errors: 403, 404.

Soft delete: sets `deleted_at`.

---

## Mentor Slots (1:1 meetings)

### GET /mentor-slots

Auth: required.

- Student: sees their own mentor's available slots.
- Teacher/admin: sees their own slots.
- Only future slots (not started yet) are returned.

Success 200:
```json
[
  { "id": "s-1", "mentor_id": "1", "mentor_name": "Mentor Name", "starts_at": "2026-11-12T15:00:00Z", "ends_at": "2026-11-12T15:30:00Z", "booked": false, "booked_by": null },
  { "id": "s-2", "mentor_id": "1", "mentor_name": "Mentor Name", "starts_at": "2026-11-12T15:30:00Z", "ends_at": "2026-11-12T16:00:00Z", "booked": true, "booked_by": { "id": "5", "name": "Student Name" } }
]
```

Students never learn who booked another student's slot — `booked_by` is omitted for bookings they don't own.

Errors: 403.

### POST /mentor-slots

Auth: required (lecturer or admin only).

Body:
```json
{ "date": "2026-11-12", "start": "15:00", "end": "18:00", "slot_minutes": 30 }
```

Rules:
- `date` as `YYYY-MM-DD`, `start`/`end` as `HH:MM` (local time).
- Window must be at least `slot_minutes` long (default 30).
- Window must be in the future.
- Backend cuts the window into back-to-back slots, skipping any that clash with existing slots.

Success 201: array of created slot rows.

Errors: 400 (invalid window / too short / past date), 403.

### DELETE /mentor-slots/:id

Auth: required (lecturer or admin only — owner of the slot).

Success 204: empty body.

Errors: 403, 404, 409 (slot is booked — cancel the booking first).

### POST /mentor-slots/:id/book

Auth: required (student only).

Success 200: updated slot (`booked: true`, `booked_by` set to the student).

Rules:
- Student may have at most one upcoming booking at a time.
- 409 if the slot is already taken, already started, or the student already holds a booking.

Errors: 403, 404, 409.

### DELETE /mentor-slots/:id/book

Auth: required.

- Student: cancels their own booking.
- Teacher/admin: cancels any booking.

Success 204: empty body.

Errors: 403, 404.

---

## Deadlines

### GET /users/me/deadlines

Auth: required.

Success 200:
```json
[
  { "assignment_id": "202", "module_id": "10", "module_title": "CS101", "title": "Homework 1", "due_at": "2026-11-10T23:59:00Z", "submitted": true, "grade": 85 }
]
```

Upcoming assignments across all enrolled modules, sorted by `due_at`, with submission status. One joined query — no query-per-module loop.

---

## Gradebook

### GET /modules/:id/gradebook

Auth: required (lecturer or admin enrolled in module; student sees own row only).

Success 200:
```json
{
  "students": [
    { "user_id": "1", "name": "Alice", "assignments": [ { "assignment_id": "202", "title": "Homework 1", "max_points": 100, "grade": 85, "submitted_at": "2026-11-05T10:00:00Z" } ] }
  ],
  "assignments": [ { "id": "202", "title": "Homework 1", "max_points": 100, "due_at": "2026-11-10T23:59:00Z" } ]
}
```

---

## Frontend Auth Contract (separate repo)

- Configure `VITE_API_URL` to point at the backend (e.g. `http://localhost:8080`).
- Store `access_token` and `refresh_token` (memory or `localStorage` — document the XSS tradeoff).
- Send `Authorization: Bearer <access_token>` on every request.
- On 401: clear tokens, redirect to `/login`.
- On 403: show "not authorized".
- On 409: show conflict message (duplicate enrollment, etc.).
- On 413 / 415: show file error message.
