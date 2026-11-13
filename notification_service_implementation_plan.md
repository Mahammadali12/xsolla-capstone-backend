# Notification Service --- Implementation Plan

## Goal

Build an independent Go notification service that consumes domain events
from the LMS through RabbitMQ and delivers notifications through
pluggable channels.

The notification scope is not finalized yet. Therefore, this plan
focuses on the reusable infrastructure. Specific notification policies,
recipients, templates, and real delivery channels can be added after the
lead confirms the scope.

## Agreed design

-   The LMS is the **event producer**.
-   RabbitMQ transports events between the LMS and the notification
    service.
-   The notification service is the **event consumer**.
-   The LMS publishes facts about business operations, not instructions
    such as `send_email`.
-   Notification rules/policies live in the notification service.
-   The notification service owns delivery, retries, delivery history,
    and idempotency.
-   The notification service has its own configuration and persistence.
    It does not directly access the LMS database.
-   Start with an email sender interface and a logging/mock
    implementation. Do not enable real email delivery yet.

------------------------------------------------------------------------

## Step 1 --- Create the independent Go service

Create a separate repository or module for the notification service.

Suggested initial structure:

``` text
notification-service/
├── cmd/
│   └── worker/
│       └── main.go
├── internal/
│   ├── config/
│   ├── broker/
│   ├── events/
│   ├── consumer/
│   ├── notification/
│   ├── channels/
│   │   └── email/
│   └── store/
├── migrations/
├── Dockerfile
├── docker-compose.yml
├── go.mod
└── .env.example
```

Keep the first version small. Avoid creating packages that have no clear
responsibility yet.

**Done when:** - The service builds independently. - It starts as a
separate process. - Configuration is loaded from environment
variables. - It can shut down gracefully on termination signals.

## Step 2 --- Add RabbitMQ to the development infrastructure

Add RabbitMQ to the development Compose setup.

Configure: - A durable topic exchange named `lms.events`. - A durable
queue named `notifications.events`. - A dead-letter queue named
`notifications.dlq`. - A dead-letter exchange and routing
configuration. - Persistent messages. - A health check for RabbitMQ.

Use a topic exchange so routing keys can be introduced without
redesigning the broker topology.

Initial routing-key examples:

``` text
mentor_slot.booked
mentor_slot.booking_cancelled
event.created
event.updated
submission.created
submission.graded
```

These are examples of possible domain events, not a finalized
notification scope.

**Done when:** - RabbitMQ starts with the development environment. - The
notification service can connect to it. - The service can declare or
verify its exchange and queues. - Connection failures are handled
without crashing in a restart loop.

## Step 3 --- Define the event envelope

Create a shared event contract. The LMS and notification service must
agree on this format.

Example:

``` json
{
  "id": "uuid",
  "type": "mentor_slot.booked",
  "version": 1,
  "occurred_at": "2026-11-13T12:00:00Z",
  "producer": "lms-api",
  "data": {
    "slot_id": 123,
    "mentor_id": 7,
    "student_id": 42,
    "starts_at": "2026-11-19T10:00:00Z",
    "ends_at": "2026-11-19T10:30:00Z"
  }
}
```

Field responsibilities:

  Field           Purpose
  --------------- ---------------------------------------------------
  `id`            Unique event ID; used for idempotency and tracing
  `type`          Domain event name
  `version`       Event schema version
  `occurred_at`   Time the business operation occurred
  `producer`      Service that produced the event
  `data`          Event-specific payload

Use UTC timestamps and stable identifiers. Validate required envelope
fields before dispatching an event.

### Recipient data decision

Before production integration, choose one approach:

1.  Include recipient IDs and a minimal recipient snapshot (such as
    email and display name) in the event.
2.  Include recipient IDs only and resolve contact details through an
    internal LMS API.

For the first version, recipient snapshots keep the notification service
more independent, but they expose personal data in broker messages.
Restrict broker access and avoid putting unnecessary personal
information in events.

**Done when:** - The event envelope is represented in Go types. - Valid
and invalid JSON payloads are tested. - The event contract is documented
for the LMS producer.

## Step 4 --- Implement the RabbitMQ consumer

Implement a consumer that:

1.  Connects to RabbitMQ.
2.  Declares or verifies the exchange and queue.
3.  Binds the queue to the required routing keys.
4.  Reads messages.
5.  Decodes and validates the event envelope.
6.  Passes valid events to the event dispatcher.
7.  Acknowledges messages only after successful processing.

Use manual acknowledgments. Do not acknowledge a message immediately
after receiving it.

Handle failures deliberately: - A temporary processing or provider
failure should be retried. - A malformed or unsupported message should
not be retried forever. - Messages that exceed the retry policy should
reach the dead-letter queue. - Avoid immediate requeue loops; use
delayed retry queues or another bounded retry mechanism.

**Done when:** - A test message published to RabbitMQ is consumed. - A
valid message reaches the dispatcher. - A malformed message is handled
according to the failure policy. - The consumer reconnects after a
broker interruption. - Messages are not acknowledged before processing
succeeds.

## Step 5 --- Implement the event dispatcher

The dispatcher receives a validated event and selects the relevant
notification policy.

Conceptual flow:

``` text
Receive event
    |
    v
Find policy by event type
    |
    v
Is policy enabled?
    |
    +-- No --> Finish without delivery
    |
    v
Resolve recipients
    |
    v
Select enabled channels
    |
    v
Render message
    |
    v
Create delivery work
    |
    v
Send through channel adapter
```

Unknown event types should be handled safely and logged. They should not
crash the worker.

Keep event decoding separate from policy evaluation and channel
delivery.

**Done when:** - The dispatcher can route an event to a registered
policy. - Disabled or missing policies do not trigger delivery. -
Unknown event types are observable and handled safely. - Unit tests
cover policy lookup and dispatch behavior.

## Step 6 --- Add notification policies inside the notification service

Notification rules belong to the notification service, not the LMS.

A policy should eventually describe: - Whether an event type is
enabled. - Which recipients should be notified. - Which channels should
be used. - Which template should render the message. - Any supported
conditions or preferences.

Illustrative policy:

``` yaml
event_type: mentor_slot.booked
enabled: true
channels:
  - email
recipients:
  - mentor
  - student
template: booking_confirmation
```

This is an example only. Do not treat it as an approved business
requirement.

For the initial implementation, use a simple in-code policy registry or
configuration file. Do not build a dynamic rules engine,
policy-management UI, or complex condition language before the
requirements justify them.

The LMS should publish relevant domain events independently of whether a
notification policy is currently enabled.

**Done when:** - Policies can be registered by event type. - A policy
can be enabled or disabled. - Recipient and channel selection are
separated from the event consumer. - Adding a policy does not require
changing the RabbitMQ consumer.

## Step 7 --- Define the delivery interface and add a logging sender

Define a channel abstraction so the dispatcher does not depend on a
specific email provider.

Conceptually:

``` text
NotificationChannel
    Send(ctx, recipient, message) -> error
```

Implement an email channel first, but initially make it a logging/mock
sender.

The mock sender should log: - Event ID. - Recipient identifier or safe
recipient address. - Channel. - Rendered subject and body, where
appropriate. - Result.

Do not connect the existing Gmail helper automatically. It is tied to
the LMS Google OAuth context and should not be assumed to be a suitable
system-wide notification provider.

Choose a real email provider only after the service pipeline and
delivery requirements are agreed.

**Done when:** - The dispatcher can call a channel through an
interface. - The logging sender receives and logs a rendered message. -
A different sender implementation can be substituted without changing
the dispatcher.

## Step 8 --- Add templates

Keep message templates separate from business event parsing and channel
delivery.

Start with a minimal template renderer that accepts structured data and
returns a subject and body.

Do not invent final templates before the lead confirms which
notifications are required. A temporary test template is enough to prove
the pipeline.

**Done when:** - A test event can be rendered into a message. - Missing
template data produces a clear error. - Template rendering is unit
tested.

## Step 9 --- Add delivery persistence and idempotency

The notification service should own its delivery history.

Suggested delivery record:

  Field            Purpose
  ---------------- -----------------------------
  `id`             Delivery record ID
  `event_id`       Original event ID
  `recipient_id`   Intended recipient
  `channel`        Email, in-app, etc.
  `status`         Pending, sent, failed
  `attempts`       Number of delivery attempts
  `last_error`     Most recent error
  `created_at`     Record creation time
  `sent_at`        Successful delivery time

Use a uniqueness constraint for the logical delivery, for example:

``` text
(event_id, recipient_id, channel)
```

This prevents a redelivered RabbitMQ event from creating duplicate
logical deliveries.

Important distinction: RabbitMQ provides at-least-once delivery in this
design. Do not assume exactly-once processing. The notification service
must make repeated processing safe.

Persist delivery state so failures can be investigated and retried
deliberately.

**Done when:** - Delivery attempts are stored. - Duplicate event
processing does not create duplicate logical deliveries. - Success and
failure states are recorded. - Database errors do not result in falsely
acknowledging an event.

## Step 10 --- Add the transactional outbox to the LMS

Do this when the notification service can consume events and the
producer contract is agreed.

Avoid this unsafe sequence:

``` text
1. Commit business change to PostgreSQL.
2. Publish event to RabbitMQ.
```

If the database commit succeeds but RabbitMQ publishing fails, the
business operation exists but its event is lost.

Use a transactional outbox:

``` text
LMS database transaction:
    1. Perform business operation.
    2. Insert event into outbox_events.
    3. Commit transaction.

Background publisher:
    1. Read unpublished outbox records.
    2. Publish event to RabbitMQ.
    3. Wait for publisher confirmation.
    4. Mark event as published.
```

The business change and outbox insertion must use the same database
transaction.

The publisher must tolerate crashes and duplicate publication. The
consumer's idempotency mechanism handles redelivery.

In the existing LMS, mentor-slot booking already uses an explicit
database transaction, making it a useful first integration candidate.
Other operations that currently write directly will need
transaction-aware changes before they can use the outbox safely.

**Done when:** - Business data and outbox records commit atomically. -
Failed RabbitMQ publication leaves the outbox record available for
retry. - Publisher confirmations are handled. - Re-publishing does not
cause duplicate notification delivery.

## Step 11 --- Integrate the first LMS event

After the pipeline and outbox are ready, select one event with the lead.
Use it as the first end-to-end integration.

The integration should: 1. Complete a real LMS business operation. 2.
Insert its domain event into the outbox transaction. 3. Publish the
event to RabbitMQ. 4. Consume it in the notification service. 5. Match
it to a notification policy. 6. Resolve the recipient. 7. Render a test
message. 8. Pass it to the logging sender. 9. Persist the delivery
result.

Do not integrate every possible LMS operation at once. Prove one
complete path first, then expand.

**Done when:** - One real LMS operation produces an event. - The event
reaches the notification service. - The configured policy is applied. -
A test delivery is recorded without sending a real email.

## Step 12 --- Add observability and operational safeguards

Add structured logs and metrics for: - Events received and processed. -
Events ignored due to missing or disabled policies. - Delivery successes
and failures. - Retry counts. - Dead-lettered messages. - Consumer
connection and reconnection. - Outbox backlog and oldest unpublished
event. - Processing latency.

Include `event_id` in logs so an event can be traced across the LMS,
RabbitMQ, and notification service.

Also implement: - Graceful shutdown. - Bounded retries. - Timeouts for
channel providers. - Configuration validation at startup. - Secrets
supplied through environment variables or secret management. -
Restricted RabbitMQ credentials and network access.

**Done when:** - Operators can identify a failed event by its ID. -
Retry and DLQ activity is visible. - The service shuts down without
abruptly abandoning in-flight work. - No secrets are committed to the
repository.

------------------------------------------------------------------------

## Recommended implementation order

  ------------------------------------------------------------------------
                         Order Work                  Can start before
                                                     notification scope is
                                                     finalized?
  ---------------------------- --------------------- ---------------------
                             1 Create independent Go Yes
                               service               

                             2 Add RabbitMQ          Yes
                               infrastructure        

                             3 Define event envelope Yes

                             4 Implement consumer    Yes
                               and acknowledgments   

                             5 Implement dispatcher  Yes

                             6 Add basic policy      Yes
                               registry              

                             7 Add logging email     Yes
                               sender                

                             8 Add basic template    Yes
                               rendering             

                             9 Add delivery          Yes
                               persistence and       
                               idempotency           

                            10 Integrate             After producer
                               transactional outbox  contract is agreed
                               in LMS                

                            11 Select and integrate  After lead confirms
                               first real LMS event  scope

                            12 Enable real           After requirements
                               notification policies and provider
                               and providers         decisions
  ------------------------------------------------------------------------

## Explicitly out of scope for the initial foundation

Do not implement these until there is a confirmed requirement:

-   In-app notification UI and unread counts.
-   Push notifications.
-   SMS or messaging-platform integrations.
-   User notification preference screens.
-   Admin policy-management UI.
-   Dynamic rules engine.
-   Scheduled reminders and digest emails.
-   Final notification templates and role-specific business rules.
-   Production email provider selection.

In-app notifications will eventually need their own persistent
user-facing model and read/unread APIs. They should not be treated as
merely another email sender.

## Initial end-to-end acceptance test

The first milestone is complete when this pipeline works locally:

``` text
Test Publisher
      |
      v
   RabbitMQ
      |
      v
Notification Consumer
      |
      v
Event Dispatcher
      |
      v
Policy Registry
      |
      v
Template Renderer
      |
      v
Logging Email Sender
      |
      v
Delivery History
```

The test must demonstrate: - A valid event is consumed. - The correct
policy is selected. - A test message is rendered. - The logging sender
receives it. - Delivery history is persisted. - Reprocessing the same
event does not create a duplicate logical delivery. - A processing
failure is retried or dead-lettered according to policy.

## Decisions still required from the lead

The following are business decisions, not prerequisites for building the
infrastructure:

1.  Which LMS events should generate notifications?
2.  Which roles should receive each notification: student, mentor,
    lecturer, admin?
3.  Which channels are required initially?
4.  Are notifications mandatory, configurable by users, or both?
5.  Are scheduled reminders or digest notifications required?
6.  Should recipient contact details be included in events or resolved
    through an LMS API?
7.  Which email provider should be used for production?

Until these are answered, keep the implementation generic and use test
events with a logging sender.
