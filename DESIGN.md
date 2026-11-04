# Xsolla School LMS — Design Document (Backend)

Shared contract so the Go backend and the React frontend (separate repo) build against the same API.

## 1. Stack

| Layer | Choice | Notes |
|---|---|---|
| Backend | Go, stdlib `net/http` + `database/sql` + `pgx`/`lib/pq` | This repo |
| Frontend | React + Vite, TypeScript, plain `fetch` | Separate repo; consumes this API |
| Database | PostgreSQL | Extension: `citext` |
| File storage | Local disk (`/data/uploads/`) | One directory per user; container restart loses files — documented ceiling |
| Auth | JWT access token + refresh token | Reuse checkout-api pattern; stateless, Bearer header |

**Load target:** one school, tens of users, one backend process. Nothing here needs horizontal scaling yet.

---

## 2. Repository layout

```
xsolla-capstone/              # Backend repo (this repo)
├── cmd/server/               # main.go
├── internal/
│   ├── auth/                 # JWT, password hashing, middleware
│   ├── db/                   # migrations, queries
│   ├── handlers/             # one file per resource
│   ├── middleware/           # auth, role, enrollment checks, CORS
│   └── storage/              # local disk file store
├── migrations/               # .sql files
├── go.mod / go.sum
├── API_PLAN.md               # endpoint plan
├── DESIGN.md                 # this document
├── API_CONTRACT.md           # request/response payload contract
├── .gitignore                # exists
└── README.md                 # local dev setup

Frontend repo (separate, not in this tree)
├── src/
│   ├── api/                  # fetch wrappers per endpoint (base URL from VITE_API_URL)
│   ├── components/           # shared UI
│   ├── pages/                # Login, CourseTree, AssignmentView, Gradebook, etc.
│   ├── context/              # auth context (tokens, logout)
│   └── App.tsx / main.tsx
└── package.json
```

---

## 3. Database

Extensions: `CREATE EXTENSION citext;`

**users**
- `id bigint PK`, `email citext UK`, `name text`, `role text CHECK role IN ('student','teacher','admin')`, `password_hash text NOT NULL`, `created_at timestamptz`
- Signup defaults role to `student`. First registered user becomes `admin` (bootstrap). No role-change endpoint for capstone.

**courses**
- `id bigint PK`, `title text`, `description text`, `created_by bigint FK`, `created_at timestamptz`, `deleted_at timestamptz NULL` (soft delete)

**enrollments**
- `id bigint PK`, `course_id bigint FK` (index, CASCADE), `user_id bigint FK` (index, CASCADE), `created_at timestamptz`
- `UNIQUE (course_id, user_id)` — no double enrolls, no app-level check-then-insert race

**content_items** (sections / files / assignments, nested by `parent_id`)
- `id bigint PK`, `course_id bigint FK` (index, CASCADE), `parent_id bigint FK` (index, nullable; null = course root), `type text CHECK type IN ('section','file','assignment')`, `title text`, `description text`, `position int`, `is_visible bool DEFAULT true`, `file_id bigint FK` (index, nullable), `deleted_at timestamptz NULL`, `created_at timestamptz`
- `CHECK (type = 'file' AND file_id IS NOT NULL) OR (type != 'file' AND file_id IS NULL)`
- `parent_id` must point to a `section` in the same course (or null for root); PATCH on `parent_id` rejects cycles
- `UNIQUE (course_id, parent_id, position)`

**assignments** (1:1 extension of a content item)
- `content_item_id bigint PK, FK` (CASCADE), `due_at timestamptz`, `max_points int CHECK max_points > 0`, `deleted_at timestamptz NULL` (soft delete)
- Creating an assignment = insert content_items row + assignments row in the same transaction

**files**
- `id bigint PK`, `uploaded_by bigint FK` (index), `original_name text`, `storage_key text`, `mime_type text`, `size_bytes bigint`, `deleted_at timestamptz NULL` (soft delete), `created_at timestamptz`

**submissions** (single attempt per student per assignment)
- `id bigint PK`, `assignment_id bigint FK` (index, CASCADE), `student_id bigint FK` (index), `text_body text`, `submitted_at timestamptz`, `grade numeric CHECK grade >= 0`, `feedback text nullable`, `graded_by bigint FK nullable` (index), `graded_at timestamptz nullable`, `deleted_at timestamptz NULL`
- `UNIQUE (assignment_id, student_id)` — resubmit updates the same row; grade/feedback are NOT reset (only the grader clears them)
- `grade <= max_points` enforced in application code

**submission_files**
- `submission_id bigint FK, PK`, `file_id bigint FK, PK`

Indexes on every FK column used for filtering: `enrollments(course_id, user_id)`, `content_items(course_id, parent_id, file_id)`, `files(uploaded_by)`, `submissions(assignment_id, student_id, graded_by)`, `submission_files(submission_id, file_id)`.

---

## 4. Auth flow

1. `POST /auth/signup` `{email, name, password}` → bcrypt/argon2 hash into `password_hash`, insert as `student`, return access + refresh token.
2. `POST /auth/login` `{login, password}` → lookup by email (`citext`), compare hash, generic 401 on mismatch (doesn't reveal email existence).
3. `POST /auth/refresh` → rotate refresh token, return new access token.
4. `POST /auth/logout` → revoke refresh token, 204.

Middleware extracts JWT from `Authorization: Bearer <token>`, verifies, puts `user_id` and `role` in request context. Every state-changing endpoint and every data endpoint checks enrollment where required.

Role bootstrap: first registered user = `admin`. After that, no self-service promotion. An admin must create teacher accounts (or one-time CLI seed).

---

## 5. Permission matrix

| Endpoint | student | teacher | admin | Condition |
|---|---|---|---|---|
| POST /auth/signup | any | any | any | — |
| POST /auth/login | any | any | any | — |
| POST /auth/logout / refresh | any | any | any | — |
| GET /users/me | self | self | self | — |
| GET /users?search= | — | — | any | admin only |
| POST /courses | — | create + auto-enroll as teacher | create | — |
| GET /courses | enrolled | enrolled / teacher | any | — |
| GET /courses/:id | if enrolled | if enrolled / teacher | any | else 403 |
| PATCH /courses/:id | — | if created_by | if created_by | — |
| DELETE /courses/:id | — | if created_by | any | soft delete; collect storage keys first |
| POST /courses/:id/enrollments | self only | any user | any | 409 duplicate |
| DELETE /courses/:id/enrollments/:userId | self only | any | any | — |
| GET /courses/:id/enrollments | if enrolled | if enrolled / teacher | any | — |
| GET /courses/:id/content-items | if enrolled | if enrolled / teacher | any | `is_visible` filtered server-side |
| POST /courses/:id/content-items | — | if enrolled / teacher | any | parent rules enforced |
| GET /content-items/:id | if enrolled + visible | if enrolled / teacher | any | else 403/404 |
| PATCH /content-items/:id | — | if enrolled / teacher | any | cycle check on parent_id |
| DELETE /content-items/:id | — | if enrolled / teacher | any | soft delete |
| POST /files | any authenticated | any authenticated | any | size + type allowlist |
| GET /files/:id | if accessible via link | if accessible via link | any | — |
| GET /files/:id/download | if accessible via link | if accessible via link | any | link rule below |
| DELETE /files/:id | if uploaded_by | if uploaded_by | any | refuse if referenced |
| POST /content-items/:id/submissions | self only | — | — | single attempt; file ownership check |
| GET /content-items/:id/submissions/me | self only | — | — | own submission |
| GET /courses/:id/content-items/:assignmentId/submissions | if enrolled (own) | if enrolled / teacher (all) | any | LEFT JOIN enrollments |
| POST /submissions/:id/grade | — | if enrolled as teacher | any | teacher/admin AND enrolled in course |
| GET /submissions/:id/files | if owns submission | if may grade | any | — |
| GET /users/me/deadlines | any | any | any | upcoming assignments across enrolled courses |
| GET /courses/:id/gradebook | if enrolled (own row) | if enrolled / teacher | any | all students × assignments |

**File read rule** — a file is readable if:
1. linked to a visible, non-deleted content item in a course you're enrolled in, **or**
2. linked to a submission you own, **or**
3. linked to a submission you may grade (teacher/admin enrolled in the course).

A teacher cannot download a random student's upload unless it's attached to a submission they're grading.

**Grading rule** — `POST /submissions/:id/grade`: caller must be `teacher` or `admin` **AND** enrolled in the course that owns the submission's assignment. Join: `submissions → assignments → content_items → courses → enrollments`. "Is a teacher" is not enough; must be "teaches this course."

---

## 6. File uploads (local disk)

- Endpoint: `POST /files` multipart, one file.
- Constraints enforced server-side: max size (e.g. 50 MB → 413), allowlist `pdf/pptx/md/zip` → 415 otherwise.
- MIME type derived server-side from content/extension; never trust client MIME.
- `storage_key` = `users/<user_id>/<uuid>.<ext>` generated by backend; client never supplies it.
- `original_name` sanitized before echoing into headers/responses.
- Files soft-deleted (`deleted_at`); hard delete refused while referenced by `content_items` or `submission_files`.
- Course deletion: collect `storage_key`s before cascade, delete from storage, then soft-delete DB rows.
- Submission creation accepts `file_ids` and validates each belongs to the student in the same request. Resubmit replaces `submission_files` rows (old files become storage orphans — documented ceiling; cleanup job later).

---

## 7. Endpoint list

**Auth:** signup, login, refresh, logout, me, user search (admin)

**Courses:** create, list, get, patch, delete (soft)

**Enrollments:** create (self or teacher/admin), delete, list members

**Content items** (single route set; no separate `/assignments/*`): tree (with `is_visible` server-side filter), create, get, patch, delete (soft)

**Submissions:** create/resubmit (single attempt), get mine, get roster (left join — missing work shown), grade, list files

**Deadlines:** `GET /users/me/deadlines` — upcoming assignments across enrolled courses, one joined query

**Gradebook:** `GET /courses/:id/gradebook` — students × assignments with submission/grade

---

## 8. Cross-repo integration

Frontend and backend are separate repos. The contract in `API_CONTRACT.md` is the boundary.

**CORS** — The Go backend sets `Access-Control-Allow-Origin` to the frontend origin (env `CORS_ORIGIN`, e.g. `http://localhost:5173` in dev). Because the frontend sends `Authorization: Bearer <token>` (not cookies), credentials are not required, so a single origin is fine. Do not use `*` in production; list the frontend domain explicitly.

**Env vars**
- Backend: `CORS_ORIGIN`, `DATABASE_URL`, `JWT_SECRET`, `REFRESH_TOKEN_SECRET`, `STORAGE_PATH` (default `/data/uploads/`), `PORT` (default `8080`)
- Frontend: `VITE_API_URL` (e.g. `http://localhost:8080`)

**Frontend contract — content tree** (most important payload, consumed by the separate frontend repo):

```json
[
  {
    "id": "...",
    "type": "section",
    "title": "Week 1",
    "position": 1,
    "is_visible": true,
    "children": [
      { "id": "...", "type": "file", "title": "syllabus.pdf", ... },
      { "id": "...", "type": "assignment", "title": "Homework 1", "due_at": "...", "max_points": 100, ... }
    ]
  }
]
```

Auth: store tokens (memory or `localStorage` — document XSS tradeoff), send `Authorization: Bearer` on every request. 401 → clear tokens, redirect to login.

---

## 9. Shortcuts documented (ceiling + upgrade path)

| Shortcut | Ceiling | Upgrade |
|---|---|---|
| Global role on `users`, not per-course | Can't model teacher-in-one-course / student-in-another | Move `role` to `enrollments` |
| Local disk storage | Container restart loses uploads | S3-like storage, one interface swap |
| Single submission attempt | No graded re-attempts | Add `attempt_number`, keep history |
| Resubmit replaces files (old ones orphan) | Storage bloat over a semester | Nightly orphan cleanup job |
| JWT in localStorage | XSS token theft | httpOnly cookies + refresh rotation |

---

## 10. Implementation order (backend)

1. Migrations + `db` package
2. Auth (signup/login/refresh/logout/me) + middleware + CORS
3. Courses + enrollments
4. Content items tree
5. Files + local storage
6. Submissions + grading
7. Deadlines + gradebook

The separate frontend repo consumes `API_CONTRACT.md` once backend endpoints are stable.
