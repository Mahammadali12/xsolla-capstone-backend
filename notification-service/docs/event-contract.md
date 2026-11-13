# LMS event contract

The LMS publishes domain facts to RabbitMQ; the notification service consumes
them. This document is the contract for both sides. The Go representation lives
in `internal/events` and is pinned by
`internal/events/testdata/mentor_slot_booked.json`.

## Transport

| Item | Value |
| --- | --- |
| Exchange | `lms.events` (topic, durable) |
| Routing key | the envelope `type`, e.g. `mentor_slot.booked` |
| Queue | `notifications.events` |
| Dead letters | `lms.events.dlx` → `notifications.dlq` |
| Content type | `application/json` |
| Delivery mode | persistent (publisher must set it; the current values are in `docker-compose.yml`) |

Messages are delivered at least once. The consumer uses `(producer, id)` to
discard duplicates.

## Delivery handling

- Consumers acknowledge manually, only after the handler succeeds.
- Handler failures are retried three times with 1s then 2s backoff. After that
  the message is rejected to `notifications.dlq`.
- Malformed JSON and unsupported `version` values are dead-lettered immediately
  and never retried.
- On shutdown, an in-flight unacknowledged message is requeued when the channel
  closes.
- Delivery is at least once, so consumers must be idempotent on `(producer, id)`.
- The dispatcher acknowledges events with no policy, a disabled policy, or a
  policy without channels, and logs it. An enabled policy with no matching
  recipient is an error, so the event is retried and then dead-lettered.

## Envelope

```json
{
  "id": 18422,
  "type": "mentor_slot.booked",
  "version": 1,
  "occurred_at": "2026-11-13T12:00:00Z",
  "producer": "lms-api",
  "data": {
    "slot_id": 123,
    "starts_at": "2026-11-19T10:00:00Z",
    "ends_at": "2026-11-19T10:30:00Z",
    "recipients": [
      {
        "id": 7,
        "role": "mentor",
        "name": "Mentor One",
        "email": "mentor@example.com"
      },
      {
        "id": 42,
        "role": "student",
        "name": "Student One",
        "email": "student@example.com"
      }
    ]
  }
}
```

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `id` | integer | yes | Positive. The LMS outbox row id. Never reused. |
| `type` | string | yes | `domain.event`, lowercase segments separated by dots. Max 128 chars. |
| `version` | integer | yes | At least 1. |
| `occurred_at` | string | yes | RFC 3339. Producers send UTC (`Z`); consumers normalize to UTC. |
| `producer` | string | yes | Non-blank, max 64 chars. For this repo: `lms-api`. |
| `data` | object | yes | Event-specific payload. Must be a JSON object, never `null`. |

Unknown envelope fields are ignored so the contract can grow.

## Event IDs

The event `id` is the LMS outbox row id (`BIGSERIAL`), assigned inside the same
transaction as the business change. It is **not** a domain entity id.

- Do not use `slot_id`, `submission_id`, `event_id`, etc. as the event id.
- One business fact gets one id. `mentor_slot.booked`,
  `mentor_slot.booking_cancelled`, and a later rebooking of the same slot are
  three events with three ids.
- A crashed publisher may re-publish the same row; that is safe because the id
  is unchanged.
- The consumer's dedupe key is `(producer, id)`. `id` alone is only unique
  within one producer.

## `data` and recipients

`data` is owned by the event type. Recipients live inside it, not in the
envelope, because they are event-specific. Version 1 uses a flat array:

```json
"recipients": [
  {"id": 7, "role": "mentor", "name": "Mentor One", "email": "mentor@example.com"}
]
```

- `role` is one of `student`, `mentor`, `lecturer`, `admin`.
- `id` is the LMS user id.
- `name` and `email` are the minimal delivery snapshot. Missing or empty email
  means the channel cannot deliver; the dispatcher handles that failure.
- Include only recipients the event could plausibly notify. Do not ship whole
  cohorts or extra personal data.

Recipient snapshots put personal data in broker messages. Broker access is
restricted and credentials must not be reused in production.

## Versioning

- Additive changes that consumers can ignore do not bump `version`.
- A breaking change to `data` bumps `version`. The consumer keeps support for
  the versions it understands and rejects unsupported ones as permanent
  failures (dead-letter), not retries.
- Version 1 is the only supported version until a real event needs otherwise.

## Initial event types

These are examples, not an approved notification scope:

```text
mentor_slot.booked
mentor_slot.booking_cancelled
event.created
event.updated
submission.created
submission.graded
```

Adding a type requires no broker change; the queue binds with `#`.

## Producer implementation (LMS)

The producer lands with the transactional outbox. In short:

1. In the business transaction, insert the event into `outbox_events` and get
   its `id`.
2. After commit, publish JSON to `lms.events` with routing key = `type` and
   persistent delivery mode.
3. Mark the outbox row published only after a publisher confirmation.

The notification service must not access the LMS database.
