# Google Calendar & Gmail Integration Plan — Xsolla School LMS

**Mode:** External (testing mode)  
**Audience:** Internal tool — users are organization members  
**Auth:** OAuth 2.0 per-user, refresh tokens stored encrypted at rest

---

## Overview

Integrate Google Calendar and Gmail so LMS events can be pushed to users' Google Calendars (with automatic email invitations) and standalone email can be sent via Gmail API.

All Google products share the same OAuth 2.0 plumbing: one Google Cloud project, one consent screen, one set of credentials, different scopes and API endpoints per product.

## Assumptions

- Users are **not** on a Google Workspace domain you control → **External** user type.
- App stays in **testing mode**; test users must be added manually in Google Cloud Console.
- Consent screen shows "Google hasn't verified this app" — users click Advanced → Go to unsafe (test).
- One Google account per LMS user; tokens stored per `user_id`.

---

## Section 1: Google Cloud Project & API Setup

**Goal:** Get OAuth credentials and enable the APIs.

**Steps:**
1. Create a Google Cloud Console project.
2. Enable **Google Calendar API** and **Gmail API**.
3. Configure OAuth consent screen:
   - User type: **External**
   - App name: e.g. "Xsolla School LMS"
   - User support email: your email
   - Developer contact: your email
   - Scopes to add:
     - `https://www.googleapis.com/auth/calendar.events`
     - `https://www.googleapis.com/auth/gmail.send`
     - `openid`
     - `email`
4. Add yourself (and any colleagues who need to test) as **Test users** on the consent screen. Without this, they cannot consent.
5. Create **OAuth 2.0 Client ID** (Web application).
6. Authorized redirect URIs:
   - Dev: `http://localhost:8080/auth/google/callback`
   - Prod: `https://<your-backend>/auth/google/callback`

**Deliverable:** Env vars `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_REDIRECT_URI`.

---

## Section 2: OAuth Connect Flow (Backend)

**Goal:** Let a user connect their Google account to the LMS.

**Endpoints:**

| Endpoint | Method | Purpose |
|---|---|---|
| `GET /auth/google/login` | GET | Redirect user to Google consent screen with requested scopes and `state` = current user ID |
| `GET /auth/google/callback` | GET | Google redirects here with `?code=...`; exchange code for access + refresh tokens |

**Flow:**
1. User clicks "Connect Google" → redirected to Google.
2. User signs in and grants scopes.
3. Google redirects to callback with authorization code.
4. Backend exchanges code via `https://oauth2.googleapis.com/token` → receives `access_token`, `refresh_token`, `expires_in`.
5. Tokens linked to the LMS user (Section 3).

**Deliverable:** User completes OAuth; backend stores their tokens.

---

## Section 3: Encrypted Token Storage

**Goal:** Safely store refresh tokens so the backend can act on the user's behalf later.

**Database table:** `google_tokens`

| Column | Type | Notes |
|---|---|---|
| `user_id` | bigint | FK to `users(id)` |
| `encrypted_refresh_token` | text | AES-GCM encrypted, never plaintext |
| `token_expires_at` | timestamptz | Access token expiry (for reference) |
| `updated_at` | timestamptz | Last refresh |

**Encryption:** AES-GCM with key from env `GOOGLE_TOKEN_ENCRYPTION_KEY` (32 bytes base64).

**Functions:**
- `SaveGoogleTokens(userID, tokens)` — encrypt + upsert
- `GetValidAccessToken(userID)` — decrypt refresh token; if access token expired/missing, call Google token endpoint to refresh; return usable access token
- `RevokeGoogleTokens(userID)` — delete row

**Deliverable:** Refresh tokens stored encrypted; retrievable per user.

---

## Section 4: Google Service Client

**Goal:** Reusable authenticated caller for Google APIs.

**Package:** `internal/service/google/`

**Components:**
- `NewClient(accessToken string)` — `*http.Client` with `Authorization: Bearer <token>`
- `CreateCalendarEvent(ctx, event)` — `POST https://www.googleapis.com/calendar/v3/calendars/primary/events`
- `SendEmail(ctx, to, subject, body)` — `POST https://gmail.googleapis.com/gmail/v1/users/me/messages.send` (base64url raw MIME)
- Auto-refresh on 401: use stored refresh token to get new access token, retry once

**Deliverable:** Go package for authenticated Google API calls.

---

## Section 5: Calendar Event Push (LMS → Google)

**Goal:** When an LMS event is created, optionally create it in Google Calendar.

**Integration point:** `POST /events`

**Optional body field:** `sync_to_google: bool`

**Behavior:**
- If `true` and user has Google tokens:
  - Create Google Calendar event with `attendees` = enrolled students + module lecturer
  - Google automatically sends invitation emails to attendees
- Store returned `google_event_id` on the LMS `events` row for future sync.
- If no Google tokens: skip sync, event still created in LMS (do not fail).

**Deliverable:** LMS events mirror to Google Calendar; attendees receive Google invite emails.

---

## Section 6: Calendar Event Update/Delete Sync

**Goal:** Keep Google Calendar in sync when LMS events change.

| LMS action | Google action |
|---|---|
| `PATCH /events/:id` | `PATCH` Google event if `google_event_id` exists |
| `DELETE /events/:id` | `DELETE` / cancel Google event if `google_event_id` exists |
| Google access revoked | Skip sync gracefully; log warning; do not fail LMS operation |

**Deliverable:** Updates and deletes propagate to Google Calendar.

---

## Section 7: Standalone Gmail Email

**Goal:** Send arbitrary email through Gmail (not only calendar invites).

**Endpoint:** `POST /events/:id/notify` (or similar)

**Behavior:**
- Compose email body (text or HTML).
- Send via Gmail API `users.messages.send` with base64url-encoded raw RFC 822 message.
- Uses same stored access token flow from Section 4.

**Deliverable:** Ad-hoc email notifications via connected Gmail accounts.

---

## Section 8: Frontend UI

**Goal:** Let users connect and view sync status.

**Components:**
- "Connect Google Account" button in user profile/settings.
- Status indicator: Connected / Disconnected / Reconnect needed.
- Optional per-event toggle "Sync to Google Calendar" on event creation/edit.

**Deliverable:** Users can initiate OAuth and see connection status from the UI.

---

## Section 9: Testing & Edge Cases

| Scenario | Expected behavior |
|---|---|
| Access token expired | Auto-refresh using stored refresh token, retry once |
| Refresh token invalid / revoked | 401/403 from Google; show "Reconnect Google" button |
| No Google tokens | Skip sync, LMS operation still succeeds |
| Non-test user tries to connect | Google shows "This app isn't authorized for your account" — add them as test user in Console |
| Multiple users | Tokens scoped per `user_id`; no cross-user leakage |

**Deliverable:** Integration handles failures gracefully without breaking LMS operations.

---

## Recommended Execution Order

1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9

Sections 1–4 are the auth foundation (no user-facing feature yet).  
Section 5 is the core feature (calendar push).  
Sections 6–9 are coverage and polish.

---

## Immediate Blocker

Before testing Section 2, you must add your own Google account email (and any colleagues') as **Test users** in Google Cloud Console → OAuth consent screen → Test users. Without this, External testing mode blocks consent.
