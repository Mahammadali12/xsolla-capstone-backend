# Notification policies

A policy decides what happens to one event type. Policies live only in the
notification service; the LMS publishes domain events whether or not a policy
exists.

`config/policies.json` is keyed by event type:

```json
{
  "mentor_slot.booked": {
    "enabled": true,
    "recipients": ["mentor", "student"],
    "channels": ["email"],
    "template": "booking_confirmation"
  }
}
```

| Field | Meaning |
| --- | --- |
| `enabled` | When false, matching events are acknowledged and ignored. |
| `recipients` | Recipient roles to notify: `student`, `mentor`, `lecturer`, `admin`. |
| `channels` | Delivery channels, e.g. `email`. |
| `template` | Message template name used to render the notification. |

The event type is the JSON key, so it is not repeated inside the entry.

## Validation

The worker refuses to start if the file is invalid:

- the key must look like `domain.event`;
- `recipients` may only use the four roles above;
- an enabled policy needs at least one recipient role, one non-blank channel,
  and a template;
- a disabled policy may be incomplete, so it can be staged before approval.

Channel names are not restricted yet; unknown channels fail when a channel
adapter handles the delivery.

## Behavior

- No policy, a disabled policy, or a policy without channels: the event is
  acknowledged and logged, with no delivery.
- An enabled policy whose roles match nobody in `data.recipients`: the event is
  treated as a failure, retried, then dead-lettered, so the mismatch is visible.
- Policies are read once at startup. Edit the file and restart the worker.
  Compose mounts `./config` read-only, so no rebuild is needed.

The example above is illustrative; the committed file is `{}` because no
notification scope has been approved yet.

No dynamic rules engine, conditions, preferences, or policy UI exists. Add
fields only when a real requirement needs them.
