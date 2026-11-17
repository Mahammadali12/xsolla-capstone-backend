# Delete all users (admin)

One request deletes every student, mentor and lecturer, for example to clear a demo or start a new cohort. Admins are
never deleted, so the LMS can still be used afterwards.

## `DELETE /users?confirm=true` (admin only)

| Query | Meaning |
| --- | --- |
| `confirm` | **Required**, must be `true`. Without it nothing happens (`422`), so a stray call deletes nobody. |
| `role` | Optional: `student`, `mentor` or `lecturer` deletes only the non-admins holding that role. Without it, every non-admin is deleted. |

```json
{ "deleted": 42, "by_role": { "student": 38, "mentor": 3, "lecturer": 1 } }
```

`by_role` counts how many of the deleted users held each role: a user with two roles counts under both, and a user
with no role counts under `none`. When nobody matches: `{ "deleted": 0, "by_role": {} }`.

Errors: `422` without `confirm=true` or with another `role` (`admin` included), `403` for anyone who is not an admin,
`401` when not signed in.

## What is deleted and what stays

It is the same as deleting each user with `DELETE /users/{id}`, all in one transaction (all or nothing).

- **Never deleted:** anyone who holds the admin role (also with other roles, e.g. an admin who is also a mentor), and
  the admin who sends the request.
- **Deleted with the users:** their accounts, roles and sign-ins, enrollments, hand-ins, quiz answers, team places,
  profiles and settings, mentor reviews, lecture feedback, and the 1:1 slots of deleted mentors. 1:1s they booked
  are freed.
- **Stays, handed to the admin who sent the request:** the weeks (modules), uploaded files and quizzes they created.
  Tasks and materials stay in their weeks (without an author). Grades given by a deleted mentor stay, without the
  grader. Reports stay, with `reporter: null`.
- **Files in storage (Supabase) are not deleted.** Only database rows are.

This cannot be undone. Ask for a confirmation in the frontend before sending it.

## Also fixed

`DELETE /users/{id}` failed with a server error for a user who had created a quiz (`quizzes.created_by` had no
handling). The quiz is now handed to the admin, like the user's weeks and files.
