# Announcements

An admin sends a short message to everyone or to one group. It reaches each person as an **in-app notification**
(`GET /notifications`, `type: "announcement.created"`) and an **email**, through the notification service like every
other notification (policy `announcement.created`, template `announcement`).

Table: `broadcasts` (migration `030_broadcasts`). Both routes are **admin only** (`403` for everyone else).

## `POST /notifications/broadcast`

```json
{ "message": "No lectures on Friday.", "link": "/calendar", "audience": "students" }
```

| Field | Rule |
| --- | --- |
| `message` | 1 to 500 characters, trimmed |
| `link` | `""` or a page of the LMS starting with `/` (e.g. `/calendar`, `/office-tour`); never another site |
| `audience` | `all` (students, mentors and lecturers), `students`, `mentors` or `lecturers` |

Admins are never in an audience. Only active people count: not deleted, not left, and for temporary roles not
expired.

`201`:

```json
{ "id": 7, "message": "No lectures on Friday.", "link": "/calendar", "audience": "students",
  "sent_at": "2026-11-17T09:00:00Z", "sent_by": "Adil", "recipients": 28 }
```

With nobody in the audience the announcement is saved with `"recipients": 0` and nothing is sent. A wrong form gives
`422 validation_failed` with every problem in `details`.

## `GET /notifications/broadcasts`

Every announcement, newest first, in the same shape.
