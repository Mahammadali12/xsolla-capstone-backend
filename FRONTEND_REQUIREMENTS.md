# Frontend requirements: Google Meet sync, attendees, and the user's Google Calendar

**Audience:** frontend developer working on `xsolla-capstone-frontend`.

**Written against:**

- Backend `a0db7d0` (includes `8530d5c` attendees, `94bd61d` Meet links on events/1:1s, `7444d24` Google Calendar event import, `a0db7d0` unrestricted event reads).
- Frontend `6b81b4d` (the latest code reviewed for this document).

**Scope:** the LMS calendar (`/calendar`), the event form, the 1:1 slot UI, and Settings/Google connection. `/admin/calendar` is intentionally **not** part of this work.

**What this document gives you:** the exact backend behaviour and wire formats, the flow for each feature, a file-by-file checklist of what the latest frontend still needs, and edge cases/tests. It is a requirements document, not a patch.

---

## 0. Summary: what needs to be built

| # | Feature | Backend status | Frontend work |
|---|---|---|---|
| 1 | Creating an LMS event can also create it in the creator's Google Calendar, with a Google Meet link | Done | Mostly exists; fix clearing `meeting_url`, update stale copy, verify the Meet link and Google marker |
| 2 | Events and 1:1 slots carry attendees | Done | **Add** attendee types/mappers/UI; render names only |
| 3 | The user's own Google Calendar events appear on the LMS calendar | Done | **Add** the Google events client, merge/dedupe, read-only UI, legend, hints |
| — | `GET /events` no longer filters by role/enrollment | Done | Do not filter events by role on the client; be aware students now see all events |

Current frontend (as of `6b81b4d`) already has the Google connection UI (`GoogleCard`, `GoogleProvider`, `useGoogle`, `api/google.ts`), the “Also add to my Google Calendar” checkbox (`GoogleSyncField`), the `sync_to_google` create flow (`events.http.ts`), the “On Google Calendar” badge, and `JoinMeeting`. Use those; do not rebuild them.

---

## 1. Configuration, authentication, and the Google connection

### 1.1 Backend URL

- Real backend: `VITE_API_URL` is set (e.g. `http://localhost:8080`).
- Offline demo: `VITE_API_URL` is empty; the app uses the mock APIs in `src/mocks/`.

All requests below assume the real backend.

### 1.2 LMS authentication

- `Authorization: Bearer <access_token>` on every protected endpoint.
- The existing `authedRequest` in `src/api/authed.ts` already refreshes once on 401 and clears the session when refresh fails. Keep using it.
- Error body shape from every endpoint:

```json
{
  "error": "google_not_connected",
  "message": "Google account not connected",
  "status": 404,
  "details": []
}
```

`details` is optional; `message` is human-readable. Do not show raw error codes.

### 1.3 Google connection

The backend owns the Google OAuth tokens (encrypted at rest). The frontend never sees or stores Google tokens.

Flow:

1. `GET /auth/google/me` (Bearer) tells whether the signed-in person is connected:
   - `200` → connected; response is the Google profile (`id`, `email`, `name`, `picture`, `verified_email`).
   - `404 google_not_connected` → never connected.
   - `401 google_token_expired` → token revoked/expired; user must reconnect.
   - `502 upstream_error` → Google could not be reached; unknown state.
   - `500` → server problem; unknown state.
2. `GET /auth/google/login?access_token=<JWT>` → `302` to Google's consent screen. This is a **page navigation**, so the JWT travels in the query string. The current `connectUrl()` already refreshes the access token before navigating.
3. Google redirects to `GET /auth/google/callback`; the backend stores the encrypted refresh token and redirects the browser to `GOOGLE_OAUTH_FRONTEND_REDIRECT_URI` (default `http://localhost:5173/settings`) with `?google=connected` or `?google=error`.
4. The Settings page reads that query once, shows the result, removes it from the address, and reloads the connection status. This already exists in `GoogleCard`.

Scopes requested by the backend:

- `https://www.googleapis.com/auth/calendar.events`
- `https://www.googleapis.com/auth/gmail.send`
- `openid email`

Reading the primary calendar's events uses `calendar.events`, so **no new scope or re-consent is needed** for users who already connected. Connecting from Settings is available to every role (the latest frontend already has `/settings` open to all and shows Settings in the account menu).

Use `POST /auth/google/revoke` to disconnect: it revokes the grant at Google (the app disappears from `https://myaccount.google.com/permissions`) and then removes the stored token, answering `204`. If Google is unreachable the LMS still forgets the token and answers `204`, logging the failed revoke. The Settings card needs only one Disconnect action; no “remove access at Google” link is required. `POST /auth/google/logout` remains for forgetting the stored token without revoking at Google.

---

## 2. API conventions

- Timestamps for LMS events/slots are RFC 3339 strings (e.g. `2026-11-19T10:00:00+04:00`).
- IDs:
  - LMS events: JSON number (`id: 15`). Accept number or numeric string defensively (the current code does).
  - Mentor slots: JSON string (`id: "13"`).
  - Google events: opaque strings (e.g. `pjhnfhl6s0l57phunlc3fm11p8`; recurring instances may contain `_`).
- Fields with `omitempty` (`location`, `description`, `meeting_url`, `module_id`, `google_event_id`, `booked_by`, `booked_by_name`) are absent when empty, not `null`.
- `attendees` on event/slot responses is always an array (possibly empty), never omitted.
- `all_day` is always present on events and Google events.

---

## 3. Feature 1 — create an event in the LMS and in Google Calendar (with a Meet link)

### 3.1 What the backend does

`POST /events` accepts `sync_to_google: true` (lecturer/admin only). For a **new** event:

1. The event is saved in the LMS.
2. If `sync_to_google` is true, the backend creates a Google Calendar event on the **creator's** calendar (`conferenceDataVersion=1`) and asks Google to generate a **Google Meet** conference.
3. The backend stores `google_event_id` and the Meet link in `meeting_url` on the LMS event.
4. If the event belongs to a module, the module's creator + enrolled users are sent as Google invitees (`sendUpdates=all`). A module-less event still gets a Meet link, but has no invitees.
5. A pasted `meeting_url` is replaced by Google's generated Meet link when Google returns one; if Google returns none, the pasted link is kept (`COALESCE` in the update).
6. If Google fails, the LMS event is still created; `google_event_id` is absent and the failure is only in the server log. The frontend must warn, not roll back.

Later:

- `PATCH /events/:id` on an event that has `google_event_id` pushes title/time/description/attendees to Google (non-fatal). `sync_to_google` is **not** part of PATCH and cannot add a Google copy later.
- `DELETE /events/:id` deletes the Google copy when `google_event_id` exists (non-fatal).

Meeting-link validation: `meeting_url` must start with `http://` or `https://`; otherwise `400 bad_request`.

### 3.2 Create request

```json
{
  "title": "Weekly lecture",
  "kind": "lecture",
  "starts_at": "2026-11-19T10:00:00+04:00",
  "ends_at": "2026-11-19T12:00:00+04:00",
  "all_day": false,
  "location": "Main hall",
  "description": "Module 5 kick-off",
  "meeting_url": "https://example.com/backup-link",
  "module_id": 5,
  "sync_to_google": true
}
```

`sync_to_google` is optional and defaults to false. `module_id` is optional. `kind` is one of `lecture`, `meeting`, `off-campus` (the LMS calendar's editable kinds).

### 3.3 Response (`EventResponse`)

```json
{
  "id": 15,
  "title": "Weekly lecture",
  "kind": "lecture",
  "starts_at": "2026-11-19T10:00:00+04:00",
  "ends_at": "2026-11-19T12:00:00+04:00",
  "all_day": false,
  "location": "Main hall",
  "description": "Module 5 kick-off",
  "meeting_url": "https://meet.google.com/fix-pwcp-yzu",
  "module_id": 5,
  "google_event_id": "pjhnfhl6s0l57phunlc3fm11p8",
  "attendees": [
    { "id": 1, "name": "Admin", "email": "admin@edu.com" },
    { "id": 5, "name": "User user", "email": "user1@example.com" }
  ],
  "created_at": "2026-11-12T16:30:25.561218Z"
}
```

### 3.4 Clearing a meeting link on edit

`PATCH /events/:id` treats the field as follows:

| `meeting_url` in the body | Result |
|---|---|
| omitted | keep the current link |
| `null` | keep the current link (JSON `null` → Go nil pointer) |
| `""` | **clear** the link |
| `"https://..."` | set/replace the link |

**Required frontend change:** the latest `eventToApi` sends `meeting_url: e.meetingUrl ?? null`. That means removing the link in the form does **not** clear it. Send `""` instead:

```ts
meeting_url: e.meetingUrl ?? '',
```

This mirrors how `location` and `description` are already sent.

### 3.5 Required frontend work

- Keep `GoogleSyncField`, `sync_to_google` on create, and the “Only on create” rule. Do not show the checkbox when editing.
- Keep mapping `meeting_url` → `CalendarEvent.meetingUrl` and `google_event_id` → `CalendarEvent.googleEventId`.
- Fix `eventToApi` to send `""` for an absent link (see above).
- Show the generated Meet link with the existing `JoinMeeting` component. It already opens the link 30 minutes before the start for students and always for staff.
- After create/edit, refetch `GET /events` (the existing `saveEvent` does) so the Google-generated link and `google_event_id` appear.
- Mark LMS-origin events that are on Google with the existing “On Google Calendar” badge (only when `google_event_id` is present).
- Show the non-fatal warning when Google sync failed: the event is saved, `google_event_id` is absent. The current `savedNotice` logic is right, but **update its stale text**: `meetingLinkLost` says “The server does not store meeting links yet” — the server now does. Reword to “The server did not keep the meeting link” (or similar).
- Never send `google_event_id` or `attendees` back in POST/PATCH. They are server-derived/read-only.
- Synced events are deletable/editable as normal; the backend follows the change to Google.

### 3.6 Acceptance

- Ticking “Also add to my Google Calendar” and saving a new event returns/refetches an event with a `https://meet.google.com/...` `meeting_url` and a `google_event_id`; the card shows the Join link and the Google badge.
- Sync failure: the event exists, no `google_event_id`, and the UI warns without losing the event.
- Editing a synced event keeps working (no checkbox, Google copy follows).
- Deleting a synced event removes the LMS event and attempts to remove the Google copy.
- Removing the meeting link in the edit form and saving clears it on the LMS event.
- A module-less event can sync and gets a Meet link, but no invitees.

---

## 4. Feature 2 — attendees

### 4.1 Event attendees

Every event with a `module_id` returns the **module creator + every enrolled user**, de-duplicated by user id. Events without a module return `[]`.

```json
"attendees": [
  { "id": 1, "name": "Admin", "email": "admin@edu.com" },
  { "id": 5, "name": "User user", "email": "user1@example.com" }
]
```

This is attached to `GET /events` responses as well as create/update responses.

### 4.2 1:1 slot attendees

`GET/POST/DELETE /mentor-slots*` responses include attendees:

- The mentor is always present.
- The booked student is present only for the mentor, an admin, and the student themselves.
- Other students see only the mentor; the slot still says `booked: true`, but `booked_by`, `booked_by_name`, `meeting_url`, and the student attendee are omitted.

```json
{
  "id": "13",
  "mentor_id": 2,
  "mentor_name": "Rashid Aliyev",
  "starts_at": "2026-11-20T15:00:00Z",
  "ends_at": "2026-11-20T15:30:00Z",
  "booked": true,
  "booked_by": 5,
  "booked_by_name": "User user",
  "meeting_url": "https://meet.google.com/abc-defg-hij",
  "attendees": [
    { "id": 2, "name": "Rashid Aliyev", "email": "mentor@gmail.com" },
    { "id": 5, "name": "User user", "email": "user1@example.com" }
  ]
}
```

When a booking is made or cancelled, the attendee list changes accordingly (the student appears/disappears). If the mentor has no Google connection, the booking still succeeds but `meeting_url` is absent.

### 4.3 Required frontend work

Add these types/fields:

```ts
export interface Attendee {
  id: number
  name: string
  email: string
}
```

- `ApiEvent.attendees?: ApiAttendee[] | null`
- `CalendarEvent.attendees?: Attendee[]`
- `MentorSlotResponse.attendees?: ApiAttendee[] | null`
- `MeetingSlot.attendees?: Attendee[]`
- `MeetingSlot.meetingUrl?: string`

Also:

- `mapEvent`: map `attendees` when present (`Number(id)` defensively); omit the field when empty.
- `toMeetingSlot`: map `attendees` and `meeting_url`; export it if useful for tests.
- `slotEvents`: attach `attendees` and `meetingUrl` to the meeting cards it derives for the mentor's booked 1:1 and the student's own booking. The availability summary stays without attendees.
- Event card (`DayPanel`): show a collapsible “Attendees (N)” disclosure with **names only**, e.g. a native `<details>`; no emails, no profile links. Handle missing/empty arrays by rendering nothing.
- Slot UI (`Slots.tsx`): a booked slot can show the attendee names/mentor + booked student, but the backend already hides the other student's identity — do not try to infer it. The mentor's view may already show `booked_by_name`; attendees add the mentor and are optional there.
- Do not send `attendees` in POST/PATCH bodies; the backend derives them.
- Do not render emails. They are in the API payload (all authenticated users receive them for all events, because `GET /events` is unrestricted), but the UI requirement is names only.

### 4.4 Acceptance

- A lecture with a module shows its creator and enrolled users on the event card; a module-less event shows no attendee section.
- A mentor's booked slot shows mentor + student; the booking student sees the same; a different student sees only the mentor and cannot see who booked.
- Booking/cancelling changes the slot attendee list after the next fetch.
- Attendees are not editable in the LMS.

---

## 5. Feature 3 — show the user's Google Calendar events on the LMS calendar

### 5.1 Preconditions

- The user is connected to Google (`GET /auth/google/me` → 200).
- The page is `/calendar`. **Do not** add these events to `/admin/calendar` or the staff dashboard. Admins are redirected away from `/calendar` by `ProtectedRoute`; their Google settings live at `/admin/settings`.

This feature reads the user's **primary** Google calendar only. Other calendars (holidays, birthdays, shared) are out of scope.

### 5.2 Endpoint

```
GET /google/calendar/events?from=<RFC3339>&to=<RFC3339>
Authorization: Bearer <access_token>
```

- `from`/`to` are optional; defaults are now − 1 month and now + 2 months.
- `to` must be after `from`; the range must be at most **366 days**. The frontend should send a wide window such as `now − 120 days` .. `now + 240 days` (360 days) once per calendar mount, matching the LMS events window.
- The backend pages through Google (up to 10 pages of 2500) and returns one flat array. The frontend makes one request; do not implement paging.
- Recurring events are expanded by Google (`singleEvents=true`); cancelled and start-less events are skipped; empty titles become `(No title)`.
- No attendees are returned for Google events.

### 5.3 Response

Timed event:

```json
{
  "id": "pjhnfhl6s0l57phunlc3fm11p8",
  "title": "Dentist appointment",
  "description": "A personal event",
  "location": "Smile Clinic",
  "starts_at": "2026-11-20T10:00:00+04:00",
  "ends_at": "2026-11-20T11:00:00+04:00",
  "all_day": false,
  "meeting_url": "https://meet.google.com/fix-pwcp-yzu",
  "html_link": "https://www.google.com/calendar/event?eid=..."
}
```

All-day event (one day):

```json
{
  "id": "abc123",
  "title": "Friend's birthday",
  "starts_at": "2026-11-20",
  "ends_at": "2026-11-20",
  "all_day": true
}
```

For all-day events:

- `starts_at`/`ends_at` are `YYYY-MM-DD`, **not** RFC 3339.
- `ends_at` is the last covered day **inclusive** (Google's exclusive end date is already converted by the backend). A one-day event has `starts_at === ends_at`.
- Parse them as **local** dates (`new Date(y, m - 1, d)`), never `new Date("YYYY-MM-DD")` (which parses as UTC and can shift the day).
- Render the start at local `00:00` and the end at local `23:59:59.999`.
- Multi-day events are shown on their start day (existing calendar limitation; no expansion required).

Errors:

| Status | `error` | Meaning | Frontend behaviour |
|---|---|---|---|
| 400 | `bad_request` | bad `from`/`to`, `to <= from`, range > 366 days | programmer error; do not show a scary banner |
| 401 | `google_token_expired` | Google token revoked/expired | set connection to “reconnect”; show a reconnect hint |
| 404 | `google_not_connected` | no Google link | do not fetch; no error |
| 502 | `upstream_error` | Google unavailable/refused | non-blocking hint with retry |
| 500 | `server_error` | server problem | non-blocking hint with retry |

An older backend without this route answers `404`/`405`; treat that as “no Google events” and hide the feature, not as an error.

### 5.4 Fetching and merging

- Add a dedicated hook (e.g. `useGoogleCalendarEvents`) used **only by `CalendarPage`**. Do not add Google fetching to `useCalendarEvents`: `useCalendarEvents` is also used by the staff dashboard and the admin calendar.
- Fetch only when the Google connection status is `connected`. Never call the endpoint when disconnected/reconnect — that is the current behaviour and it is correct.
- The hook must be non-blocking: the LMS calendar renders from its own data while Google loads; a Google failure must not set the page-level calendar error state.
- Merge Google events with the LMS events (and deadlines/1:1 cards) only for display on `/calendar`.
- **Dedupe by `google_event_id`:** an LMS event that was synced to Google has `google_event_id`; the Google list contains the same Google event with the same id. Drop the Google copy and keep the LMS event (it is editable and richer).
- Prefix Google event ids to avoid collisions with numeric LMS ids and `slot-*` ids, e.g. `google-${googleId}`, and mark them with a separate source flag.

Suggested shape:

```ts
export type EventKind = 'lecture' | 'meeting' | 'off-campus' | 'deadline' | 'availability' | 'google'

export interface CalendarEvent {
  // ...existing fields
  attendees?: Attendee[]
  /** Imported read-only from the user's Google Calendar. */
  source?: 'google'
  /** Google Calendar's own link to the event. */
  htmlLink?: string
}
```

### 5.5 UI requirements for Google events

- Show them in the month grid and the day panel like other events, with a distinct “Google” kind/colour and legend entry. The legend filter should include/exclude them.
- **Read-only:** no Edit/Delete actions, no drag/drop, no form. `isEditableEvent` must return false for `kind === 'google'` / `source === 'google'`.
- The event card may show title, time/“All day”, location, description, and an “Open in Google Calendar” link when `html_link` is present. Only render `html_link` if it starts with `https://`.
- If the event has `meeting_url`, reuse `JoinMeeting`. For the owner’s own Google event you may choose to show the link without the student 30-minute delay; that is a product choice.
- Do not show the LMS “On Google Calendar” badge on Google-imported events; the Google kind already marks them.
- Connection hints near the calendar/legend:
  - `disconnected`: “Connect Google Calendar in Settings to see your Google events here.” (link via `settingsPath(user)`; students and staff use `/settings`, admins `/admin/settings`).
  - `reconnect`: “Reconnect Google Calendar to see your Google events again.”
  - Google fetch error: a quiet “Google Calendar events couldn’t be loaded” + Retry.
- Update `GoogleCard` copy: the current student text says the school’s events cannot be added to a student Google calendar yet. That is now misleading: students connect so their **own** Google events appear on the LMS calendar (staff still use the tick to add LMS events to Google).
- Optional: add sample Google events to `mocks/google.ts` for the offline demo and tests.

### 5.6 Acceptance

- A connected student/mentor/lecturer opens `/calendar`; the app calls `GET /auth/google/me`, then `GET /google/calendar/events?from=...&to=...`, and the user's personal events appear marked Google.
- The Google events are read-only; no edit/delete is offered.
- An all-day Google event appears on the correct local date and shows “All day”.
- An LMS event synced to Google appears once, as the LMS event.
- No Google request is made when the user is disconnected; the LMS calendar still works.
- Revoked token → reconnect hint; Google/network error → retry hint; neither breaks the LMS calendar.
- `/admin/calendar` shows no personal Google events.

---

## 6. Backend read change: `GET /events` is no longer role-filtered

This is already deployed (commit `a0db7d0`) and affects the calendar:

- Any authenticated user receives **all non-deleted events** in the requested range, regardless of role, enrollment, or module ownership.
- Events of soft-deleted modules are excluded; module-less events are included.
- Write behaviour is unchanged: only lecturers/admins can create events; edits/deletes still enforce module ownership (or admin).
- The frontend must not filter events by role, and must not assume a student only sees their own modules’ events.
- The existing client-side rule that hides **deadlines** of modules that are not open yet is separate and may stay.

No frontend permission change is needed for the calendar; just do not reintroduce role filtering.

---

## 7. File-by-file checklist for the latest frontend

| File | Current state (`6b81b4d`) | Required change |
|---|---|---|
| `src/types/calendar.ts` | 5 kinds; no attendees/source/htmlLink | Add `'google'` kind, `attendees`, `source`, `htmlLink` |
| `src/types/slots.ts` | No attendees/meetingUrl | Add both |
| `src/api/types.ts` | `ApiEvent` has no attendees; no Google event type | Add `ApiAttendee`, `ApiEvent.attendees`, `ApiGoogleCalendarEvent` |
| `src/api/mappers.ts` | `mapEvent` ignores attendees; no Google mapper; `eventToApi` sends `meeting_url: null` | Map attendees; add `mapGoogleEvent`; send `''` to clear link |
| `src/api/events.http.ts` | Sync create flow exists | Keep; ensure the refetch is used so the Meet link appears |
| `src/api/slots.ts` | Maps bookedBy only | Map `attendees` and `meeting_url` in `toMeetingSlot` |
| `src/utils/slots.ts` | Derived meeting cards have no attendees/meetingUrl | Attach both to the mentor/student meeting cards |
| `src/components/calendar/DayPanel.tsx` | No attendee disclosure; no Google kind; no source link | Add “Attendees (N)” names-only, `KIND_LABEL.google`, optional Google link |
| `src/components/calendar/GoogleBadge.tsx` | Shows for any `googleEventId` | Do not show it on `source === 'google'` |
| `src/components/calendar/Slots.tsx` | Shows bookedBy/“Booked” | Optionally show attendee names for the caller's view |
| `src/components/calendar/JoinMeeting.tsx` | Exists | Reuse for event and 1:1 Meet links; adjust copy if used for 1:1 |
| `src/components/calendar/eventClass.ts` / `calendar.css` | 5 event colours | Add a Google colour/class |
| `src/pages/CalendarPage.tsx` | LMS + deadlines + slots only | Add Google hook, merge/dedupe, legend entry, connect/reconnect/error hints |
| `src/api/google.ts` / `google.http.ts` | Status/connect only | Add `GET /google/calendar/events` client + mapper |
| `src/hooks/useCalendarEvents.ts` | LMS only | **Do not add Google fetching here** (shared with admin/staff) |
| `src/components/google/GoogleCard.tsx` | Student copy says school events can’t go to a student calendar | Update copy for seeing the student’s own Google events |
| `src/utils/googleSync.ts` | `meetingLinkLost` says the server doesn’t store links yet | Update stale message |
| `src/utils/authoring.ts` | `isEditableEvent` permits only lecture/meeting/off-campus | Also require `source !== 'google'` |
| `src/mocks/google.ts` | Connection only | Add sample Google events for offline/tests (optional but useful) |
| Tests | No attendees/Google-events coverage | Add unit/component/e2e coverage (see §9) |

**Do not** change `src/pages/admin/calendar/AdminCalendarPage.tsx` to add personal Google events.

---

## 8. Edge cases and failure modes

- **No Google connection:** no Google-calendar request; calendar is LMS-only. Do not show a Google error banner, only a connect hint.
- **Revoked/expired Google token:** `GET /google/calendar/events` → 401; switch the connection state to reconnect; link to Settings; keep LMS events.
- **Google down / network error:** 502/500; non-blocking retry hint.
- **Older backend without the route:** 404/405 → treat as no Google events; hide the hint/feature.
- **Range:** frontend sends 360 days; backend rejects > 366 days and `to <= from`. Do not page by month.
- **All-day events:** local date parsing; inclusive ends; never use the UTC `Date` string parser for `YYYY-MM-DD`.
- **Duplicate LMS/Google event:** dedupe by `google_event_id`; keep the LMS copy.
- **LMS event deleted but Google copy remains:** it may appear as a read-only Google event; that is acceptable.
- **Google event without a title:** backend sends `(No title)`.
- **Cancelled/no-start Google events:** backend omits them; no frontend work.
- **Attendee visibility:** `GET /events` is unrestricted, so attendees (including emails) are returned to any authenticated user. Render names only. For slots, the backend hides other students; do not infer.
- **Meeting link clearing:** PATCH must send `""`; `null` keeps the old link.
- **Sync failure:** event still saved; warn; do not retry automatically.
- **Google link replacement:** a pasted link is replaced by Google’s generated Meet link when sync succeeds.
- **Admins:** redirected from `/calendar`; no personal Google events in `/admin/calendar`.

---

## 9. Testing checklist

### 9.1 Backend requests (manual/curl)

Use a valid `$TOKEN` from a lecturer or admin:

```bash
# 1. Create an event and ask for the Google copy
curl -s -X POST "$API/events" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Sync test","kind":"lecture","starts_at":"2026-11-20T10:00:00Z","ends_at":"2026-11-20T11:00:00Z","sync_to_google":true}'

# 2. List events (all authenticated roles now get all events, with attendees)
curl -s "$API/events?from=2026-11-01T00:00:00Z&to=2026-12-01T00:00:00Z" -H "Authorization: Bearer $TOKEN"

# 3. Clear a meeting link
curl -s -X PATCH "$API/events/15" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"meeting_url":""}'

# 4. The user's Google Calendar events
curl -s "$API/google/calendar/events?from=2026-11-01T00:00:00Z&to=2026-12-01T00:00:00Z" \
  -H "Authorization: Bearer $TOKEN"

# 5. Slots (attendees + booked-by privacy)
curl -s "$API/mentor-slots" -H "Authorization: Bearer $TOKEN"
```

Also check: no token → 401; unlinked user → 404 `google_not_connected`; range > 366 days → 400; student POST /events → 403.

### 9.2 Frontend unit/component tests

- `mapEvent` maps `attendees`, tolerates missing/empty.
- `eventToApi` sends `meeting_url: ''` when absent, and the URL when present.
- `mapGoogleEvent`: timed RFC 3339 pass-through; all-day `YYYY-MM-DD` → local start/end (inclusive); missing title fallback; `https`-only `html_link`.
- Merge/dedupe: LMS event with `googleEventId` suppresses the Google copy; Google-only events survive.
- `toMeetingSlot`: attendees/meetingUrl mapping; null/missing tolerated.
- `slotEvents`: meeting cards carry attendees/meetingUrl; availability summaries do not.
- Day panel: attendees disclosure shows count + names; absent/empty shows nothing; Google event has no edit/delete.
- Calendar page: connected → fetch + Google event visible; disconnected → no request + connect hint; fetch error → LMS still visible + retry hint.

### 9.3 E2E scenarios

1. Connect Google in Settings, open `/calendar`, see a sample Google event marked Google; no edit/delete.
2. Create an event with the Google checkbox; see the Meet link and the Google badge; the Google copy exists.
3. Create an event without the checkbox; no Google badge; no link.
4. Event with a module shows attendees; module-less event does not.
5. Mentor books/selects a 1:1; the booking card shows the Meet link and the student in attendees; another student does not see the booker.
6. Revoke/expire connection (mock or backend) → reconnect hint; calendar still loads.
7. Admin calendar does not show personal Google events.

---

## 10. Appendix

### 10.1 Endpoint summary

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/auth/login` | — | LMS login |
| POST | `/auth/signup` | — | LMS signup |
| POST | `/auth/refresh` | — | rotate access/refresh tokens |
| POST | `/auth/logout` | — | revoke refresh token |
| GET | `/auth/google/login?access_token=` | optional/query JWT | start Google consent (page navigation) |
| GET | `/auth/google/callback` | — | Google return; redirects to frontend `/settings?google=...` |
| GET | `/auth/google/me` | Bearer | Google connection status/profile |
| GET | `/events?from&to` | Bearer | all events for any signed-in user, with attendees |
| POST | `/events` | Bearer (lecturer/admin) | create; optional `sync_to_google` |
| PATCH | `/events/{id}` | Bearer (lecturer/admin) | update; `meeting_url: ""` clears |
| DELETE | `/events/{id}` | Bearer (lecturer/admin) | delete (also Google copy) |
| GET | `/mentor-slots` | Bearer | visible slots, with attendees |
| POST | `/mentor-slots` | Bearer (mentor/admin) | create availability |
| DELETE | `/mentor-slots/{id}` | Bearer | remove free slot |
| POST | `/mentor-slots/{id}/book` | Bearer (student) | book; may create Google event + Meet |
| DELETE | `/mentor-slots/{id}/book` | Bearer | cancel; removes Google event |
| GET | `/google/calendar/events?from&to` | Bearer | caller's primary Google Calendar, read-only |

### 10.2 Wire types

```ts
export interface ApiAttendee {
  id: number | string
  name: string
  email: string
}

export interface ApiEvent {
  id: number | string
  title: string
  kind: string
  starts_at: string
  ends_at: string
  all_day: boolean
  location?: string | null
  description?: string | null
  meeting_url?: string | null
  module_id?: number | string | null
  google_event_id?: string | null
  attendees?: ApiAttendee[] | null
  created_at?: string
}

export interface ApiMentorSlot {
  id: string
  mentor_id: number
  mentor_name: string
  starts_at: string
  ends_at: string
  booked: boolean
  booked_by?: number
  booked_by_name?: string
  meeting_url?: string | null
  attendees?: ApiAttendee[] | null
}

export interface ApiGoogleCalendarEvent {
  id: string
  title: string
  description?: string | null
  location?: string | null
  /** RFC 3339, or YYYY-MM-DD when all_day */
  starts_at: string
  /** RFC 3339, or the inclusive last day when all_day */
  ends_at: string
  all_day: boolean
  meeting_url?: string | null
  html_link?: string | null
}
```

### 10.3 Environment and scopes

Backend env (already used by the existing Google integration):

- `GOOGLE_OAUTH_CLIENT_ID`
- `GOOGLE_OAUTH_CLIENT_SECRET`
- `GOOGLE_OAUTH_REDIRECT_URI` (e.g. `http://localhost:8080/auth/google/callback`)
- `GOOGLE_OAUTH_FRONTEND_REDIRECT_URI` (e.g. `http://localhost:5173/settings`)
- `GOOGLE_TOKEN_ENCRYPTION_KEY`

Frontend env:

- `VITE_API_URL` (empty for offline mocks)

Google scopes: `calendar.events`, `gmail.send`, `openid email`.

### 10.4 Out of scope

- Other Google calendars (holidays, birthdays, shared) — primary only.
- Showing attendee emails or profile links.
- Editing/deleting Google events from the LMS.
- Personal Google events in `/admin/calendar`.
- Expanding multi-day events across grid days.
- Google event attendees/RSVP (the Google calendar endpoint does not return attendees).
