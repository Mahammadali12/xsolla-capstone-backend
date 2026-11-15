# SMTP Email Delivery Implementation Plan

## Goal

Replace the notification service's logging email sender with a real SMTP sender.

The implementation must be **SMTP-provider-agnostic**. Gmail will be the first SMTP server used for testing, but the application must not contain Gmail-specific logic. Later, switching to an SMTP server associated with a custom domain should require configuration changes, not changes to notification business logic.

## Target flow

```text
Notification event
    |
    v
Notification dispatcher
    |
    v
Existing EmailSender interface
    |
    v
SMTP implementation
    |
    v
Configured SMTP server
    |
    v
Recipient's mailbox
```

The notification service remains responsible for policies, templates, recipient resolution, delivery history, and retry decisions. The SMTP adapter is responsible only for sending an email through the configured SMTP server.

---

## Step 1 — Review the existing email sender interface

Before changing anything:

1. Locate the existing `EmailSender` interface.
2. Locate the logging/mock implementation currently used by the dispatcher.
3. Confirm how the dispatcher handles sender errors and how delivery attempts are recorded.
4. Keep the interface unchanged unless it is missing information required to send an email.

The goal is to replace the implementation, not redesign the notification pipeline.

**Done when:**
- The current sender interface and its call sites are understood.
- Existing notification dispatching and delivery tracking do not need to change.

## Step 2 — Define provider-neutral SMTP configuration

Add SMTP configuration to the notification service.

Suggested environment variables:

| Variable | Purpose |
|---|---|
| `SMTP_HOST` | SMTP server hostname |
| `SMTP_PORT` | SMTP submission port |
| `SMTP_USERNAME` | Authentication username; optional for servers that do not require authentication |
| `SMTP_PASSWORD` | Authentication password; optional for servers that do not require authentication |
| `SMTP_FROM` | Sender address used for outgoing messages |
| `SMTP_TLS_MODE` | Connection security mode: `starttls` or `tls` |
| `SMTP_TIMEOUT` | Maximum time allowed for the SMTP operation |

Configuration rules:

- Require `SMTP_HOST`, `SMTP_PORT`, and `SMTP_FROM`.
- Require `SMTP_USERNAME` and `SMTP_PASSWORD` together, or leave both empty if the configured server does not use SMTP authentication.
- Reject unsupported TLS modes.
- Do not disable certificate verification in production.
- Do not hardcode provider hostnames, credentials, ports, or sender addresses in Go code.
- Validate configuration when the service starts, so invalid settings fail early.

Use `starttls` for STARTTLS connections, commonly on port 587. Use `tls` for implicit TLS connections, commonly on port 465. The configured SMTP server determines which combination is correct.

**Done when:**
- SMTP settings are loaded from environment variables.
- Invalid or incomplete settings produce clear startup errors.
- No provider-specific settings are embedded in application logic.

## Step 3 — Implement the SMTP sender

Create an SMTP implementation of the existing `EmailSender` interface.

Its responsibilities should be limited to:

1. Accept the recipient and rendered message.
2. Establish a connection to the configured SMTP host and port.
3. Establish TLS according to `SMTP_TLS_MODE`.
4. Authenticate when credentials are configured.
5. Submit the message.
6. Return a useful error if sending fails.

Keep connection details and SMTP protocol handling inside this implementation. The dispatcher should not know which SMTP provider is being used.

Use a maintained SMTP client library or a carefully implemented SMTP client. Set connection and operation timeouts. Keep normal TLS certificate and hostname verification enabled.

**Done when:**
- The logging sender can be replaced by the SMTP sender through dependency injection.
- The dispatcher does not contain SMTP protocol code.
- Connection, authentication, TLS, and send failures are returned to the caller.

## Step 4 — Build valid email messages

Make sure the sender can deliver the message formats required by the notification service.

At minimum:

- `From`
- `To`
- `Subject`
- Plain-text body

Support an HTML body if the existing template renderer produces one. Prefer sending both plain-text and HTML alternatives when both are available.

Additional requirements:

- Encode headers and message bodies correctly.
- Reject invalid email addresses and newline characters in header values to prevent header injection.
- Do not put credentials or internal error details in outgoing messages.
- Keep message rendering in the template layer, not in the SMTP adapter.

**Done when:**
- A plain-text message can be sent.
- An HTML message can be sent if HTML templates are supported.
- Message headers and body render correctly in common mail clients.

## Step 5 — Configure Docker for SMTP

Pass SMTP configuration into the notification-service container through Docker Compose.

Example:

```yaml
services:
  notification-service:
    build: .
    env_file:
      - .env
```

Keep real credentials in the local `.env` file and add `.env` to `.gitignore`.

Commit only an `.env.example` containing placeholder values. Never put real SMTP credentials in the Dockerfile, source code, image layers, or repository.

For production, use the deployment platform's secret-management mechanism rather than committing a `.env` file.

**Done when:**
- The container receives the SMTP configuration at runtime.
- The application image contains no SMTP credentials.
- The service can be configured without rebuilding the image.

## Step 6 — Send the first real test email through Gmail

Use Gmail as the first SMTP server, without adding Gmail-specific code to the application.

Example local configuration:

```env
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USERNAME=your-address@gmail.com
SMTP_PASSWORD=your-google-app-password
SMTP_FROM=your-address@gmail.com
SMTP_TLS_MODE=starttls
SMTP_TIMEOUT=15s
```

For Gmail SMTP authentication, use a Google App Password when the account is eligible for it. Do not use your normal Google account password. App Passwords require 2-Step Verification and may be unavailable for some account types or managed accounts.

Keep the password private. Do not paste it into source code, commit it, or share it in chat.

Test with a single recipient and a simple message before connecting the sender to normal notification events.

**Done when:**
- The notification-service container can connect to Gmail SMTP.
- SMTP authentication succeeds.
- A real test email arrives in the intended inbox.
- The application logs success without exposing credentials or message contents unnecessarily.

## Step 7 — Test SMTP failure handling

Test more than the successful path.

Cover at least:

- Incorrect credentials.
- Unreachable SMTP host.
- Connection timeout.
- TLS negotiation failure.
- Invalid recipient address.
- SMTP temporary failure.
- SMTP permanent failure.

Make sure failures flow back to the existing delivery and retry mechanism.

General handling:

- Temporary network errors and SMTP 4xx responses can be retried according to the service's bounded retry policy.
- SMTP 5xx responses generally indicate a permanent failure and should not be retried indefinitely.
- Record the attempt count and a sanitized error in delivery history.
- Do not create a new logical delivery record for every retry.

**Important limitation:** SMTP does not provide an exactly-once delivery guarantee. For example, the SMTP server may accept a message and the connection may fail before the client receives the final response. Retrying in that uncertain situation can result in a duplicate email. Keep the existing event/delivery idempotency protections, but do not claim they can eliminate every possible duplicate at the SMTP boundary.

**Done when:**
- Failures are recorded.
- Transient errors follow bounded retries.
- Permanent errors do not retry forever.
- A failed send does not crash the entire consumer process.

## Step 8 — Connect the SMTP sender to the notification pipeline

Replace the logging sender in the service's dependency wiring with the SMTP implementation.

Keep the existing sequence:

```text
RabbitMQ event
    -> Consumer
    -> Dispatcher
    -> Policy evaluation
    -> Recipient resolution
    -> Template rendering
    -> EmailSender
    -> Delivery result persistence
```

Do not add SMTP calls to RabbitMQ consumer code or individual event handlers. All email delivery should pass through the existing sender abstraction.

**Done when:**
- An eligible event causes the notification service to send an actual email.
- Disabled policies or events that do not match a policy do not send email.
- Delivery status and errors are persisted through the existing mechanism.

## Step 9 — Add operational safeguards

Before treating the integration as production-ready:

- Use bounded connection and operation timeouts.
- Keep TLS certificate verification enabled.
- Avoid logging passwords, authentication exchanges, full message bodies, or unnecessary personal data.
- Log the event ID and delivery ID so a send can be traced.
- Monitor send success, failure, retry counts, and queue backlog.
- Ensure shutdown allows in-flight work to finish or be safely retried.
- Keep SMTP credentials out of source control and container images.

**Done when:**
- Email attempts can be traced without exposing secrets.
- A temporary SMTP outage does not cause an infinite retry loop.
- The consumer can recover after the SMTP server becomes available again.

## Step 10 — Switch to a custom-domain SMTP server later

When a custom-domain SMTP server or relay is available, configure its connection details and credentials through the same environment variables.

For example, the deployment configuration may change:

```env
SMTP_HOST=mail.example.com
SMTP_PORT=587
SMTP_USERNAME=notifications@example.com
SMTP_PASSWORD=<secret>
SMTP_FROM=notifications@example.com
SMTP_TLS_MODE=starttls
SMTP_TIMEOUT=15s
```

The actual host, port, authentication requirements, and TLS mode must come from the selected SMTP server's configuration.

The application should not need a Gmail-to-custom-domain migration or provider-specific code change. Only the SMTP configuration and possibly the authorized sender address should change.

**Done when:**
- The same notification-service image can send through the new SMTP server by changing configuration alone.
- Existing templates, policies, event contracts, and delivery records continue to work.

---

## Recommended implementation order

1. Review the existing `EmailSender` interface and delivery error handling.
2. Add and validate provider-neutral SMTP configuration.
3. Implement the SMTP sender behind the existing interface.
4. Test message construction.
5. Pass configuration into Docker Compose securely.
6. Send one real test email through Gmail SMTP.
7. Test connection, authentication, TLS, and SMTP response failures.
8. Wire the SMTP sender into the notification pipeline.
9. Add operational safeguards and verify recovery behavior.
10. Document the configuration needed to switch SMTP servers later.

## Out of scope for this iteration

Do not build these unless the project requirements call for them:

- Hosting your own SMTP server.
- Purchasing or configuring a domain.
- Building an email administration dashboard.
- Gmail API integration or OAuth-based Gmail sending.
- Provider-specific SDKs.
- A new notification policy engine.
- A new retry system if the notification service already has one.

## Final acceptance test

The implementation is complete when all of the following are true:

1. The notification service starts with valid SMTP configuration supplied through Docker.
2. A test email can be sent through Gmail SMTP.
3. A real notification event can trigger an email through the existing pipeline.
4. Delivery success and failure are recorded.
5. Transient failures use bounded retries, while permanent failures are not retried indefinitely.
6. Credentials are not committed or logged.
7. Changing to another SMTP server requires configuration changes only, not changes to notification business logic.
