# Reports

Anyone signed in (student, mentor, lecturer, admin) can report a problem to the admins, for example a mentor
reporting a student's behaviour. A report is a **subject** and a **message**. Only admins can read reports, mark them
resolved with a note, and delete them. The sender only gets their own report back once, when they send it.

Table: `reports` (migration `028_reports`). A report stays when its sender's account is deleted; `reporter` is then
`null`.

## Sending (anyone signed in)

### `POST /reports`

```json
{ "subject": "Student X misbehaves in class", "message": "He interrupts others every lecture and ..." }
```

- `subject`: 1 to 200 characters, `message`: 1 to 5000 characters. Both are trimmed.
- `201`:

```json
{ "id": 14, "subject": "Student X misbehaves in class", "message": "He interrupts ...", "created_at": "2026-11-16T10:00:00Z" }
```

- `422 validation_failed` lists every problem in `details`, e.g.
  `[{"field": "subject", "message": "subject is required"}, {"field": "message", "message": "message must be at most 5000 characters"}]`.
- `400` for a body that is not one JSON object, `413` for a body over 64 KB, `401` when not signed in.

## Reading and handling (admin only, `403` for everyone else)

### `GET /reports`

Every report, newest first. Optional filters, combined with AND:

| Query | Meaning |
| --- | --- |
| `status` | `open` or `resolved` |
| `role` | the sender has this role: `student`, `mentor`, `lecturer`, `admin` |
| `reporter_id` | sent by this user |
| `q` | words in the subject or the message (case does not matter) |

```json
[
  {
    "id": 14,
    "subject": "Student X misbehaves in class",
    "message": "He interrupts others every lecture and ...",
    "status": "open",
    "reporter": { "id": 7, "name": "Murad", "email": "murad@example.com", "roles": ["mentor"] },
    "admin_note": null,
    "resolved_by": null,
    "resolved_at": null,
    "created_at": "2026-11-16T10:00:00Z"
  }
]
```

A bad `status` or `role` gives `422`, a bad `reporter_id` `400`.

### `GET /reports/summary`

```json
{ "open": 3, "resolved": 11, "total": 14 }
```

### `GET /reports/{id}`

One report, same shape as in the list. Unknown id: `404`.

### `PATCH /reports/{id}`

```json
{ "status": "resolved", "admin_note": "Talked to the student and his mentor." }
```

- Send `status` (`open` or `resolved`), `admin_note` (up to 2000 characters) or both. A field left out stays as it is.
- Resolving records `resolved_by` (the admin) and `resolved_at`. Resolving again keeps the first ones. Setting
  `open` again clears them.
- `"admin_note": ""` removes the note.
- Answers the updated report. `422` for an unknown status, a note over 2000 characters, or an empty body; `404` for
  an unknown id.

### `DELETE /reports/{id}`

Removes the report for good. `204`; `404` for an unknown id.
