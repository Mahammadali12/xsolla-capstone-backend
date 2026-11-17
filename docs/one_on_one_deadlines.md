# Deadlines follow the 1:1, and late work turns the week red

A student's work for a week is due by **08:00 on the day of their 1:1** with their mentor, so the mentor can look at it
before the meeting. The backend works this out; the frontend only shows it.

Lecturers do **not** set a deadline. `due_at` on an assignment is optional and ignored for students.

## Mentor 1:1 times

Mentors open 1:1 slots on **Friday and Monday** (school time, Asia/Baku). `POST /mentor-slots` refuses any other day,
and a window that runs past midnight. If a mentor forgets to open Friday slots, only Monday is left to choose from.

## The deadline

The 1:1 weekend belongs to the **module**: from the first Friday on or after the day the module opens (`opens_at`)
to the Tuesday after, 00:00.

| The student's 1:1 in that weekend | Their deadline |
| --- | --- |
| Friday 12:30 | Friday 08:00 |
| Monday 14:30 | Monday 08:00 |
| On another day of that weekend | 08:00 on that day |
| Several | 08:00 on the day of the earliest |
| Not booked yet (or cancelled) | Monday 08:00 |

The last row means nobody is late only because they have not booked yet: a student who hands in on Saturday and books a
Monday 1:1 on Sunday is on time. The deadline is worked out each time it is read, so booking, cancelling or moving the
1:1 moves it at once. The module's `closes_at` plays no part.

## Late work

Work is accepted at any time (before the module **opens** it is refused: `403 module_not_open`). A hand-in is late like
this:

- the **first** hand-in is late when it comes after the deadline;
- a **later** hand-in (a change) is late when it comes after the deadline **and before the 1:1 is over** (the mentor may
  already have read the earlier work). With no 1:1 booked, the 1:1 counts as over at the end of the weekend (Tuesday
  00:00). A change after the 1:1, for example after the feedback, is **not** late;
- a task is late when any of its hand-ins is late.

A late hand-in **turns the student's grade for that week red** by itself (`enrollments.grade = 'red'`, `graded_by`
empty). A mentor or admin can change it afterwards through `PATCH /modules/{moduleId}/enrollments/{userId}/grade`, for an
exceptional case. Only a new late hand-in turns it red again.

## In the API

`GET /modules/{moduleId}/content-items` and `GET /content-items/{id}`, for a **student**, every assignment has:

```json
{
  "id": 12, "type": "assignment", "title": "Routing app",
  "due_at": "2026-11-23T04:00:00Z",
  "due_from": "no_one_on_one"
}
```

- `due_at`: the student's own deadline.
- `due_from`: `one_on_one` (08:00 on the day of their 1:1) or `no_one_on_one` (no 1:1 booked: Monday 08:00).
- `task_due_at`: only when the task still carries a deadline of its own (older tasks).

Admins, lecturers and mentors get no student deadline on the task.

Submissions (`POST .../submissions`, `GET .../submissions`, `GET .../submissions/me` and its `history`) carry:

- `due_at`: the student's deadline for the task;
- `late`: this hand-in was late. On the **newest** hand-in it says the **task** is late (any of its hand-ins was).

The analytics `assignment_status` (`on_time_count`, `late_count`, `missing_count`) uses the same rule and the student's
own deadline. Only assignments whose deadline falls in the period, and hand-ins before its end, count.

## Not covered

- Booking a Friday slot after 08:00 on that Friday makes the deadline one that has already passed. The slot table does
  not record when a slot was booked.
- The late flag is worked out on every read from the booked slots, so moving a 1:1 afterwards can change it. The week
  colour is set when the late hand-in is made and is not undone.
