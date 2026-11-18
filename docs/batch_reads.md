# Combined reads

The course pages used to ask for every week separately: content items, quizzes and enrollments once per week, and
the student's hand-in once per task. With 12 weeks that was about 90 requests. These routes answer the same data
for many weeks (or tasks) in **one** request each.

| Combined route | Per-week route it replaces |
| --- | --- |
| `GET /content-items?module_ids=1,2,3` | `GET /modules/{moduleId}/content-items` |
| `GET /quizzes?module_ids=1,2,3` | `GET /modules/{moduleId}/quizzes` |
| `GET /enrollments?module_ids=1,2,3` | `GET /modules/{moduleId}/enrollments` |
| `GET /submissions/me?assignment_ids=41,42` | `GET /modules/{moduleId}/content-items/{assignmentId}/submissions/me` |

Any signed-in user. The per-week routes stay as they are.

## The answer

An object keyed by the week id (task id for hand-ins). Each value is **exactly** what the per-week route answers for
that id, with the same rules: a student gets only visible items, their own deadlines and only their own enrollment
row; a mentor or lecturer is refused quizzes where the per-week route refuses them; and so on.

```json
{
  "4": [{ "id": 41, "type": "assignment", "title": "CLI tool", "due_at": "2026-11-23T04:00:00Z" }],
  "5": []
}
```

An id the per-week route would answer with `403` or `404` (not enrolled, not allowed, unknown week, no hand-in yet) is
**left out** of the object. A week that exists but has nothing in it is there with `[]`.

## Errors

- `400`: the ids are missing, not a comma-separated list of positive numbers, or more than 200.
- `401`: not signed in.
- `500`: a database error for any of the ids (nothing partial is sent).

The server still reads each week on its own inside the request (the same code as the per-week route), so the rules
cannot drift apart; what is saved is the round trips between the browser and the server.
