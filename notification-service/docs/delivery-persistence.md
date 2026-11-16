# Delivery persistence

The notification service keeps its own delivery history in Postgres. It never
reads the LMS database.

## Table

`schema.sql` creates the delivery table at startup (and the `notifications`
table of the `in_app` channel, see `in-app.md`):

| Column | Meaning |
| --- | --- |
| `id` | Delivery record id. |
| `producer` | Envelope producer, e.g. `lms-api`. |
| `event_id` | The event's outbox id. |
| `event_type` | Envelope type, kept for diagnosis. |
| `recipient_id` | LMS user id from `data.recipients[]`. |
| `channel` | Channel name: `email` or `in_app`. |
| `status` | `pending`, `sent`, or `failed`. |
| `attempts` | How many times this delivery was attempted. |
| `last_error` | Most recent failure cause. |
| `created_at` | When the logical delivery was first recorded. |
| `sent_at` | When it succeeded. |

The logical-delivery key is `(producer, event_id, recipient_id, channel)`.

`producer` is part of the key because event ids are only unique within one
producer; `event_id` is not globally unique. One event produces one row per
recipient and channel, so a rebooked slot (new event id) creates new rows while
a redelivered message does not.

## Claim, send, record

For each recipient and channel the dispatcher:

1. Claims the delivery. The insert and the dedupe are one statement:
   `INSERT … ON CONFLICT (producer, event_id, recipient_id, channel) DO UPDATE
   SET attempts = attempts + 1 WHERE status <> 'sent'`.
2. A claim that returns no row means the delivery already succeeded: it is
   skipped and logged, and nothing is sent.
3. Sends through the channel adapter.
4. On success marks the row `sent` with `sent_at`. On failure marks it `failed`
   with `last_error`, increments stay visible in `attempts`, and returns the
   error so the consumer retries.

The consumer retries a failing event three times, so a failing delivery records
`attempts = 3` and the last cause before the message is dead-lettered.

## At-least-once, not exactly-once

Duplicate rows are impossible, but duplicate sends are: if the process dies
after the channel accepted a message and before the row is marked `sent`, the
redelivered event claims the row again and sends again. No design with an
external mail provider can fully close that window; do not claim exactly-once.

## Failure behavior

Database errors are never swallowed:

- a failed claim or a failed `MarkDeliverySent` returns an error, so the event
  is **not acknowledged**; the consumer retries it;
- a failed `MarkDeliveryFailed` is logged and the original send error is still
  returned, so retry/dead-letter behavior is unchanged.

`/readyz` pings the database and returns 503 while it is down. `/healthz` stays
liveness-only.

## Not in this step

No retry worker, manual retry endpoint, retention policy, or migration tooling.
`schema.sql` only runs `CREATE TABLE IF NOT EXISTS`; add real migrations when
the schema changes.
