# LMS Analytics --- Backend Specification

**Status:** Implementation specification\
**Audience:** Backend engineers\
**Product:** Xsolla School LMS\
**Access:** Administrators only

## 1. Purpose and scope

Implement an admin-only analytics API using the existing LMS backend and
PostgreSQL database.

The first release supports exactly these views:

1.  School overview: weekly student standing distribution.
2.  Student analytics: assignment submission status and quiz accuracy by
    module for one student.
3.  Mentor analytics: student-reported feedback averages by dimension
    and weekly standing distribution for one mentor's assigned students.

There is no team analytics dashboard. Team grades are already reflected
in the individual student standing assigned by mentors. Do not add
team-level aggregates for this release.

The analytics implementation should be a read-only reporting layer over
the existing LMS domain. Do not create a separate microservice or
database for this MVP.

## 2. Existing domain data to use

The implementation should map to the existing schema and repository
conventions. Based on the current project model, relevant data includes:

-   Users and roles: students, mentors, administrators; a user may hold
    multiple roles.
-   Modules: weekly program units.
-   Enrollments: student/module relationship and current categorical
    grade.
-   Assignments/content items: assignment metadata and deadlines.
-   Submissions: student submission records, submission timestamps,
    feedback, grader and grading timestamp.
-   Quizzes and quiz attempts: quiz/student relationship, answers,
    score, total, submission timestamp.
-   Mentor reviews: student-reported review for a module, mentor/team
    attribution at review submission time, session response, four
    dimension ratings, overall rating, comment, and submission
    timestamp.
-   Mentor slots: booked mentor time slots. A booking does not prove
    that a session occurred.

Before implementation, verify exact table/column names and existing
constraints against the migrations and store code. Do not create
duplicate tables or fields if the project already has equivalent
structures.

## 3. Architecture

Follow the existing backend layering:

**HTTP Handler → Analytics Service → Analytics Store → PostgreSQL**

Responsibilities:

### Handler

-   Register admin analytics routes.
-   Authenticate the request and enforce admin role.
-   Parse and validate query/path parameters.
-   Call the service.
-   Return consistent response DTOs and error formats.
-   Do not contain SQL or business aggregation logic.

### Service

-   Validate entity existence and eligibility.
-   Apply reporting-period defaults and constraints.
-   Coordinate store queries.
-   Enforce metric semantics that require more than one query.
-   Populate response metadata such as period and generated timestamp.
-   Do not duplicate authorization logic that belongs in middleware, but
    do enforce domain-level access constraints where appropriate.

### Store

-   Execute parameterized PostgreSQL queries.
-   Return aggregate rows, not raw quiz answers or large raw submission
    collections.
-   Use explicit period boundaries and stable ordering.
-   Keep SQL aggregation logic in the store layer.

### DTO

Create analytics request/response DTOs consistent with existing project
naming and JSON conventions. The frontend should receive pre-aggregated
data.

Suggested files, adjusted to match the repository's actual structure:

-   `internal/dto/analytics.go`
-   `internal/handlers/analytics.go`
-   `internal/service/analytics_service.go`
-   `internal/store/analytics_store.go`

Register routes using the existing router and middleware patterns.

## 4. Authorization and privacy

All analytics endpoints are admin-only.

-   Enforce authorization on the backend, not only in the frontend.
-   A request from a user without the `admin` role must receive the
    project's standard forbidden response.
-   Multi-role users are authorized if their role set includes `admin`.
-   Do not expose analytics endpoints to mentors, lecturers, or students
    in this release.
-   Do not return raw mentor review comments in these endpoints.
-   Do not return raw quiz answer JSON.
-   Avoid logging review content, answer payloads, or unnecessary
    personal data.
-   Apply the project's standard authentication, request validation,
    error handling, and rate limiting conventions.

Mentor review ratings are sensitive feedback. The admin-only policy is
intentional. If a minimum response threshold is later required,
implement it in the service/store contract rather than relying on the
frontend to hide low-count averages.

## 5. API design

Use the existing API prefix and response envelope conventions. The
following route names are proposed; adapt only to match established
project conventions.

### 5.1 School overview

`GET /admin/analytics/overview`

Query parameters: - `from` --- optional inclusive reporting start
date. - `to` --- optional inclusive reporting end date, or use a
documented exclusive upper bound convention consistently. - `module_ids`
--- optional only if multi-module filtering is needed; avoid adding it
unless the product requires it.

Returns weekly standing counts.

### 5.2 Student directory/search

`GET /admin/analytics/students`

Query parameters: - `q` --- optional search string. - `limit` and
`offset` or the project's standard cursor pagination parameters.

Returns only the fields needed by the selector: - student ID - display
name - optional email/student identifier if needed to disambiguate

Do not return full student profiles from this endpoint.

### 5.3 Student analytics

`GET /admin/analytics/students/{student_id}`

Query parameters: - `from` - `to` - optional `module_id`

Returns: - student identity summary - assignment submission status
counts - quiz accuracy by module

### 5.4 Mentor directory/search

`GET /admin/analytics/mentors`

Query parameters: - `q` - pagination parameters

Returns only mentor ID and display name, plus a minimal disambiguating
field if needed.

### 5.5 Mentor analytics

`GET /admin/analytics/mentors/{mentor_id}`

Query parameters: - `from` - `to`

Returns: - mentor identity summary - average student feedback by
dimension and response count - weekly standing distribution for students
attributed to the mentor during each period

Do not add team endpoints.

## 6. Metric definitions and query semantics

These definitions are part of the API contract. Frontend and backend
must not independently reinterpret them.

### 6.1 Weekly student standing distribution

For each module/reporting week, count enrolled students with a recorded
standing in each category:

-   `green_count`
-   `yellow_count`
-   `red_count`
-   `total_count`

Recommended denominator:

`total_count = green_count + yellow_count + red_count`

This means the denominator is students with a recorded standing for that
period, not all currently active users. Return it explicitly.

Rules: - Use the grade recorded for that specific student/module
period. - Do not infer a grade from assignment submissions, quiz
accuracy, or other activity. - Do not convert Green/Yellow/Red into
numeric values or average them. - Missing/null grades are excluded from
all three category counts and from `total_count`. - Do not classify
missing grades as Red. - Return a row for a period with no recorded
grades only if the product wants explicit empty weeks; if returned, all
counts and total are zero and the frontend must render it as "No
recorded grades." - Sort periods chronologically. - Use stable
module/week ordering from the modules table, not lexical ordering of
labels.

**Important schema limitation:** The current `enrollments` model stores
the current grade, not necessarily a grade history. If an enrollment row
is overwritten each week, it cannot answer historical weekly
distribution correctly. Historical reporting requires a grade history
table or equivalent immutable records. Do not pretend the current grade
is a historical weekly grade.

### 6.2 Assignment submission status for one student

Return three counts:

-   `on_time_count`
-   `late_count`
-   `missing_count`

Also return: - `eligible_count` - optionally `no_deadline_count` only if
product explicitly wants to expose it.

Define the population as eligible student-assignment pairs whose
deadline has passed within the requested reporting period.

Classification: - **On time:** at least one qualifying submission exists
and the first submission timestamp is less than or equal to the
deadline. - **Late:** a qualifying submission exists, but the first
submission timestamp is after the deadline. - **Missing:** no qualifying
submission exists by the reporting cutoff and the assignment deadline
has passed.

Rules: - Exclude future-due assignments. - Exclude assignments for which
the student was not enrolled/eligible. - Exclude assignments without a
deadline from the three-way deadline-based classification unless product
approves a separate "No deadline" category. - Use the assignment's
authoritative deadline and timezone convention. Store and compare
timestamps consistently, preferably UTC, with display conversion handled
by the client. - Define how deleted/archived assignments and submissions
are treated according to existing domain rules. - If multiple
submissions are allowed, classify deadline compliance using the first
submission timestamp. Do not allow a later resubmission to turn an
originally late submission into an on-time submission. - If the system
stores only the latest submission, it cannot reliably implement
first-submission classification. Confirm the schema and add submission
history if needed. - Ensure each eligible student-assignment pair is
counted exactly once, even if it has multiple submission rows.

Recommended SQL approach: 1. Build eligible assignments for the selected
student and period. 2. Aggregate submissions to one row per
student/assignment using `MIN(submitted_at)` for first submission. 3.
Left join those aggregates to eligible assignments. 4. Classify with a
`CASE` expression. 5. Aggregate counts.

Do not calculate these counts by fetching all submissions into Go.

### 6.3 Quiz accuracy by module

For each module, return: - `correct_total` (or `score_total` if score
represents points) - `question_total` (or `possible_total`) -
`accuracy` - `attempt_count`

Recommended calculation:

`accuracy = SUM(score) / NULLIF(SUM(total), 0) * 100`

Rules: - Aggregate numerator and denominator across attempts, rather
than averaging per-quiz percentages. - Use the actual semantics of
`score` and `total`. If they represent points, do not name them question
counts. - Exclude attempts that are invalid, deleted, or outside the
requested reporting period according to existing domain rules. - Modules
with no attempts should return no accuracy value, not 0%. - Protect
against division by zero. - Respect the existing uniqueness rule for
`(quiz_id, student_id)` if one attempt per quiz/student is enforced. -
Do not return answer JSON.

### 6.4 Mentor feedback by dimension

For the selected mentor, calculate the arithmetic mean for each of the
four student-reported dimensions:

-   `rating_clarity`
-   `rating_engagement`
-   `rating_comfort`
-   `rating_time`

Return for each dimension: - dimension key - average rating - response
count

Rules: - Include only submitted, valid mentor reviews attributed to the
selected mentor. - Respect the review's submitted timestamp and
requested reporting period. - Exclude null ratings from that dimension's
average and count. - Validate rating values against the defined 1--5
range. - Compute each dimension's average using its own non-null
response count. Counts may differ by dimension. - Return
`average = null` and `count = 0` when no valid rating exists. - Do not
substitute the overall rating for a missing dimension. - Do not return
free-text comments or raw review rows in this API. - If a privacy
threshold is approved, suppress averages below that threshold and return
a suppression indicator; do not silently return a misleading value.

The `overall` rating is not one of the four requested dimensions and
should not be added to the chart in this release.

### 6.5 Mentor's student standing distribution

For each week/module, aggregate Green/Yellow/Red standing among students
assigned to the selected mentor for that reporting period.

Required attribution: - Use the mentor-to-student relationship valid for
the reporting period. - Use the student's grade for that same period. -
Count each student at most once per period. - Return category counts and
total denominator for each period.

**Critical schema limitation:** The current team membership/assignment
data appears to represent current membership and may not preserve
historical mentor assignments. A mentor's current five students cannot
automatically be treated as that mentor's students in prior weeks.

If historical mentor attribution is not stored, choose one of these
explicitly: 1. Implement a mentor/student assignment history table and
backfill only periods that can be reliably established; or 2. Return a
current-roster-only chart and label it as such, without implying
historical attribution.

Do not silently attribute all historical grades to the mentor who
currently owns the student.

Mentor reviews include mentor/team attribution at review submission
time, which can help attribute feedback. That does not, by itself,
establish assignment history for every student's standing.

## 7. Data model changes required for correctness

### 7.1 Grade history

If `enrollments.grade` is overwritten as each module is assessed, add
immutable grade history.

Suggested table:

``` sql
CREATE TABLE enrollment_grade_history (
    id UUID PRIMARY KEY,
    enrollment_id UUID NOT NULL REFERENCES enrollments(id),
    student_id UUID NOT NULL REFERENCES users(id),
    module_id UUID NOT NULL REFERENCES modules(id),
    grade TEXT NOT NULL CHECK (grade IN ('green', 'yellow', 'red')),
    graded_by UUID REFERENCES users(id),
    graded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_grade_history_module_graded_at
    ON enrollment_grade_history (module_id, graded_at);

CREATE INDEX idx_grade_history_student_module
    ON enrollment_grade_history (student_id, module_id);
```

Adjust IDs, names, constraints, and foreign keys to the actual schema.

Write path: - When a mentor changes a student's grade for a module,
update the current enrollment grade and insert the history row in the
same database transaction. - Do not update or delete prior history rows
during normal grade edits. - Decide whether a grade edit creates a new
history event or replaces an existing assessment event. Recommended:
preserve each change as an audit event; if the product needs one final
grade per module, expose the latest valid event for the distribution
while retaining the audit trail. - Backfill only if historical values
can be reconstructed reliably. Do not manufacture past grades from
current values.

If the product already stores one immutable enrollment row per
student/module and never overwrites grades, verify that this is
sufficient before adding another table.

### 7.2 Mentor/student assignment history

If the application changes which mentor owns a student and the current
relationship is not historically versioned, add an effective-dated
assignment table.

Suggested table:

``` sql
CREATE TABLE student_mentor_assignments (
    id UUID PRIMARY KEY,
    student_id UUID NOT NULL REFERENCES users(id),
    mentor_id UUID NOT NULL REFERENCES users(id),
    valid_from TIMESTAMPTZ NOT NULL,
    valid_to TIMESTAMPTZ,
    changed_by UUID REFERENCES users(id),
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (valid_to IS NULL OR valid_to > valid_from)
);

CREATE INDEX idx_student_mentor_assignments_student_period
    ON student_mentor_assignments (student_id, valid_from, valid_to);

CREATE INDEX idx_student_mentor_assignments_mentor_period
    ON student_mentor_assignments (mentor_id, valid_from, valid_to);
```

Enforce at most one active mentor assignment per student if that is the
product rule. A PostgreSQL exclusion constraint or a partial unique
strategy may be appropriate depending on whether overlapping historical
ranges are allowed.

Attribution rule for a reporting period: the assignment interval must
overlap the period according to the agreed effective-date semantics. If
a student can change mentors mid-week, product must decide whether the
weekly grade belongs to the mentor assigned at the time the grade was
given or the mentor assigned for most of that week. Prefer the mentor
recorded on the grade event (`graded_by` plus assignment context) if the
business rule is "the grading mentor owns the assessment." Otherwise
define an explicit period rule.

Do not assume `mentor_reviews.mentor_id` is a complete substitute for
this table.

### 7.3 Submission history check

Verify whether `submissions` preserves all attempts or only the latest
submission per student/content item. If only the latest row is retained,
add an immutable submission event/history model or otherwise preserve
the first submitted timestamp. Correct on-time/late analytics depend on
it.

### 7.4 Session attendance

No session attendance model is required for this release. `mentor_slots`
represents bookings and cannot be used as proof that a meeting occurred.
Do not add session-completion analytics based on booked slots.

## 8. Query and performance requirements

-   Use PostgreSQL aggregation, joins, CTEs, and indexes. Do not load
    raw data into Go for aggregation.
-   Use parameterized SQL for all user-supplied filters.
-   Avoid N+1 queries when building charts.
-   Query only the requested period and entity.
-   Use `EXPLAIN (ANALYZE, BUFFERS)` against realistic data before
    adding indexes indiscriminately.
-   Add indexes based on actual query plans. Likely candidates include:
    -   grade history by module and assessment time
    -   grade history by student and module
    -   submissions by student/content item and submission timestamp
    -   quiz attempts by student/quiz and submitted timestamp
    -   mentor reviews by mentor and submitted timestamp
    -   mentor assignment history by mentor/student and validity
        interval
-   If current tables are small, ordinary indexed queries are
    sufficient. Do not introduce materialized views, Redis, a reporting
    database, or a separate analytics service for the MVP.
-   Add caching only if measured latency justifies it. Define
    invalidation for grade edits, submissions, quiz attempts, and mentor
    reviews.

## 9. Response DTOs

The following are illustrative. Align naming and envelopes with the
existing API conventions.

### 9.1 Overview response

``` json
{
  "period": {
    "from": "2026-09-01",
    "to": "2026-11-14"
  },
  "generated_at": "2026-11-14T17:00:00Z",
  "weeks": [
    {
      "module_id": "module-id",
      "label": "Week 4",
      "green_count": 3,
      "yellow_count": 1,
      "red_count": 1,
      "total_count": 5
    }
  ]
}
```

### 9.2 Student analytics response

``` json
{
  "student": {
    "id": "student-id",
    "name": "Student Name"
  },
  "period": {
    "from": "2026-09-01",
    "to": "2026-11-14"
  },
  "generated_at": "2026-11-14T17:00:00Z",
  "assignment_status": {
    "on_time_count": 4,
    "late_count": 1,
    "missing_count": 2,
    "eligible_count": 7
  },
  "quiz_accuracy_by_module": [
    {
      "module_id": "module-id",
      "label": "Week 4",
      "score_total": 16,
      "possible_total": 20,
      "accuracy": 80.0,
      "attempt_count": 2
    }
  ]
}
```

### 9.3 Mentor analytics response

``` json
{
  "mentor": {
    "id": "mentor-id",
    "name": "Mentor Name"
  },
  "period": {
    "from": "2026-09-01",
    "to": "2026-11-14"
  },
  "generated_at": "2026-11-14T17:00:00Z",
  "feedback_by_dimension": [
    {
      "dimension": "clarity",
      "average": 4.4,
      "count": 5
    },
    {
      "dimension": "engagement",
      "average": 4.2,
      "count": 5
    },
    {
      "dimension": "comfort",
      "average": 4.8,
      "count": 4
    },
    {
      "dimension": "time_management",
      "average": 4.0,
      "count": 5
    }
  ],
  "student_standing_by_week": [
    {
      "module_id": "module-id",
      "label": "Week 4",
      "green_count": 3,
      "yellow_count": 1,
      "red_count": 1,
      "total_count": 5
    }
  ],
  "standing_attribution": "historical"
}
```

`standing_attribution` should make the historical/current-roster
limitation explicit, for example `historical` or `current_roster`. Use
an enum defined in the API contract.

For averages with no data, return `average: null` and `count: 0`.

## 10. Validation and error behavior

-   Invalid date ranges: return the standard validation error.
-   `from` after `to`: reject.
-   Excessively large date ranges: cap or reject according to an agreed
    maximum.
-   Unknown student/mentor IDs: return the standard not-found response.
-   A valid user without the expected student/mentor role: return not
    found or validation error according to the project's
    resource-disclosure policy.
-   Unauthorized caller: standard forbidden response.
-   Empty analytics dataset: return a successful response with empty
    arrays and zero counts where the metric has a valid denominator; do
    not treat empty data as an internal error.
-   Database failure: return the standard internal error and log a
    request/correlation ID without sensitive payloads.

## 11. Testing requirements

### Unit tests

-   Date range validation and defaults.
-   Grade category aggregation.
-   Null/missing grade handling.
-   Assignment deadline classification.
-   First-submission handling when multiple submissions exist.
-   Exclusion of future and ineligible assignments.
-   Quiz accuracy uses sum(score)/sum(total), not average of
    percentages.
-   Zero-total quiz attempts do not cause division by zero.
-   Per-dimension mentor averages exclude null ratings and use
    independent counts.
-   Empty dimensions return null average and zero count.
-   Mentor assignment attribution across changes.
-   Current-roster mode is explicitly identified if historical
    attribution is unavailable.

### Store/integration tests

Use a test PostgreSQL database and cover: - Student with on-time, late,
and missing assignments. - Assignment with future deadline. - Assignment
without deadline. - Student with no quiz attempts in a module. -
Multiple quizzes with different totals to verify weighted aggregation. -
Mentor with different response counts for each dimension. - Mentor with
no reviews. - Student changing mentors between reporting periods. -
Missing standing records. - Grade changes across weeks.

### Authorization tests

-   Admin can access all analytics routes.
-   Student cannot access.
-   Mentor cannot access.
-   Lecturer without admin role cannot access.
-   Multi-role mentor/admin can access.
-   Direct API calls receive the same authorization enforcement as UI
    navigation.

## 12. Implementation sequence

1.  Verify the actual schema and determine whether grade history,
    submission history, and mentor assignment history are available.
2.  Agree on metric definitions and reporting-period boundaries with the
    product owner.
3.  Implement DTOs and admin-only route registration.
4.  Implement store queries and PostgreSQL integration tests.
5.  Implement service orchestration and validation.
6.  Add grade history write-path changes if historical weekly standing
    is not currently persisted.
7.  Add mentor assignment history if historical mentor attribution is
    required and not currently persisted.
8.  Implement student/mentor search endpoints.
9.  Implement API integration tests and authorization tests.
10. Publish the API contract to the frontend team.

## 13. Decisions required before implementation

These are real data-definition decisions, not frontend styling choices:

1.  **Historical grades:** Does each student/module have a persistent
    grade record, or is one enrollment grade overwritten over time?
2.  **Mentor attribution:** Should a student's weekly standing belong to
    the mentor who assigned the grade, or the mentor assigned to the
    student during that week?
3.  **Submission attempts:** Does the database retain every submission
    or only the latest? The "first submission" rule requires the former
    or a preserved first timestamp.
4.  **Reporting range:** What is the default number of weeks shown, and
    should the end date include the current incomplete week?
5.  **Feedback privacy:** Should mentor averages be hidden below a
    minimum response count? If yes, specify the threshold.
6.  **Assignment eligibility:** Which enrollment/status rules determine
    whether a student was expected to submit an assignment?

Do not silently make assumptions on these points. Record the agreed
decisions in the API contract.

## 14. Explicitly out of scope

-   Team analytics endpoints or dashboard.
-   Mentor leaderboard/ranking.
-   Composite student risk score.
-   Composite mentor score.
-   Attendance/session completion metrics.
-   Booked-slot-as-attendance inference.
-   Review comment browsing.
-   Raw quiz-answer exposure.
-   CSV/PDF exports.
-   Scheduled alerts or notifications.
-   Separate analytics microservice/database.
-   Materialized views or data warehouse before performance evidence
    requires them.
