# LMS Analytics — AI Feedback Summary Backend Specification

**Status:** Implementation specification  
**Audience:** Backend engineers  
**Product:** Xsolla School LMS  
**Access:** Administrators only  
**Scope:** AI-generated summaries for individual student and mentor analytics pages

---

## 1. Purpose

Add AI-generated plain-text summaries to the bottom of:

1. **Each student analytics page** — summarize feedback that the student has received from mentors/graders through assignment submissions.
2. **Each mentor analytics page** — summarize student-submitted mentor reviews, including both free-text comments and multiple-choice/structured answers.

There is **no AI summary on the school overview dashboard**.

The backend retrieves the relevant feedback from PostgreSQL, prepares a safe and bounded input, calls the configured AI provider, and returns the generated summary. The frontend must not send underlying feedback records to the AI endpoint.

The API response is JSON containing a single plain-text `summary` field plus minimal metadata. The frontend does not receive raw feedback records, prompts, model request payloads, or provider credentials.

## 2. Scope and source data

### 2.1 Student summary — feedback received by a student

Use the feedback text stored on the student's assignment submission records.

Relevant source data:
- Submission/student relationship.
- Assignment/content-item title.
- Module/week label.
- Submission feedback text.
- `graded_at` (or the authoritative timestamp for when the feedback was provided).

Only non-empty feedback should be included.

The summary should identify recurring feedback themes, strengths, areas to improve, and concrete suggestions that appear in the feedback. It must not invent feedback or infer performance from unrelated data.

**Important distinction:** This endpoint summarizes feedback given *to the student*. It must not summarize the student's quiz answers, mentor reviews they submitted about their mentor, or the student's Green/Yellow/Red standing unless the product explicitly expands the scope later.

If the current schema stores only the latest feedback for a submission and overwrites older feedback, the endpoint can summarize only the feedback currently persisted. It cannot reconstruct overwritten feedback.

### 2.2 Mentor summary — feedback submitted about a mentor

Use the student's submitted mentor-review records attributed to the selected mentor.

Based on the current `mentor_reviews` domain, include these available fields:

**Free-text input**
- `comment`

**Structured / multiple-choice input**
- `had_session`
- `no_session_reason` (when `had_session` is false)
- `topics`
- `rating_clarity`
- `rating_engagement`
- `rating_comfort`
- `rating_time`
- `overall`

The four dimension ratings use a 1–5 scale. `overall` is a separate overall rating and must not be confused with one of the four dimensions.

Verify the actual database types and permitted values before implementation. For example, `topics` may be a selection list or another structured value; preserve its actual meaning rather than treating it as arbitrary prose.

Only submitted reviews attributed to the selected mentor should be included. Use the review's `submitted_at` timestamp for date filtering.

The summary should combine:
- recurring themes and suggestions from written comments;
- positive and negative patterns in the four dimension ratings;
- the overall rating distribution or average;
- recurring selected topics;
- session/no-session responses and selected reasons, where available.

Do not treat a booked mentor slot as proof that a session happened. For this summary, `had_session` is the student's self-reported answer in the review.

### 2.3 Data that must not be sent to the model

Do not include:
- student names, email addresses, phone numbers, or other direct identifiers;
- mentor names or contact information;
- user IDs, database IDs, tokens, or authentication data;
- raw quiz-answer JSON;
- unrelated profile information;
- internal notes that are not part of the feedback being summarized.

Use module/assignment labels and review content as needed for context. If the AI provider does not require identifying details, do not send them.

## 3. API endpoints

Both endpoints are admin-only and generate a summary on demand.

### 3.1 Generate a student feedback summary

`POST /admin/analytics/students/{student_id}/ai-summary`

Purpose:
- Fetch the selected student's assignment feedback.
- Generate a concise summary.
- Return the summary as plain text inside a JSON response.

Request body:

```json
{
  "from": "2026-09-01",
  "to": "2026-11-16",
  "force_refresh": false
}
```

Fields:
- `from` — optional start date, inclusive.
- `to` — optional end boundary, exclusive.
- `force_refresh` — optional boolean. When true, bypass a valid cached summary and generate a new one.

When dates are omitted, use the same reporting-period defaults as the selected student analytics page. The frontend should pass the active dashboard date filter so the summary and charts describe the same period.

The endpoint must fetch feedback from the database itself. Do not accept a list of feedback records or arbitrary prompt text from the client.

### 3.2 Generate a mentor feedback summary

`POST /admin/analytics/mentors/{mentor_id}/ai-summary`

Purpose:
- Fetch student-submitted reviews for the selected mentor.
- Include both free-text and structured review answers.
- Generate a concise summary.
- Return the summary as plain text inside a JSON response.

Request body:

```json
{
  "from": "2026-09-01",
  "to": "2026-11-16",
  "force_refresh": false
}
```

Fields have the same semantics as the student endpoint. The frontend should pass the active mentor analytics date filter.

The endpoint must fetch reviews from the database itself. Do not accept review records, comments, selected answers, or arbitrary prompt text from the client.

### 3.3 Response format

Both endpoints return the same response envelope:

```json
{
  "summary": "Students generally describe the mentor as clear and approachable. Comments frequently appreciate the practical explanations and willingness to answer questions. The ratings are strongest for comfort, while engagement has more mixed responses. A useful improvement would be to make meeting time more consistent and ensure that each session ends with clear next steps.",
  "source_count": 5,
  "generated_at": "2026-11-16T08:30:00Z",
  "cached": false,
  "period": {
    "from": "2026-09-01",
    "to": "2026-11-16"
  }
}
```

The example summary is illustrative only; actual content must be generated from retrieved records.

Response fields:
- `summary` — the plain-text summary. This is the only AI-generated content the frontend should render.
- `source_count` — number of feedback records included in generation.
- `generated_at` — timestamp of generation or cached-summary creation.
- `cached` — whether the response was served from a valid cached result.
- `period` — effective reporting period used.

The value of `summary` must be plain text: no HTML, Markdown formatting, code blocks, embedded JSON, links, or fabricated names/quotations. A few short paragraphs are acceptable. Keep the output concise and suitable for the bottom section of an analytics page.

If the existing API uses a standard response envelope, keep that envelope and place the fields above within it. Do not introduce a new response convention for these routes.

## 4. Summary content requirements

### 4.1 Student summary

The model summarizes only the retrieved assignment feedback.

Expected content:
- recurring strengths identified by graders;
- recurring areas for improvement;
- repeated advice or actionable next steps;
- changes over time only when dated feedback genuinely supports a trend.

Do not:
- create a student risk score;
- assign a Green/Yellow/Red grade;
- infer quiz performance from assignment feedback;
- claim improvement or decline based on one isolated comment;
- treat missing feedback as negative feedback;
- invent actions that were not suggested or reasonably implied by the comments.

If feedback is mixed, represent both positive and critical observations.

### 4.2 Mentor summary

The model combines free-text and structured mentor-review answers.

Expected content:
- recurring positive themes in comments;
- recurring improvement suggestions or concerns;
- relative strengths and weaker areas across Clarity, Engagement, Comfort, and Time management;
- overall rating pattern;
- frequently selected topics;
- reported session/no-session pattern and recurring reasons, when enough responses exist.

The four dimensions must retain their exact meanings:
- Clarity
- Engagement
- Comfort
- Time management

Rating interpretation:
- The scale is 1–5.
- Include response counts when interpreting averages.
- Do not call a dimension strong or weak based on one response without noting the very small sample.
- Do not infer causation between a rating and student performance.
- Do not compare mentors or rank them.

The summary must distinguish student-reported experience from objectively verified session attendance. For example, describe `had_session` as “students reported having a session,” not “the mentor conducted a session.”

## 5. Backend architecture

Follow the existing backend architecture:

**HTTP Handler → Analytics Summary Service → Feedback Store → AI Provider Adapter**

### Handler
- Register both routes.
- Enforce administrator authorization.
- Validate path parameters and request body.
- Call the summary service.
- Return the agreed JSON response.
- Do not contain SQL, prompt construction, or provider-specific logic.

### Analytics summary service
- Resolve and validate the selected student or mentor.
- Resolve the effective reporting period.
- Retrieve the relevant feedback records from the store.
- Normalize and validate feedback values.
- Return a no-feedback response without calling the AI provider when no usable records exist.
- Build the prompt/input payload.
- Enforce input-size limits.
- Check the summary cache.
- Call the AI provider adapter when generation is required.
- Validate and persist the returned summary.
- Return response metadata.

### Feedback store
- Retrieve only records for the requested entity and reporting period.
- Use parameterized SQL.
- Return minimal fields needed for summarization.
- Avoid fetching unrelated student or mentor profile data.

### AI provider adapter
- Hide provider-specific SDK/API details from the service.
- Accept a system instruction and normalized feedback payload.
- Return generated text plus provider/model metadata.
- Support timeouts and bounded retries.
- Never expose provider credentials or raw provider responses to the frontend.

Suggested files, adjusted to repository conventions:

- `internal/handlers/analytics_summary.go`
- `internal/service/analytics_summary_service.go`
- `internal/store/analytics_feedback_store.go`
- `internal/ai/provider.go`
- `internal/ai/<configured_provider>.go`
- `internal/dto/analytics_summary.go`

Do not create a separate microservice for this feature.

## 6. Retrieval and normalization

### 6.1 Student feedback retrieval

The store query should:
1. Select submissions belonging to the requested student.
2. Join assignment/content-item and module metadata for context.
3. Select non-null, non-empty feedback.
4. Filter by the authoritative feedback timestamp, preferably `graded_at`.
5. Order records chronologically.
6. Return only fields required by the summarizer.

Normalized record example:

```json
{
  "module": "Week 4",
  "assignment": "REST API implementation",
  "feedback_date": "2026-10-22",
  "feedback": "The implementation is clear, but error handling is incomplete. Add explicit handling for invalid IDs and database failures."
}
```

Do not include student ID, grader ID, or student contact information in the model input.

If feedback can be edited, use the currently persisted feedback and its authoritative update/graded timestamp. Do not claim to have a full feedback history if the database does not preserve it.

### 6.2 Mentor review retrieval

The store query should:
1. Select submitted reviews where `mentor_id` matches the selected mentor.
2. Filter by `submitted_at`.
3. Select the text comment and all relevant structured answers.
4. Exclude invalid or null rating values from rating calculations.
5. Order source records chronologically.
6. Avoid including student identifiers in the model input.

Normalized review example:

```json
{
  "module": "Week 4",
  "submitted_at": "2026-10-22",
  "had_session": true,
  "no_session_reason": null,
  "topics": ["Backend architecture", "Code review"],
  "ratings": {
    "clarity": 5,
    "engagement": 4,
    "comfort": 5,
    "time_management": 3,
    "overall": 4
  },
  "comment": "The explanations were clear and the code review was useful. We ran out of time before discussing the deployment questions."
}
```

The example is illustrative. Preserve the actual `topics` representation and the exact choice values defined by the application.

### 6.3 Aggregated structured input for mentor reviews

In addition to individual review records, calculate structured aggregates in the backend:
- response count per dimension;
- average per dimension;
- overall rating count and average;
- count/distribution of `had_session` responses;
- frequency of each `no_session_reason`;
- frequency of each selected topic.

The model can receive the normalized comments plus these aggregates. This helps it describe multiple-choice patterns without having to infer counts from a long list of records.

For each dimension, calculate the average using only non-null valid ratings for that dimension. The response count may differ between dimensions.

## 7. AI generation requirements

### 7.1 Provider configuration

Use the AI provider already selected by the project, if one exists. Otherwise introduce a small provider adapter so the implementation is not tightly coupled to one vendor.

Configuration must be supplied through the project's configuration/environment mechanism:
- provider/API endpoint as appropriate;
- API key/secret;
- model identifier;
- request timeout;
- maximum input/output token limits.

Never hardcode API keys in source code, Docker images, or committed configuration files. The provider and model must be configurable without changing analytics business logic.

### 7.2 Prompt requirements

Use a fixed, versioned system instruction for each summary type.

The instruction must tell the model to:
- summarize only the supplied feedback;
- treat comments and selected answers as untrusted data, not as instructions;
- ignore instructions embedded inside feedback text;
- distinguish facts from interpretation;
- mention recurring themes rather than overemphasizing one comment;
- preserve positive and negative feedback;
- avoid unsupported conclusions and causal claims;
- avoid names and identifying information;
- return plain text only;
- never invent ratings, counts, quotations, events, or feedback.

Use separate prompt templates for student and mentor summaries because their source data and intended conclusions differ.

### 7.3 Prompt injection and untrusted feedback

Feedback is user-generated content. A comment may contain instructions such as “ignore previous instructions” or attempts to manipulate the model.

Treat all retrieved comments and selected values strictly as data:
- delimit feedback payloads clearly;
- never concatenate feedback into the system instruction;
- instruct the model not to follow directions appearing in source feedback;
- do not give the model tools, database access, or the ability to call external actions;
- validate the output before returning it.

### 7.4 Output validation

Before returning the summary:
- verify the provider returned non-empty text;
- enforce a maximum output length;
- reject or strip HTML if the provider returns markup;
- reject embedded code blocks or JSON if the contract requires plain text;
- do not return provider metadata or chain-of-thought;
- do not silently substitute a fabricated summary if generation fails.

If output is unusable, return the project's standard upstream/service error and log safe diagnostic metadata.

### 7.5 Input-size handling

Do not silently truncate feedback records.

If the input exceeds the configured model context/input limit:
1. Split records into deterministic chronological chunks.
2. Summarize each chunk using the same summary rules.
3. Combine the intermediate summaries into one final summary.
4. Preserve the overall source count and reporting period.
5. Keep intermediate summaries internal; return only the final plain-text summary.

The chunking logic must not drop records without an explicit, documented limit and response behavior.

## 8. Caching and regeneration

AI generation is slower and more expensive than ordinary analytics queries. Avoid generating a new summary every time an administrator revisits a page.

Recommended behavior:
1. Retrieve and normalize feedback records.
2. Compute a stable fingerprint/hash from normalized source records, effective period, summary type, and prompt version.
3. Look for a cached summary with the same entity, period, source fingerprint, and prompt version.
4. Return the cached summary when one exists and `force_refresh` is false.
5. Otherwise generate and store a new summary.

This ensures that:
- new submission feedback invalidates the student's previous summary;
- new mentor reviews invalidate the mentor's previous summary;
- edits to existing feedback invalidate the relevant summary;
- a prompt-version change can trigger regeneration;
- administrators can explicitly request regeneration with `force_refresh`.

The frontend should not implement its own content-based cache or decide whether a summary is stale.

### 8.1 Suggested cache table

A dedicated cache table is recommended if the project does not already have a suitable cache mechanism.

```sql
CREATE TABLE analytics_ai_summaries (
    id UUID PRIMARY KEY,
    subject_type TEXT NOT NULL
        CHECK (subject_type IN ('student', 'mentor')),
    subject_id UUID NOT NULL,
    period_from DATE,
    period_to DATE,
    source_fingerprint TEXT NOT NULL,
    prompt_version TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    summary_text TEXT NOT NULL,
    source_count INTEGER NOT NULL,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_analytics_ai_summaries_subject
    ON analytics_ai_summaries (
        subject_type,
        subject_id,
        period_from,
        period_to,
        generated_at DESC
    );
```

Adapt UUID types and naming to the existing database schema.

Do not store raw comments or the complete prompt payload in this table. Store the generated summary and non-sensitive provenance metadata only.

Use a unique constraint or transactional upsert strategy to avoid duplicate cache rows for the same subject, period, source fingerprint, and prompt version.

### 8.2 Concurrent requests

If two administrators request the same summary at the same time:
- avoid unnecessary duplicate model calls where practical;
- use a short-lived per-key lock or equivalent coordination;
- ensure the cache remains consistent;
- do not block unrelated summaries for other students or mentors.

## 9. Empty data and errors

### 9.1 No feedback exists

If a student has no non-empty submission feedback, do not call the model.

Return a successful response with a deterministic plain-text message, for example:

```json
{
  "summary": "There is not enough written assignment feedback to generate a summary yet.",
  "source_count": 0,
  "generated_at": null,
  "cached": false,
  "period": {
    "from": "2026-09-01",
    "to": "2026-11-16"
  }
}
```

If a mentor has no submitted reviews, return an equivalent message. Do not infer that the mentor has poor or good performance.

If reviews exist but all comments are empty, structured answers may still support a mentor summary. Generate from structured answers when valid structured responses exist.

If no usable text or structured responses exist, return the no-feedback message without calling the model.

### 9.2 Errors

Use the project's standard error envelope and HTTP status conventions.

Expected cases:
- `400` — invalid date range or malformed request.
- `401` — unauthenticated request.
- `403` — authenticated user is not an admin.
- `404` — selected student/mentor does not exist or does not have the expected role, following the project's resource-disclosure convention.
- `429` — generation rate limit reached, if rate limiting is applied.
- `502` or the project's equivalent — AI provider returned an invalid response or failed upstream.
- `503` — AI generation service is unavailable.
- `500` — unexpected internal failure.

A provider failure must not be converted into a successful response containing an invented fallback summary. The frontend should keep the rest of the analytics page usable and show an error state only in the AI summary section.

## 10. Security, privacy, and operational controls

- Enforce admin authorization on both endpoints at the backend.
- Do not trust frontend route guards as authorization.
- Do not return raw feedback records in either response.
- Do not expose prompts, provider payloads, API keys, or provider error bodies to the client.
- Do not log raw comments or full model input/output by default.
- Log entity type, entity ID where permitted, source count, duration, cache hit/miss, model identifier, prompt version, and correlation/request ID.
- Apply request rate limits and reasonable date-range/input-size limits.
- Set a provider timeout and bounded retry policy.
- Handle provider unavailability without blocking the underlying student/mentor analytics endpoints.
- Confirm the project's privacy/data-processing requirements before sending student feedback to an external AI provider. If an external provider is used, use its approved data-handling configuration.

## 11. Testing requirements

### 11.1 Student summary tests

- Student with several written submission feedback records.
- Student with no feedback.
- Student with empty/whitespace-only feedback.
- Feedback filtered correctly by the reporting period.
- Feedback records ordered deterministically.
- Assignment/module context is included.
- No student name, email, ID, or grader ID is included in model input.
- New or edited feedback changes the source fingerprint.
- Cached summary is reused when source data and prompt version have not changed.
- `force_refresh` causes regeneration.

### 11.2 Mentor summary tests

- Mentor with comments and complete structured ratings.
- Mentor with comments but no structured ratings.
- Mentor with structured ratings but no comments.
- Different response counts for different rating dimensions.
- Null/invalid ratings excluded from the corresponding aggregate.
- `had_session=false` uses `no_session_reason` only where applicable.
- Topic and reason values are counted according to their actual stored representation.
- Reviews belonging to another mentor are excluded.
- Reviews outside the requested period are excluded.
- No student identifiers appear in model input.
- Empty review set does not call the AI provider.
- New or edited review changes the source fingerprint.

### 11.3 AI adapter and resilience tests

- Provider returns valid plain text.
- Provider times out.
- Provider returns an empty response.
- Provider returns HTML, Markdown, or JSON unexpectedly.
- Provider returns output exceeding the configured limit.
- Provider is unavailable.
- Oversized input is chunked without dropping source records.
- Feedback text containing prompt-injection instructions is treated as data.
- Duplicate concurrent requests do not cause avoidable duplicate generations.

### 11.4 Authorization tests

- Admin can call both endpoints.
- Student cannot call either endpoint.
- Mentor cannot call either endpoint.
- Lecturer without admin role cannot call either endpoint.
- Multi-role user with admin role can call both endpoints.
- Direct API calls enforce the same restrictions as the UI.

## 12. Implementation sequence

1. Confirm exact source columns and relationships for submission feedback and mentor reviews.
2. Confirm the AI provider and approved data-handling configuration.
3. Agree on the effective default reporting period with the frontend team.
4. Define DTOs and register the two admin-only endpoints.
5. Implement store queries and normalization for student feedback.
6. Implement store queries and structured aggregates for mentor reviews.
7. Implement the provider adapter and separate versioned prompts.
8. Implement input-size handling and output validation.
9. Implement summary fingerprinting and cache persistence.
10. Add service, store, provider, cache, and authorization tests.
11. Publish the API contract to the frontend team.

## 13. Explicitly out of scope

- AI summary for the school overview dashboard.
- AI summary for teams.
- Mentor leaderboard or mentor ranking.
- Student or mentor composite score.
- Student risk prediction.
- Recommendations that automatically change grades or mentor assignments.
- AI-generated grading of assignments or quizzes.
- AI-generated feedback sent directly to students or mentors.
- Review-comment browsing endpoint.
- Returning raw feedback records to the frontend through these summary endpoints.
- Chatbot or follow-up conversation with the generated summary.
- AI summaries for lectures, attendance, or booked mentor slots.
