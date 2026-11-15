# Message templates

Templates turn an event plus one recipient into a subject, a plain-text body,
and an optional HTML body. They are kept apart from event parsing, policy
lookup, and channel delivery: a policy names a template, the dispatcher renders
one message per recipient, and the channel adapter sends it.

## Files

One directory, configured with `TEMPLATES_PATH`:

```text
config/templates/booking_confirmation.subject.tmpl
config/templates/booking_confirmation.body.tmpl
config/templates/booking_confirmation.html.tmpl   # optional
config/templates/_layout.html.tmpl                # shared HTML wrapper
config/templates/xsolla_lms.png                   # optional inline logo
```

The file name before the suffix is the template name a policy references.
Subject and plain body are required; the HTML file is optional. Templates are
parsed at startup; a malformed template, a missing subject or body file, an HTML
body without a layout, or a missing directory stops the worker.

## Data

Each template is executed with:

| Root | Contents |
| --- | --- |
| `.Event` | Envelope: `ID`, `Type`, `Version`, `OccurredAt`, `Producer`, `Data`. |
| `.Recipient` | The recipient snapshot: `ID`, `Role`, `Name`, `Email`. |
| `.Payload` | The event's `data` object, e.g. `.Payload.starts_at`. |

Example plain body:

```text
Hi {{.Recipient.Name}},

Your mentor slot is booked from {{.Payload.starts_at}} to {{.Payload.ends_at}}.
```

Missing data is an error, not an empty string: rendering fails with the
template name, the part (subject, body, or html), and the missing key. The
dispatcher renders every recipient before sending anything, so a template
failure never leaves a half-delivered event; the consumer retries and then
dead-letters it.

## HTML bodies

An HTML fragment holds only the card content (heading, greeting, details). The
shared `_layout.html.tmpl` wraps it with the page, preheader, CSS, logo, and
footer; render passes it `.Subject`, `.Content`, and `.Logo`.

The layout receives the fragment as trusted, already-escaped markup, but the
fragments themselves run through `html/template`. Every payload value is
therefore escaped for the context it appears in, so an LMS title or description
cannot inject markup.

When a message has an HTML body, the SMTP channel sends
`multipart/alternative`: the plain text first and the HTML last, so clients pick
the last part they can render. Clients that cannot render HTML still get the
plain text. Messages without an HTML body stay single-part `text/plain`.

If `xsolla_lms.png` sits next to the templates, the worker attaches it as an
inline CID image and the layout uses it as the logo; otherwise the layout falls
back to a text wordmark. Images are never loaded from a URL at send time.

## Status

`config/templates/` holds one subject/body pair and one HTML fragment per
policy: booking confirmation and cancellation, module created, content item
uploaded, event created, mentor availability created, submission created, and
enrollment graded.

No localization, dark-mode testing across every client, or hot reload yet.
Templates are re-read on restart, not per message.
