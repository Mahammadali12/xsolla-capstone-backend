# Xsolla School LMS — Backend plan (OpenAPI-style contract)

One school, tens of users, one backend process. No rate limiting, no read replicas, no microservice splits. Every endpoint is one SQL call, or one transaction where noted.

## Auth decision (made today)

JWT with refresh tokens, reusing the checkout-api pattern. Endpoints: `/auth/signup`, `/auth/login`, `/auth/refresh`, `/auth/logout`. Auth user id flows into every request via the JWT subject. No CSRF needed for stateless JWT.

## Schema decisions

**Extensions**
- `CREATE EXTENSION citext;` — case-insensitive, unique email. No lowercasing needed before write.

**USERS**
- `id bigint PK`, `email citext UK`, `name text`, `roles TEXT[] NOT NULL DEFAULT '{}'`, `password_hash text NOT NULL`, `created_at timestamptz`
- Signup defaults roles to `["student"]`. First registered user becomes `admin` (bootstrap); after that only an admin can create lecturer accounts. No role-change endpoint for capstone.

**MODULES** (renamed from courses — frontend refers to them as modules)
- `id bigint PK`, `title text`, `description text`, `created_by bigint FK`, `created_at timestamptz`, `deleted_at timestamptz NULL` (soft delete)

**ENROLLMENTS**
- `id bigint PK`, `module_id bigint FK` (index, CASCADE with module), `user_id bigint FK` (index, CASCADE with user), `created_at timestamptz`
- `UNIQUE (module_id, user_id)` — no double enrolls, no app-level check-then-insert race

**CONTENT_ITEMS** (files / assignments / links, flat — no sections, no nesting)
- `id bigint PK`, `module_id bigint FK` (index, CASCADE), `parent_id bigint FK` (index, nullable; always null — sections removed), `type text CHECK type IN ('file','assignment','link')`, `title text`, `description text`, `position int`, `is_visible bool DEFAULT true`, `file_id bigint FK` (index, nullable), `url text NULL`, `deleted_at timestamptz NULL`, `created_at timestamptz`
- `CHECK (type = 'file' AND file_id IS NOT NULL) OR (type IN ('assignment','link') AND file_id IS NULL)` — both directions
- `parent_id` is always null (sections removed — no parent-child nesting)
- `UNIQUE (module_id, parent_id, position)` — no duplicate positions under the same parent

**ASSIGNMENTS** (1:1 extension of a content item)
- `content_item_id bigint PK, FK` (CASCADE), `due_at timestamptz`, `max_points int CHECK max_points > 0`, `deleted_at timestamptz NULL` (soft delete)
- Creating an assignment = insert CONTENT_ITEMS row + ASSIGNMENTS row in the same transaction

**FILES**
- `id bigint PK`, `uploaded_by bigint FK` (index), `original_name text`, `storage_key text`, `mime_type text`, `size_bytes bigint`, `deleted_at timestamptz NULL` (soft delete), `created_at timestamptz`

**SUBMISSIONS** (single attempt per student per assignment)
- `id bigint PK`, `assignment_id bigint FK` (index, CASCADE), `student_id bigint FK` (index), `text_body text`, `submitted_at timestamptz`, `grade numeric CHECK grade >= 0`, `feedback text nullable`, `graded_by bigint FK nullable` (index), `graded_at timestamptz nullable`, `deleted_at timestamptz NULL`
- `UNIQUE (assignment_id, student_id)` — resubmit updates the same row (text_body, submitted_at; grade/feedback are NOT reset — only the grader clears them)

**SUBMISSION_FILES**
- `submission_id bigint FK, PK`, `file_id bigint FK, PK` — no duplicate file links on one submission

**EVENTS** (calendar events: lectures, meetings, off-campus days)
- `id bigint PK`, `title text`, `kind text CHECK kind IN ('lecture','meeting','off-campus')`, `starts_at timestamptz`, `ends_at timestamptz`, `all_day bool DEFAULT false`, `location text nullable`, `description text nullable`, `module_id bigint nullable FK REFERENCES modules(id)`, `deleted_at timestamptz NULL`, `created_at timestamptz`

**MENTOR_SLOTS** (1:1 meeting availability + bookings)
- `id bigint PK`, `mentor_id bigint FK REFERENCES users(id)` (index), `starts_at timestamptz`, `ends_at timestamptz`, `booked bool DEFAULT false`, `booked_by bigint nullable FK REFERENCES users(id)` (index), `deleted_at timestamptz NULL`, `created_at timestamptz`
- Constraint: `ends_at > starts_at`

**Grade bound**: `CHECK (grade IS NULL OR grade <= (SELECT max_points FROM assignments WHERE content_item_id = submission.assignment_id))` — enforced in application code (cross-table check not possible in Postgres CHECK).

## Permission matrix

| Endpoint | student | lecturer | admin | Condition |
|---|---|---|---|---|
| POST /auth/signup | any | any | any | — |
| POST /auth/login | any | any | any | — |
| POST /auth/logout | any | any | any | — |
| GET /users/me | self | self | self | — |
| GET /users?search= | — | — | any | admin only |
| POST /modules | — | create + auto-enroll as lecturer | create | — |
| GET /modules | enrolled | enrolled / lecturer | any | — |
| GET /modules/:id | if enrolled | if enrolled / lecturer | any | 403 otherwise |
| PATCH /modules/:id | — | if created_by | if created_by | — |
| DELETE /modules/:id | — | if created_by | any | soft delete; collect storage keys first |
| POST /modules/:id/enrollments | self only | any user | any | 409 duplicate |
| DELETE /modules/:id/enrollments/:userId | self only | any | any | — |
| GET /modules/:id/enrollments | if enrolled | if enrolled / lecturer | any | — |
| GET /modules/:id/content-items | if enrolled | if enrolled / lecturer | any | `is_visible` filtered server-side |
| POST /modules/:id/content-items | — | if enrolled / lecturer | any | parent rules enforced |
| GET /content-items/:id | if enrolled + visible | if enrolled / lecturer | any | 403/404 |
| PATCH /content-items/:id | — | if enrolled / lecturer | any | cycle check on parent_id |
| DELETE /content-items/:id | — | if enrolled / lecturer | any | soft delete |
| POST /files | any authenticated | any authenticated | any | size + type allowlist |
| GET /files/:id | if accessible via link | if accessible via link | any | — |
| GET /files/:id/download | if accessible via link | if accessible via link | any | link rule below |
| DELETE /files/:id | if uploaded_by | if uploaded_by | any | refuse if referenced |
| POST /modules/:id/content-items/:assignmentId/submissions | self only | — | — | single attempt; file ownership check |
| GET /content-items/:assignmentId/submissions/me | self only | — | — | own submission |
| GET /modules/:id/submissions | if enrolled (own only) | if enrolled / lecturer (all roster) | any | LEFT JOIN enrollments — missing work shown |
| POST /submissions/:id/grade | — | if enrolled as mentor | any | one line: mentor/admin AND enrolled in module |
| GET /submissions/:id/files | if owns submission | if may grade | any | — |
| GET /events?from=&to= | if modules open | if enrolled/lecturer | any | date range filter |
| POST /events | — | create | create | lecturer/admin only |
| PATCH /events/:id | — | if created | any | lecturer/admin only |
| DELETE /events/:id | — | if created | any | soft delete |
| GET /mentor-slots | own mentor's slots | own slots | own slots | future slots only |
| POST /mentor-slots | — | open window | open window | mentor creates slots |
| DELETE /mentor-slots/:id | — | own slot | own slot | only if free |
| POST /mentor-slots/:id/book | self only | — | — | 409 if taken or already booked |
| DELETE /mentor-slots/:id/book | cancel own | cancel any | cancel any | — |
| GET /users/me/deadlines | any | any | any | upcoming assignments across enrolled modules |
| GET /modules/:id/gradebook | if enrolled (own row) | if enrolled / lecturer | any | all students × assignments |

**File read rule** — a file is readable if:
1. it's linked to a visible, non-deleted content item in a module you're enrolled in, **or**
2. it's linked to a submission you own, **or**
3. you may grade that submission (lecturer/admin enrolled in the module).

A lecturer cannot download a random student's upload unless it's attached to a submission they're grading.

**Grading rule** — `POST /submissions/:id/grade`: caller must be `mentor` or `admin` **AND** enrolled in the module that owns that submission's assignment. Query joins `submissions → assignments → content_items → modules → enrollments` and checks the caller's enrollment role for that module. "Is a lecturer" is not enough; must be "teaches this module."

## Endpoints

### Auth

**POST /auth/signup**
- Body: `{ email, name, password }`
- Duplicate names allowed; email unique (DB `citext UK` → 409 on violation)
- Password hashed (bcrypt/argon2) into `password_hash`; never stored plaintext
- Role defaults to `student`; first user becomes `admin`
- Returns: `id, email, name, role, created_at` (never password hash)

**POST /auth/login**
- Body: `{ login, password }` — login = email
- Generic 401 "invalid credentials" on mismatch (doesn't reveal email existence)
- Returns: user fields + access token + refresh token

**POST /auth/refresh**
- Body: `{ refresh_token }`
- Returns: new access token + refresh token

**POST /auth/logout**
- Body: `{ refresh_token }` or bearer token
- Revokes token(s); returns 204

### Users

**GET /users/me**
- Returns: `id, email, name, role, created_at`
- Needs: authenticated user id

**GET /users?search=**
- Admin only. Returns users matching email/name substring.
- Needed so admins/lecturers can look up `user_id` for enrollment

### Modules

**POST /modules**
- Body: `{ title, description }`
- Lecturer/admin creates; auto-enrolls creator as `lecturer`
- Returns: module row

**GET /modules**
- Student: modules they're enrolled in. Teacher: modules they teach + are enrolled in. Admin: all non-deleted modules.
- One joined query

**GET /modules/:id**
- 404 if missing/soft-deleted. 403 if not enrolled and not admin/lecturer of module

**PATCH /modules/:id**
- Body: `{ title?, description? }`
- Only `created_by` or admin

**DELETE /modules/:id**
- Soft delete (`deleted_at`). Before DB delete, collect `storage_key`s of all linked files, delete from storage, then soft-delete module (CASCADE handles enrollments/content/assignments/submissions in DB)

### Enrollments

**POST /modules/:moduleId/enrollments**
- Body: `{ user_id }`
- Student: self only. Teacher/admin: any user.
- 409 on duplicate (`UNIQUE module_id, user_id`)

**DELETE /modules/:moduleId/enrollments/:userId**
- Student: self only. Teacher/admin: any. Returns 204

**GET /modules/:moduleId/enrollments**
- Members with user info. One join query.

### Content items (files, assignments — single route set)

**GET /modules/:moduleId/content-items**
- Returns flat list. All items have `parent_id = null` (sections removed).
- Server-side filter: `is_visible = true` and `deleted_at IS NULL` for students/lecturers; admin sees all.
- One query for all items of module; list built in memory (O(n), n small)
- Response shape:
  ```json
  [
    {
      "id": "...",
      "type": "file",
      "title": "...",
      "position": 1,
      "is_visible": true,
      "file_id": "..."
    }
  ]
  ```

**POST /modules/:moduleId/content-items**
- Body: `{ parent_id?, type, title, description?, position?, is_visible?, file_id?, url?, due_at?, max_points? }`
- `parent_id` must be null (sections removed); cycle rejected
- `type = 'file'` requires `file_id`; `type = 'assignment'` inserts ASSIGNMENTS row in same transaction (`due_at`, `max_points`)
- `type = 'link'` requires `url`
- `type = 'section'` returns 400 (not supported)
- `position` defaults to last+1 under parent
- Returns: created item (+ assignment fields if type=assignment)

**GET /content-items/:id**
- Returns item + `due_at, max_points` if assignment (one join)
- 403/404 if not visible and not authorized

**PATCH /content-items/:id**
- Body: any updatable field
- Course membership check
- Teacher/admin of module, or `created_by`

**DELETE /content-items/:id**
- Soft delete. CASCADE soft-deletes ASSIGNMENTS row. Submissions stay (graded work preserved).

### Files

**POST /files**
- Body: multipart file
- Constraints: size cap (e.g. 50 MB → 413), type allowlist (pdf, pptx, md, zip → 415), MIME derived server-side not trusted from client
- `storage_key` generated by backend (e.g. `users/<id>/<uuid>.<ext>`), never from client
- `original_name` sanitized before any header/response
- Returns: `id, storage_key, original_name, mime_type, size_bytes, created_at`

**GET /files/:id**
- Metadata only. Link-rule access check.

**GET /files/:id/download**
- File stream or signed URL. Link-rule access check.

**DELETE /files/:id**
- Uploader only (or admin). Refuse if referenced by a content item or submission (soft delete preferred to preserve grading history). Hard delete only when unreferenced.

### Submissions (merged — no separate /assignments route set)

**POST /modules/:moduleId/content-items/:assignmentId/submissions** (alias: POST /content-items/:id/submissions when type=assignment)
- Body: `{ text_body?, file_ids: [...] }`
- Student id = auth user. `UNIQUE (assignment_id, student_id)` — resubmit updates same row
- File ownership: student may only attach `file_ids` they uploaded
- Resubmit behavior: replace `SUBMISSION_FILES` rows (delete old, insert new); old files become storage orphans (documented ceiling — cleanup job is the upgrade path)
- Grade/feedback are NOT reset on resubmit
- Allowed only while ungraded and before `due_at` (single-attempt decision)
- Returns: submission row

**GET /content-items/:assignmentId/submissions/me**
- Own submission: text, grade, feedback, files

**GET /modules/:moduleId/content-items/:assignmentId/submissions** (lecturer/admin)
- Full enrolled roster: `enrollments LEFT JOIN submissions` — one row per enrolled student, missing work shown as null
- Teacher must be enrolled in the module

**POST /submissions/:id/grade**
- Body: `{ grade, feedback }`
- Rule: caller is `lecturer` or `admin` AND enrolled in the module owning the submission
- Sets `graded_by = auth user`, `graded_at = now`, `grade CHECK 0 <= grade <= max_points` (enforced in app code)

**GET /submissions/:id/files**
- List of files attached to this submission. Access: owner or grader.

### Calendar Events

**GET /events?from=&to=**
- Returns events in date range. Students see only events from open modules (module lock enforced server-side).
- One query with date range filter.

**POST /events** (lecturer/admin only)
- Body: `{ title, kind, starts_at, ends_at, all_day?, location?, description?, module_id? }`
- Creates a calendar event. `kind` in `lecture | meeting | off-campus`.

**PATCH /events/:id** (lecturer/admin only)
- Body: partial event fields
- Updates event. Creator or admin.

**DELETE /events/:id** (lecturer/admin only)
- Soft delete.

### Mentor Slots (1:1 meetings)

**GET /mentor-slots**
- Student: own mentor's slots. Teacher/admin: own slots. Future slots only.

**POST /mentor-slots** (lecturer/admin only)
- Body: `{ date, start, end, slot_minutes? }` — date as `YYYY-MM-DD`, times as `HH:MM`.
- Backend cuts window into back-to-back slots (default 30 min), skipping clashes with existing slots.

**DELETE /mentor-slots/:id** (lecturer/admin only)
- Only if free (not booked). Otherwise 409.

**POST /mentor-slots/:id/book** (student only)
- Locks the slot. 409 if taken or student already has an upcoming booking.

**DELETE /mentor-slots/:id/book**
- Student cancels own booking; lecturer/admin cancels any booking.

### Deadlines (requirement 2)

**GET /users/me/deadlines**
- Upcoming assignments across all enrolled modules, sorted by `due_at`, with submission status (submitted? grade?)
- One joined query — no query-per-module loop

### Gradebook

**GET /modules/:id/gradebook**
- Teacher/admin of module. All enrolled students × all assignments with submission/grade.
- Student: own row only.

## Errors

Standard JSON body on every endpoint:
```json
{ "error": "code", "message": "human readable", "status": 400 }
```

Codes: 400 bad body, 401 invalid credentials, 403 forbidden, 404 missing, 409 duplicate/conflict, 413 file too large, 415 unsupported file type, 422 validation failed, 500 server error.

## Not in scope yet (deliberate)

- PDF preview (future feature 1.1)
- Interactive game — nothing in ER covers it; no endpoints until defined
- Multiple submission attempts — single attempt documented above; revisit after capstone
- Storage backend — S3-compatible (Supabase) via interface-based `Storage` layer; local disk removed
- Section nesting — content items are flat (files/assignments only); sections removed

## Framework decision pending

Backend language/framework not chosen yet (Go fits existing `.gitignore`). Route list and contract above are framework-agnostic.
