# In-app notifications

Every user gets their own notifications in the LMS, made from the same events,
policies and templates as the emails (`notification-service/docs/in-app.md`).
The notification worker saves them in its own database. The LMS API reads that
database and serves them to their user.

## Setting

`NOTIFY_DATABASE_URL`: the notification worker's database, e.g.
`host=localhost port=5433 user=notify password=notify dbname=notify sslmode=disable`
(the notification stack's compose Postgres). It is optional:

- not set: the three routes answer `503 service_unavailable`, and the rest of
  the API works as before;
- set, but the database does not answer: the API still starts, the routes answer
  `500` and work again as soon as the database is back.

The `notifications` table is created by the worker when it starts, so start the
worker once before using the routes.

## Routes

All three need a signed-in user (any role) and only ever touch that user's own
notifications.

### `GET /notifications`

The unread notifications, newest first. Read ones are never sent.

- `limit` (optional): 1 to 100, default 50. Anything else gives `400`.

```json
[
  {
    "id": 41,
    "type": "enrollment.graded",
    "title": "Grade posted for Week 3: Concurrency",
    "message": "Hi Anna,\n\nMurad posted your grade for Week 3: Concurrency: green.",
    "created_at": "2026-11-16T08:00:00Z",
    "read_at": null
  }
]
```

- `type` is the event: `module.created`, `content_item.uploaded`,
  `event.created`, `mentor_availability.created`, `mentor_slot.booked`,
  `mentor_slot.canceled`, `submission.created`, `enrollment.graded`.
- `title` is the email's subject, `message` its plain text body.
- No unread notifications: `[]`.

### `POST /notifications/{id}/read`

Marks one notification read, so the next `GET /notifications` does not send it.
Answers `204`. Marking it again is fine (it keeps the first read time).
Someone else's or an unknown id: `404`.

### `POST /notifications/read-all`

Marks every unread notification of the user read.

```json
{ "marked": 3 }
```
