# Task deadlines follow the 1:1

A student's work is due by **08:00 on the day of their 1:1** with their mentor, so the mentor can look at it before
the meeting. Mentors open 1:1 slots on Friday and Monday.

## The rule

It applies to tasks (assignments) whose own deadline falls on a 1:1 weekend: **Friday to Monday**, in school time
(Asia/Baku). The task form's default deadline is Friday 08:00, so that is most tasks. A task due Tuesday to Thursday
keeps its own deadline.

| The student's 1:1 that Friday to Monday | Their deadline |
| --- | --- |
| Friday 12:30 | Friday 08:00 |
| Monday 14:30 | Monday 08:00 |
| On another day of that weekend | 08:00 on that day |
| Several | 08:00 on the day of the earliest |
| Not booked yet (or cancelled) | Monday 08:00 |

The last row means nobody is late only because they have not booked yet: a student who hands in on Saturday and
books a Monday 1:1 on Sunday is on time.

The deadline is worked out each time it is read, from the student's booked slots (`mentor_slots.booked_by`), so
booking, cancelling or moving a 1:1 moves it at once. Nothing new is stored.

## In the API

`GET /modules/{moduleId}/content-items` and `GET /content-items/{id}`, for a **student**:

```json
{
  "id": 12, "type": "assignment", "title": "Routing app",
  "due_at": "2026-11-23T04:00:00Z",
  "task_due_at": "2026-11-20T04:00:00Z",
  "due_from": "no_one_on_one"
}
```

- `due_at`: the student's own deadline. Everything that already uses `due_at` (overdue labels, closing the hand-in)
  follows it.
- `task_due_at`: the deadline the lecturer set.
- `due_from`: `one_on_one` (08:00 on the day of their 1:1) or `no_one_on_one` (no 1:1 booked: Monday 08:00).

`task_due_at` and `due_from` are left out for tasks that keep their own deadline, and for admins, lecturers and
mentors, who always see the lecturer's deadline.

## Not covered

- Booking a Friday slot after 08:00 on that Friday makes the deadline one that has already passed. The slot table
  does not record when a slot was booked, so this case cannot be told apart yet.
- The server does not refuse late hand-ins (it never did). The frontend closes the hand-in at `due_at`, and also at
  the module's `closes_at`, so a week must close after Monday 08:00 for Monday deadlines to work.
- The analytics "on time / late" counts still use the lecturer's deadline.
