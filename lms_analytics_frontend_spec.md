# LMS Analytics --- Frontend Specification

**Status:** Implementation specification\
**Audience:** Frontend engineers\
**Product:** Xsolla School LMS\
**Access:** Administrators only

## 1. Purpose and scope

Build an administrator-only analytics area that gives school
administrators a reliable view of student standing, individual student
performance, and individual mentor feedback/performance.

The first release contains exactly three dashboard views:

1.  **School overview** --- weekly student standing distribution.
2.  **Student analytics** --- assignment submission status and quiz
    accuracy by module for one selected student.
3.  **Mentor analytics** --- student-reported feedback by mentorship
    dimension and weekly standing distribution for the mentor's assigned
    students.

There is no team analytics dashboard. Team-level standing is already
reflected by the grades mentors assign to students; a separate team
dashboard would duplicate that signal and is out of scope.

Do not add other charts, KPIs, rankings, risk scores, exports, or
dashboards without product approval.

## 2. Roles and access

-   The analytics navigation item and all analytics routes are visible
    only to users with the `admin` role.
-   Frontend route guards are for navigation and user experience only.
    Backend authorization remains authoritative.
-   Non-admin users must not be able to load analytics data by directly
    calling frontend routes or API endpoints.
-   A user with multiple roles may access analytics only if their
    authenticated role set includes `admin`.

## 3. Navigation and information architecture

Add an **Analytics** entry to the admin navigation.

Suggested route structure:

-   `/admin/analytics` --- school overview
-   `/admin/analytics/students` --- student selector/directory and
    selected student analytics
-   `/admin/analytics/mentors` --- mentor selector/directory and
    selected mentor analytics

The student and mentor pages should support selecting an entity from a
searchable list. A selected entity should be represented in the URL
where the existing routing conventions permit it, for example
`/admin/analytics/students/:studentId` and
`/admin/analytics/mentors/:mentorId`.

Use the existing admin layout, navigation configuration, routing
conventions, design tokens, charting library, and API client. Do not
introduce a second UI framework or charting dependency if the project
already has an adequate one.

## 4. Shared page behavior

### 4.1 Filters

Provide only filters that can be applied consistently to the relevant
data:

-   **Date/week range:** limits the reporting period.
-   **Module:** available on student analytics where applicable.
-   **Mentor:** available on student analytics only if the backend can
    resolve the mentor attribution for the selected period reliably.

The school overview and mentor standing chart are week-based. The
student page can use the selected module and reporting period to scope
assignment and quiz data.

Do not show a team filter in this release.

If the current schema cannot attribute historical student records to the
correct mentor, do not imply that a current mentor filter provides
historical attribution. Use the current assignment only with a clear
label, or defer that filter until mentor assignment history exists.

### 4.2 Common states

Every dashboard and chart must support:

-   Initial loading state.
-   Empty state with a clear explanation.
-   Error state with retry.
-   Partial-data state when one chart succeeds and another fails.
-   Updated-at timestamp, based on the backend response.
-   Accessible labels and non-color-only status indicators.

Do not render fabricated zeroes while data is loading or when the API
returns unavailable data. Distinguish "no records" from "failed to
load."

### 4.3 Chart interaction

-   Tooltips must show the exact value and denominator where meaningful.
-   Legends must identify each series/category.
-   Charts must remain understandable without relying only on color.
-   Where practical, clicking a chart segment or point should apply the
    corresponding filter or navigate to the relevant student/mentor
    detail. Do not implement drill-downs that the API cannot support.
-   Provide a compact data table or accessible textual equivalent for
    each chart.

## 5. Dashboard specifications

## 5.1 School overview

**Route:** `/admin/analytics`

**Purpose:** Show how the school's student standing is distributed
across weeks.

### Chart: Weekly student standing distribution

**Visualization:** Stacked bar chart.

**X-axis:** Week/module period, in chronological order.

**Y-axis:** Number of students.

**Series:** - Green - Yellow - Red

Each bar represents one reporting week. The three segments represent the
count of students whose recorded standing for that week is Green,
Yellow, or Red.

**Tooltip:** - Week/module label. - Standing category. - Student
count. - Total students included in that week. - Percentage of that
week's included students.

**Legend:** Green, Yellow, Red, with text labels.

**Required behavior:** - Default to the most recent reporting period
supported by the API, with enough preceding weeks to show a useful
trend. The default range should be configurable by the product team; use
the backend's supported range rather than hardcoding a number of weeks
in the frontend. - Allow the administrator to change the week/date
range. - Preserve chronological order. - Weeks with no recorded grades
must be represented as missing/empty data, not as zero students in all
categories. - If the population changes between weeks, display each
week's denominator in the tooltip. Do not silently compare percentages
as if the cohort were fixed. - Do not combine Green, Yellow, and Red
into a numeric average or "school score."

**Data contract expectation:** Each point provides a period
identifier/label, counts for each standing, and the total number of
students included. The API must define whether the population is active
students, enrolled students, or students with a recorded grade for that
period. The recommended chart denominator is students with a recorded
standing for that week; display the denominator so missing grades remain
visible as a limitation.

## 5.2 Student analytics

**Route:** `/admin/analytics/students` and selected-student detail
route.

**Purpose:** Allow an administrator to inspect the assignment and quiz
activity of one student.

### Student selection

Provide a searchable student selector. Search results should identify
students sufficiently to disambiguate them, for example by full name and
email or student identifier, subject to existing admin UI conventions.

The page must not load charts until a student is selected. If the route
contains an invalid or unavailable student ID, show a not-found state.

Display the selected student's name and relevant existing identifiers.
Do not expose unnecessary personal information.

### Chart A: Assignment submission status

**Visualization:** Donut chart or horizontal stacked bar. Prefer a donut
only if the number of categories remains small and labels remain
readable; otherwise use a horizontal stacked bar.

**Categories:** - Submitted on time - Submitted late - Not submitted /
missing

Use the backend's explicit status classification. The frontend must not
infer status solely from `submitted_at` if deadlines, eligibility, or
multiple submission attempts need domain logic.

**Tooltip/legend details:** - Count per status. - Total eligible
assignments included. - Percentage of eligible assignments. - For
missing work, count only assignments whose deadline has passed and for
which the student was expected to submit.

**Required behavior:** - Exclude assignments not yet due from the
denominator and chart. - Exclude assignments for which the student was
not eligible/enrolled. - An assignment submitted before or at its
deadline is "Submitted on time." - An assignment first submitted after
its deadline is "Submitted late." - An assignment with no qualifying
submission after its deadline is "Not submitted / missing." - The
backend must define how resubmissions are handled. Recommended rule:
classify based on the first submission timestamp for deadline
compliance, while showing the latest submission separately only if the
product later requires it. - If an assignment has no deadline, do not
classify it as on-time or late. It should be excluded from this
deadline-based chart or surfaced as a separately identified "No
deadline" category only after product approval.

### Chart B: Quiz accuracy by module

**Visualization:** Bar chart.

**X-axis:** Module/week.

**Y-axis:** Accuracy percentage, from 0% to 100%.

**Value:** Total correct answers divided by total attempted/scored
questions for that student in that module, multiplied by 100.

**Tooltip:** - Module name/week. - Correct answers. - Total scored
answers. - Accuracy percentage. - Number of quiz attempts included.

**Required behavior:** - Aggregate all eligible quiz attempts for the
selected student in a module using `sum(score) / sum(total)`, not the
unweighted average of quiz percentages. - Do not treat a module with no
quiz attempts as 0% accuracy. Show no data for that module. - Do not
include quizzes that are not yet available/due if the product's quiz
rules define them as ineligible. - If the existing data model permits
only one attempt per student per quiz, use that recorded attempt; do not
invent attempt history. - If `score` and `total` are not question counts
but points, label the chart "Score accuracy" or "Points earned"
according to the actual data semantics. Do not label points as
questions. - Keep the Y-axis fixed at 0--100%.

## 5.3 Mentor analytics

**Route:** `/admin/analytics/mentors` and selected-mentor detail route.

**Purpose:** Allow an administrator to inspect student-reported
mentorship feedback and the weekly standing distribution of the mentor's
assigned students.

### Mentor selection

Provide a searchable mentor selector. A mentor is a user who has the
mentor role. Users may have multiple roles; role membership must be
handled by the backend.

Display mentor name and only the identifying details needed to
distinguish mentors.

### Chart A: Student feedback by dimension

**Visualization:** Four horizontal bars or a compact column chart.

**Dimensions (1--5 scale):** - Clarity - Engagement - Comfort - Time
management

These are the four student-reported mentorship dimensions stored by
mentor reviews.

**Value:** Arithmetic mean of valid student ratings for each dimension,
on a 1--5 scale.

**Required chart details:** - Show the average rating to one decimal
place. - Show the number of submitted ratings (`n`) for each
dimension. - Keep the axis fixed from 1 to 5, or use a clearly marked
0--5 axis if required by the existing charting component. Never visually
exaggerate small differences with an unlabeled truncated axis. - A
dimension with no ratings must show "No responses," not 0. - If
dimensions have different response counts, display each dimension's own
`n`. - Do not display identifiable review comments in this chart. - Do
not expose review details to mentors. This analytics area is admin-only.

**Privacy behavior:** - The chart is available only to admins. -
Consider a minimum response threshold before showing averages for very
small samples. The threshold is a product/privacy decision; until
agreed, show `n` prominently and do not claim that a small sample is
representative. - Any future comments view must be separately specified
with privacy rules. It is not part of this release.

### Chart B: Mentor's student standing distribution

**Visualization:** Stacked bar chart.

**X-axis:** Week/module period, chronological.

**Y-axis:** Number of students.

**Series:** Green, Yellow, Red.

**Population:** The selected mentor's assigned students for the
reporting period.

**Tooltip:** - Week/module label. - Standing category. - Student
count. - Total students included for that mentor and week. - Percentage
of that week's included students.

**Required behavior:** - Use the mentor-to-student assignment applicable
to the reporting period, not merely today's assignment, if historical
attribution is available. - The chart must include only the selected
mentor's students, not the whole school. - If the mentor has five
assigned students, the total for a fully populated week will normally be
five, but the chart must not hardcode five. Display the actual
denominator. - If historical mentor assignment cannot be determined
because the database stores only current membership, the backend must
either: 1. return only a clearly labelled current-roster view, or 2.
defer historical weeks until assignment history is implemented. Do not
present current students' past grades as if they were necessarily under
the selected mentor at that time. - Missing standing records must not be
counted as Red. - Do not create a team dashboard or team comparison
chart.

## 6. Data presentation and terminology

Use the existing standing terminology exactly: **Green**, **Yellow**,
and **Red**. These are categorical states, not numerical scores.

Keep the following concepts separate: - Student standing:
mentor-assigned weekly categorical grade. - Assignment submission
status: on-time, late, or missing. - Quiz accuracy: performance on
scored quiz attempts. - Mentor feedback: student-reported ratings across
four dimensions.

Do not combine these into one student or mentor score.

## 7. Frontend data contracts

The frontend should consume pre-aggregated API responses. It should not
download raw submissions, quiz answers, or all student records to
calculate analytics in the browser.

Expected response shapes (illustrative; align field naming with existing
API conventions):

### School overview

``` json
{
  "period": { "from": "2026-09-01", "to": "2026-11-14" },
  "updated_at": "2026-11-14T17:00:00Z",
  "weeks": [
    {
      "module_id": "module-id",
      "label": "Week 4",
      "green": 3,
      "yellow": 1,
      "red": 1,
      "total": 5
    }
  ]
}
```

### Student analytics

``` json
{
  "student": { "id": "student-id", "name": "Student Name" },
  "period": { "from": "2026-09-01", "to": "2026-11-14" },
  "updated_at": "2026-11-14T17:00:00Z",
  "assignment_status": {
    "on_time": 4,
    "late": 1,
    "missing": 2,
    "eligible_total": 7
  },
  "quiz_accuracy_by_module": [
    {
      "module_id": "module-id",
      "label": "Week 4",
      "correct": 16,
      "total": 20,
      "accuracy": 80,
      "attempt_count": 2
    }
  ]
}
```

### Mentor analytics

``` json
{
  "mentor": { "id": "mentor-id", "name": "Mentor Name" },
  "period": { "from": "2026-09-01", "to": "2026-11-14" },
  "updated_at": "2026-11-14T17:00:00Z",
  "feedback_by_dimension": [
    { "dimension": "clarity", "average": 4.4, "count": 5 },
    { "dimension": "engagement", "average": 4.2, "count": 5 },
    { "dimension": "comfort", "average": 4.8, "count": 4 },
    { "dimension": "time_management", "average": 4.0, "count": 5 }
  ],
  "student_standing_by_week": [
    {
      "module_id": "module-id",
      "label": "Week 4",
      "green": 3,
      "yellow": 1,
      "red": 1,
      "total": 5
    }
  ]
}
```

These are contract examples, not a requirement to copy exact JSON names.
The frontend and backend teams must agree on one stable schema before
implementation.

## 8. Non-functional requirements

-   Responsive layout consistent with the existing admin panel.
-   Charts must remain legible at the supported desktop and tablet
    breakpoints.
-   Use server-side pagination for student and mentor selectors if the
    population is large.
-   Debounce search requests using the project's existing request
    utilities.
-   Cancel or ignore stale requests when selection changes quickly.
-   Cache only where consistent with the project's existing query/cache
    strategy; invalidate or refetch after relevant data changes.
-   Do not log sensitive review content or raw quiz answers in the
    browser console.
-   Use accessible color contrast and textual status labels.

## 9. Acceptance criteria

### School overview

-   Admin can open the overview and see weekly Green/Yellow/Red counts.
-   The chart is chronological and each week exposes its denominator.
-   Missing grades are not represented as Red or as zero-valued standing
    categories.
-   Date/week filtering updates the chart.

### Student analytics

-   Admin can search for and select a student.
-   Assignment chart correctly separates on-time, late, and missing
    eligible work.
-   Future assignments and ineligible assignments are excluded.
-   Quiz chart shows accuracy per module using aggregated correct/total
    values.
-   Modules without attempts display no data rather than 0%.

### Mentor analytics

-   Admin can search for and select a mentor.
-   Feedback chart shows the four dimension averages and per-dimension
    response counts.
-   Empty dimensions display "No responses."
-   Standing chart includes only the mentor's attributed students for
    each period, with actual denominators.
-   If historical attribution is unavailable, the UI clearly labels the
    data as current-roster or the historical view is withheld.

### Authorization

-   Non-admin users cannot access analytics routes or retrieve analytics
    data.
-   Backend authorization errors are handled cleanly by the frontend.

## 10. Explicitly out of scope

-   Team dashboard or team comparison charts.
-   Mentor leaderboard or ranking.
-   Composite student risk score.
-   Composite mentor score.
-   Attendance or actual meeting completion analytics.
-   Mentor comments/review text browsing.
-   CSV/PDF exports.
-   Notifications or automated alerts.
-   Student/mentor-facing analytics pages.
-   Real-time streaming updates.
