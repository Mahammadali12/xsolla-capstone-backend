# In-app notifications

In-app notifications follow the same path as the emails. They are one more
channel, `in_app`, next to `email`:

1. The LMS writes a domain event to its outbox and publishes it to RabbitMQ.
2. The worker looks up the event type's policy in `config/policies.json`.
3. The policy's `recipients` pick the users from `data.recipients[]`.
4. The policy's `template` renders the subject and the plain text body for each
   recipient.
5. For each recipient, each channel in `channels` delivers the message. `email`
   sends it. `in_app` saves it in the `notifications` table.

Every committed policy has `"channels": ["in_app", "email"]`. `in_app` comes
first: when SMTP is down, the in-app notification is still saved, and the event
is retried for the email only (`deliveries` marks `in_app` as `sent`).

## Table

`internal/store/schema.sql` creates it at startup, in the worker's own database:

| Column | Meaning |
| --- | --- |
| `id` | Notification id. The LMS uses it to mark it read. |
| `producer`, `event_id` | The event, e.g. `lms-api` and its outbox id. |
| `event_type` | e.g. `enrollment.graded`. |
| `recipient_id` | LMS user id of the one person who sees it. |
| `title` | The rendered subject. |
| `body` | The rendered plain text body, trimmed. |
| `created_at` | When the event happened (`occurred_at`). |
| `read_at` | `NULL` until the user reads it. |

`UNIQUE (producer, event_id, recipient_id)`: a redelivered event keeps the one
notification it already has, and a read notification stays read.

## Reading them

The worker never reads the table back. The LMS API reads it through
`NOTIFY_DATABASE_URL` and serves `GET /notifications` (unread only),
`POST /notifications/{id}/read` and `POST /notifications/read-all`. Each user
only sees and marks their own. See the LMS `docs/notifications.md`.

## Adding a notification

Add or edit the policy and its template, as for an email. Keep `in_app` in the
policy's `channels`. To send an event by email only, leave `in_app` out.
