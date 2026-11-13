# Transactional outbox

Booking a mentor slot writes both the business change and its domain event in
one transaction, so the LMS cannot commit a booking and lose the event.

## Schema

`internal/db/migrations/018_outbox_events.up.sql`:

| Column | Meaning |
| --- | --- |
| `id` | Event id. This is the `id` in the published envelope. |
| `event_type` | Envelope `type` and the AMQP routing key, e.g. `mentor_slot.booked`. |
| `payload` | The envelope's `data` object only (slot, times, recipients). |
| `created_at` | When the business transaction committed; becomes `occurred_at`. |
| `published_at` | Set only after a broker confirmation. `NULL` means pending. |
| `attempts` | Failed publication attempts. |
| `last_error` | Most recent publication failure. |

The row `id` is never `slot_id`: one booking is one event, and re-booking a slot
must produce a new event id even though the slot id repeats.

## Write path

`StoreImpl.BookMentorSlot` opens the transaction, locks the student, claims the
slot, builds the recipient snapshot (mentor and student `name`/`email`), inserts
the outbox row, and commits. Any failure rolls the booking back too. Events are
written by the same `*Query` bound to that transaction, never through the pool.

Only `mentor_slot.booked` is integrated. Other operations still write directly
and need the same transaction-aware treatment before they can emit events.

## Publish path

`cmd/server/main.go` starts `outbox.Publisher` in a goroutine after migrations.

1. Poll `outbox_events WHERE published_at IS NULL ORDER BY id LIMIT 20`, oldest
   first so order is preserved.
2. Build the envelope at publish time: `id` = row id, `version` = 1,
   `producer` = `lms-api`, `occurred_at` = `created_at`, `data` = `payload`.
3. Publish to the topic exchange `lms.events` with the event type as routing
   key, persistent delivery, and publisher confirms enabled.
4. Only after a positive broker ack, set `published_at = NOW()`.

Any publish error, nack, or 5s confirm timeout is recorded in `last_error` and
`attempts`, the row stays unpublished, and the loop retries with backoff from
1s up to 30s. The AMQP connection is dropped on failure and re-established by
the next attempt.

The publisher declares only the exchange. The notification service owns the
queue, dead-letter exchange, DLQ, and all notification policy.

## Delivery guarantees

Publishing is at-least-once:

- a crash after the broker confirmed but before `published_at` was written
  republishes the same event id on restart;
- a stuck publisher can be restarted at any time; pending rows remain.

Duplicate publication is harmless because the notification service deduplicates
on `(producer, event_id, recipient_id, channel)` in its own `deliveries` table
and skips deliveries already marked `sent`.

## Operations

Backlog:

```sql
SELECT id, event_type, created_at, attempts, last_error
FROM outbox_events
WHERE published_at IS NULL
ORDER BY id;
```

A growing backlog with a connection error means RabbitMQ is unreachable; the
API keeps serving and bookings keep succeeding. Rows publish automatically once
the broker returns. There is no retention or cleanup job yet; add one when the
table grows.