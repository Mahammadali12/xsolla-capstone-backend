# Implementation Plan

[Overview]
Create a Bruno API test request file for `POST /auth/google/logout` so the user can import and run it against the locally running server to verify the Google account disconnect endpoint. The endpoint removes the signed-in user's stored Google OAuth refresh token and returns 204 No Content, requiring a valid Bearer JWT. The file follows the existing conventions in `request/Capstone-request/auth/`.

[Types]
No type system changes. This is a Bruno YAML request file, not Go code.

[Files]

### New files
- `request/Capstone-request/auth/google-logout.yml` — Bruno request for `POST /auth/google/logout`. Method: POST, URL: `http://localhost:8080/auth/google/logout`, auth: Bearer token via `Authorization: Bearer {{accessToken}}` header (matching `google-login.yml`), no request body. Expected success response: 204 No Content.

### Existing files to modify
None.

### Files to delete or move
None.

### Configuration updates
None.

[Functions]
None (no Go code changes; this is purely a test request artifact).

[Classes]
None.

[Dependencies]
None.

[Testing]
The Bruno request file itself is the test. Manual validation steps:
1. Start the server (`go run cmd/server/main.go`).
2. Obtain a valid access token (e.g., via `POST /auth/login` → copy `access_token`).
3. Import `request/Capstone-request/auth/google-logout.yml` into Bruno (or open the collection folder).
4. Set the `accessToken` variable to a valid JWT.
5. Send the request → expect HTTP 204 No Content.
6. Verify with `GET /auth/google/me` afterward → expect 404 (`google_not_connected`) confirming the token was removed.

Edge cases to verify manually:
- Without a bearer token → expect 401 Unauthorized.
- With an expired/invalid token → expect 401.

[Implementation Order]
1. Create `request/Capstone-request/auth/google-logout.yml` with the Bruno request definition matching folder conventions.
2. (Manual) Import into Bruno and validate against a running server as described above.