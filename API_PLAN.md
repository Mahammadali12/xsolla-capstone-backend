# Xsolla School LMS — Backend plan (OpenAPI-style contract)

One school, tens of users, one backend process. No rate limiting, no read replicas, no microservice splits. Every endpoint is one SQL call, or one transaction where noted.

## Auth decision (made today)

JWT with refresh tokens, reusing the checkout-api pattern. Endpoints: `/auth/signup`, `/auth/login`, `/auth/refresh`, `/auth/logout`. Auth user id flows into every request via the JWT subject. No CSRF needed for stateless JWT.

## Schema decisions

**Extensions**
- `CREATE EXTENSION citext;` — case-insensitive, unique email. No lowercasing needed before write.

**USERS**
- `id bigint PK`, `email citext UK`, `name text`, `role text CHECK role IN ('student','teacher','admin')`, `password_hash text NOT NULL`, `created_at timestamptz`
- Signup defaults role to `student`. First registered user becomes `admin` (bootstrap); after that only an admin can create teacher accounts. No role-change endpoint for capstone.

**COURSES**
- `id bigint PK`, `title text`, `description text`, `created_by bigint FK`, `created_at timestamptz`, `deleted_at timestamptz NULL` (soft delete)

**ENROLLMENTS**
- `id bigint PK`, `course_id bigint FK` (index, CASCADE with course), `user_id bigint FK` (index, CASCADE with user), `created_at timestamptz`
- `UNIQUE (course_id, user_id)` — no double enrolls, no app-level check-then-insert race

**CONTENT_ITEMS** (sections / files / assignments, nested by `parent_id`)
- `id bigint PK`, `course_id bigint FK` (index, CASCADE), `parent_id bigint FK` (index, nullable; root = course root), `type text CHECK type IN ('section','file','assignment')`, `title text`, `description text`, `position int`, `is_visible bool DEFAULT true`, `file_id bigint FK` (index, nullable), `deleted_at timestamptz NULL`, `created_at timestamptz`
- `CHECK (type = 'file' AND file_id IS NOT NULL) OR (type != 'file' AND file_id IS NULL)` — both directions
- `parent_id` must point to a `section` (or be null for course root), same course. PATCH on `parent_id` rejects cycles.
- `UNIQUE (course_id, parent_id, position)` — no duplicate positions under the same parent

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

**Grade bound**: `CHECK (grade IS NULL OR grade <= (SELECT max_points FROM assignments WHERE content_item_id = submission.assignment_id))` — enforced in application code (cross-table check not possible in Postgres CHECK).

## Permission matrix

| Endpoint | student | teacher | admin | Condition |
|---|---|---|---|---|
| POST /auth/signup | any | any | any | — |
| POST /auth/login | any | any | any | — |
| POST /auth/logout | any | any | any | — |
| GET /users/me | self | self | self | — |
| GET /users?search= | — | — | any | admin only |
| POST /courses | — | create + auto-enroll as teacher | create | — |
| GET /courses | enrolled | enrolled / teacher | any | — |
| GET /courses/:id | if enrolled | if enrolled / teacher | any | 403 otherwise |
| PATCH /courses/:id | — | if created_by | if created_by | — |
| DELETE /courses/:id | — | if created_by | any | soft delete; collect storage keys first |
| POST /courses/:id/enrollments | self only | any user | any | 409 duplicate |
| DELETE /courses/:id/enrollments/:userId | self only | any | any | — |
| GET /courses/:id/enrollments | if enrolled | if enrolled / teacher | any | — |
| GET /courses/:id/content-items | if enrolled | if enrolled / teacher | any | is_visible filtered server-side |
| POST /courses/:id/content-items | — | if enrolled / teacher | any | parent rules enforced |
| GET /content-items/:id | if enrolled + visible | if enrolled / teacher | any | 403/404 |
| PATCH /content-items/:id | — | if enrolled / teacher | any | cycle check on parent_id |
| DELETE /content-items/:id | — | if enrolled / teacher | any | soft delete |
| POST /files | any authenticated | any authenticated | any | size + type allowlist |
| GET /files/:id | if accessible via link | if accessible via link | any | — |
| GET /files/:id/download | if accessible via link | if accessible via link | any | link rule below |
| DELETE /files/:id | if uploaded_by | if uploaded_by | any | refuse if referenced |
| GET /courses/:id/assignments | if enrolled | if enrolled / teacher | any | alias of content-items type=assignment |
| POST /assignments/:assignmentId/submissions | self only | — | — | single attempt; file ownership check |
| GET /assignments/:assignmentId/submissions/me | self only | — | — | own submission |
| GET /courses/:id/submissions | if enrolled (own only) | if enrolled / teacher (all roster) | any | LEFT JOIN enrollments — missing work shown |
| POST /submissions/:id/grade | — | if enrolled as teacher | any | one line: teacher/admin AND enrolled in course |
| GET /submissions/:id/files | if owns submission | if may grade | any | — |
| GET /users/me/deadlines | any | any | any | upcoming assignments across enrolled courses |
| GET /courses/:id/gradebook | if enrolled (own row) | if enrolled / teacher | any | all students × assignments |

**File read rule** — a file is readable if:
1. it's linked to a visible, non-deleted content item in a course you're enrolled in, **or**
2. it's linked to a submission you own, **or**
3. you may grade that submission (teacher/admin enrolled in the course).

A teacher cannot download a random student's upload unless it's attached to a submission they're grading.

**Grading rule** — `POST /submissions/:id/grade`: caller must be `teacher` or `admin` **AND** enrolled in the course that owns that submission's assignment. Query joins `submissions → assignments → content_items → courses → enrollments` and checks the caller's enrollment role for that course. "Is a teacher" is not enough; must be "teaches this course."

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
- Needed so admins/teachers can look up `user_id` for enrollment

### Courses

**POST /courses**
- Body: `{ title, description }`
- Teacher/admin creates; auto-enrolls creator as `teacher`
- Returns: course row

**GET /courses**
- Student: courses they're enrolled in. Teacher: courses they teach + are enrolled in. Admin: all non-deleted courses.
- One joined query

**GET /courses/:id**
- 404 if missing/soft-deleted. 403 if not enrolled and not admin/teacher of course

**PATCH /courses/:id**
- Body: `{ title?, description? }`
- Only `created_by` or admin

**DELETE /courses/:id**
- Soft delete (`deleted_at`). Before DB delete, collect `storage_key`s of all linked files, delete from storage, then soft-delete course (CASCADE handles enrollments/content/assignments/submissions in DB)

### Enrollments

**POST /courses/:courseId/enrollments**
- Body: `{ user_id }`
- Student: self only. Teacher/admin: any user.
- 409 on duplicate (`UNIQUE course_id, user_id`)

**DELETE /courses/:courseId/enrollments/:userId**
- Student: self only. Teacher/admin: any. Returns 204

**GET /courses/:courseId/enrollments**
- Members with user info. One join query.

### Content items (sections, files, assignments — single route set)

**GET /courses/:courseId/content-items**
- Returns nested tree. Root items have `parent_id = null`.
- Server-side filter: `is_visible = true` and `deleted_at IS NULL` for students/teachers; admin sees all.
- One query for all items of course; tree built in memory (O(n), n small)
- Response shape:
  ```json
  [
    {
      "id": "...",
      "type": "section",
      "title": "...",
      "position": 1,
      "is_visible": true,
      "children": [ { "id": "...", "type": "file", ... } ]
    }
  ]
  ```

**POST /courses/:courseId/content-items**
- Body: `{ parent_id?, type, title, description?, position?, is_visible?, file_id?, due_at?, max_points? }`
- `parent_id` must be a section in same course (or null); cycle rejected
- `type = 'file'` requires `file_id`; `type = 'assignment'` inserts ASSIGNMENTS row in same transaction (`due_at`, `max_points`)
- `position` defaults to last+1 under parent
- Returns: created item (+ assignment fields if type=assignment)

**GET /content-items/:id**
- Returns item + `due_at, max_points` if assignment (one join)
- 403/404 if not visible and not authorized

**PATCH /content-items/:id**
- Body: any updatable field
- Cycle check on `parent_id`; course membership check
- Teacher/admin of course, or `created_by`

**DELETE /content-items/:id**
- Soft delete. CASCADE soft-deletes children and ASSIGNMENTS row. Submissions stay (graded work preserved).

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

**POST /courses/:courseId/content-items/:assignmentId/submissions** (alias: POST /content-items/:id/submissions when type=assignment)
- Body: `{ text_body?, file_ids: [...] }`
- Student id = auth user. `UNIQUE (assignment_id, student_id)` — resubmit updates same row
- File ownership: student may only attach `file_ids` they uploaded
- Resubmit behavior: replace `SUBMISSION_FILES` rows (delete old, insert new); old files become storage orphans (documented ceiling — cleanup job is the upgrade path)
- Grade/feedback are NOT reset on resubmit
- Allowed only while ungraded and before `due_at` (single-attempt decision)
- Returns: submission row

**GET /content-items/:assignmentId/submissions/me**
- Own submission: text, grade, feedback, files

**GET /courses/:courseId/content-items/:assignmentId/submissions** (teacher/admin)
- Full enrolled roster: `enrollments LEFT JOIN submissions` — one row per enrolled student, missing work shown as null
- Teacher must be enrolled in the course

**POST /submissions/:id/grade**
- Body: `{ grade, feedback }`
- Rule: caller is `teacher` or `admin` AND enrolled in the course owning the submission
- Sets `graded_by = auth user`, `graded_at = now`, `grade CHECK 0 <= grade <= max_points` (enforced in app code)

**GET /submissions/:id/files**
- List of files attached to this submission. Access: owner or grader.

### Deadlines (requirement 2)

**GET /users/me/deadlines**
- Upcoming assignments across all enrolled courses, sorted by `due_at`, with submission status (submitted? grade?)
- One joined query — no query-per-course loop

### Gradebook

**GET /courses/:id/gradebook**
- Teacher/admin of course. All enrolled students × all assignments with submission/grade.
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
- Storage backend — local disk for capstone (disappears on container restart; documented ceiling). S3-like later is one config change.

## Framework decision pending

Backend language/framework not chosen yet (Go fits existing `.gitignore`). Route list and contract above are framework-agnostic.
