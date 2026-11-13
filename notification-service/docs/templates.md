# Message templates

Templates turn an event plus one recipient into a subject and body. They are
kept apart from event parsing, policy lookup, and channel delivery: a policy
names a template, the dispatcher renders one message per recipient, and the
channel adapter sends it.

## Files

One directory, configured with `TEMPLATES_PATH`:

```text
config/templates/booking_confirmation.subject.tmpl
config/templates/booking_confirmation.body.tmpl
```

The file name before the suffix is the template name a policy references.
Templates are parsed at startup; a malformed template, a missing subject or
body file, or a missing directory stops the worker.

## Data

Each template is executed with:

| Root | Contents |
| --- | --- |
| `.Event` | Envelope: `ID`, `Type`, `Version`, `OccurredAt`, `Producer`, `Data`. |
| `.Recipient` | The recipient snapshot: `ID`, `Role`, `Name`, `Email`. |
| `.Payload` | The event's `data` object, e.g. `.Payload.starts_at`. |

Example:

```text
Hi {{.Recipient.Name}},

Your mentor slot is booked from {{.Payload.starts_at}} to {{.Payload.ends_at}}.
```

Missing data is an error, not an empty string: rendering fails with the
template name, the part (subject or body), and the missing key. The dispatcher
renders every recipient before sending anything, so a template failure never
leaves a half-delivered event; the consumer retries and then dead-letters it.

## Status

`config/templates/booking_confirmation.*` is a temporary example that proves the
pipeline. It is not approved notification copy, and no policy references it
while `config/policies.json` is `{}`.

No HTML, localization, layouts, or hot reload yet. Templates are re-read on
restart, not per message.
