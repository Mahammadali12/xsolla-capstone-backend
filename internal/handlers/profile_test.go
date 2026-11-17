package handlers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
)

// profFake records what the handlers pass on and answers with fixed values.
type profFake struct {
	profile    service.Profile
	err        error
	gotUpdate  *service.ProfileUpdate
	gotViewer  int64
	gotRoles   []string
	gotTarget  int64
	gotAvatar  []byte
	removed    bool
	prefsPatch map[string]json.RawMessage
}

func (f *profFake) MyProfile(context.Context, int64) (*service.Profile, error) {
	return &f.profile, f.err
}
func (f *profFake) ProfileFor(_ context.Context, viewer int64, roles []string, target int64) (*service.Profile, error) {
	f.gotViewer, f.gotRoles, f.gotTarget = viewer, roles, target
	return &f.profile, f.err
}
func (f *profFake) UpdateMyProfile(_ context.Context, _ int64, upd service.ProfileUpdate) (*service.Profile, error) {
	f.gotUpdate = &upd
	return &f.profile, f.err
}
func (f *profFake) SetAvatar(_ context.Context, _ int64, data []byte) (string, error) {
	f.gotAvatar = data
	if f.err != nil {
		return "", f.err
	}
	return strings.Repeat("ab", 16) + ".png", nil
}
func (f *profFake) RemoveAvatar(context.Context, int64) error { f.removed = true; return f.err }
func (f *profFake) OpenAvatar(_ context.Context, name string) (io.ReadCloser, string, error) {
	if name != "known.png" {
		return nil, "", service.ErrAvatarNotFound
	}
	return io.NopCloser(strings.NewReader("PNGDATA")), "image/png", nil
}
func (f *profFake) Get(context.Context, int64) (map[string]any, error) {
	return service.DefaultPreferences(), f.err
}
func (f *profFake) Update(_ context.Context, _ int64, patch map[string]json.RawMessage) (map[string]any, error) {
	f.prefsPatch = patch
	if f.err != nil {
		return nil, f.err
	}
	return service.DefaultPreferences(), nil
}

var profSecret = []byte("profile-test-secret")

func profMux(f *profFake, publicURL string) *http.ServeMux {
	h := NewProfileHandlers(f, f, publicURL)
	a := &middleware.Authenticator{Secret: profSecret}
	mux := http.NewServeMux()
	mux.Handle("GET /users/me/profile", a.Middleware(http.HandlerFunc(h.GetMyProfile)))
	mux.Handle("PATCH /users/me/profile", a.Middleware(http.HandlerFunc(h.UpdateMyProfile)))
	mux.Handle("GET /users/{id}/profile", a.Middleware(http.HandlerFunc(h.GetProfile)))
	mux.Handle("PUT /users/me/avatar", a.Middleware(http.HandlerFunc(h.PutMyAvatar)))
	mux.Handle("DELETE /users/me/avatar", a.Middleware(http.HandlerFunc(h.DeleteMyAvatar)))
	mux.Handle("GET /users/me/preferences", a.Middleware(http.HandlerFunc(h.GetMyPreferences)))
	mux.Handle("PATCH /users/me/preferences", a.Middleware(http.HandlerFunc(h.UpdateMyPreferences)))
	mux.HandleFunc("GET /avatars/{name}", h.ServeAvatar)
	return mux
}

func profDo(t *testing.T, mux http.Handler, method, path, contentType string, body io.Reader, signedIn bool) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if signedIn {
		token, err := auth.AccessToken(7, []string{"student"}, profSecret, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var obj map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &obj)
	return rec, obj
}

func TestProfileRoutes(t *testing.T) {
	name := "known.png"
	f := &profFake{profile: service.Profile{AvatarFile: &name, Bio: "Go and cats.", SlackHandle: "rashad.a", Visibility: "everyone"}}
	mux := profMux(f, "https://api.example.com/")

	rec, obj := profDo(t, mux, "GET", "/users/me/profile", "", nil, true)
	if rec.Code != 200 || obj["avatar_url"] != "https://api.example.com/avatars/known.png" || obj["bio"] != "Go and cats." ||
		obj["gitlab_handle"] != "" || obj["visibility"] != "everyone" || len(obj) != 5 {
		t.Errorf("GET me: %d %s", rec.Code, rec.Body)
	}
	if rec, _ := profDo(t, mux, "GET", "/users/me/profile", "", nil, false); rec.Code != 401 {
		t.Errorf("no token: %d", rec.Code)
	}

	f.profile.AvatarFile = nil
	rec, obj = profDo(t, mux, "GET", "/users/12/profile", "", nil, true)
	if rec.Code != 200 || obj["avatar_url"] != nil || f.gotViewer != 7 || f.gotTarget != 12 || f.gotRoles[0] != "student" {
		t.Errorf("GET other: %d %s (viewer %d target %d)", rec.Code, rec.Body, f.gotViewer, f.gotTarget)
	}
	if _, present := obj["avatar_url"]; !present {
		t.Error("avatar_url must be null, not missing")
	}
	for _, bad := range []string{"/users/abc/profile", "/users/0/profile", "/users/-3/profile"} {
		if rec, _ := profDo(t, mux, "GET", bad, "", nil, true); rec.Code != 400 {
			t.Errorf("%s: %d, want 400", bad, rec.Code)
		}
	}
	f.err = service.ErrProfileUserNotFound
	if rec, _ := profDo(t, mux, "GET", "/users/99/profile", "", nil, true); rec.Code != 404 {
		t.Errorf("unknown user: %d", rec.Code)
	}
	f.err = errors.New("database down")
	if rec, _ := profDo(t, mux, "GET", "/users/me/profile", "", nil, true); rec.Code != 500 {
		t.Errorf("database down: %d", rec.Code)
	}
}

func TestPatchProfileParsing(t *testing.T) {
	f := &profFake{profile: service.Profile{Visibility: "everyone"}}
	mux := profMux(f, "")

	rec, _ := profDo(t, mux, "PATCH", "/users/me/profile", "application/json", strings.NewReader(`{"bio": "hi", "visibility": "staff_only"}`), true)
	if rec.Code != 200 || f.gotUpdate == nil || *f.gotUpdate.Bio != "hi" || *f.gotUpdate.Visibility != "staff_only" || f.gotUpdate.SlackHandle != nil {
		t.Fatalf("valid patch: %d %s %+v", rec.Code, rec.Body, f.gotUpdate)
	}
	if rec, _ := profDo(t, mux, "PATCH", "/users/me/profile", "application/json", strings.NewReader(`{}`), true); rec.Code != 200 {
		t.Errorf("empty patch: %d", rec.Code)
	}

	for body, want := range map[string]string{
		`{"avatar_url": "https://x"}`: "PUT /users/me/avatar",
		`{"name": "New name"}`:        "not a profile field",
		`{"bio": null}`:               "bio must be a string",
		`{"bio": 5}`:                  "bio must be a string",
		`{"slack_handle": ["a"]}`:     "slack_handle must be a string",
		`[1, 2]`:                      "one JSON object",
		`null`:                        "one JSON object",
		`{"bio": "a"} {"bio": "b"}`:   "one JSON object",
		`{"bio": `:                    "one JSON object",
	} {
		f.gotUpdate = nil
		rec, obj := profDo(t, mux, "PATCH", "/users/me/profile", "application/json", strings.NewReader(body), true)
		if rec.Code != 400 || obj["error"] != "bad_request" || !strings.Contains(obj["message"].(string), want) {
			t.Errorf("%s: %d %s, want 400 mentioning %q", body, rec.Code, rec.Body, want)
		}
		if f.gotUpdate != nil {
			t.Errorf("%s: reached the service", body)
		}
	}
	if rec, _ := profDo(t, mux, "PATCH", "/users/me/profile", "application/json", strings.NewReader(`{"bio": "`+strings.Repeat("x", 20000)+`"}`), true); rec.Code != 413 {
		t.Errorf("huge body: %d, want 413", rec.Code)
	}

	f.err = &service.BadRequestError{Message: "bio must be at most 300 characters (it has 301)"}
	rec, obj := profDo(t, mux, "PATCH", "/users/me/profile", "application/json", strings.NewReader(`{"bio": "x"}`), true)
	if rec.Code != 400 || obj["message"] != "bio must be at most 300 characters (it has 301)" {
		t.Errorf("rule broken in the service: %d %s", rec.Code, rec.Body)
	}
}

func multipartBody(t *testing.T, field, filename string, data []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	w.Close()
	return &buf, w.FormDataContentType()
}

func TestAvatarRoutes(t *testing.T) {
	f := &profFake{}
	mux := profMux(f, "")

	body, ct := multipartBody(t, "file", "me.png", []byte("picture bytes"))
	rec, obj := profDo(t, mux, "PUT", "/users/me/avatar", ct, body, true)
	if rec.Code != 200 || obj["avatar_url"] != "http://example.com/avatars/"+strings.Repeat("ab", 16)+".png" || string(f.gotAvatar) != "picture bytes" {
		t.Errorf("upload: %d %s", rec.Code, rec.Body)
	}

	f.err = service.ErrAvatarInvalid
	body, ct = multipartBody(t, "file", "me.png", []byte("not a picture"))
	rec, obj = profDo(t, mux, "PUT", "/users/me/avatar", ct, body, true)
	if rec.Code != 400 || obj["message"] != service.AvatarRule {
		t.Errorf("wrong type: %d %s", rec.Code, rec.Body)
	}
	f.err = nil

	f.gotAvatar = nil
	body, ct = multipartBody(t, "file", "big.png", make([]byte, service.AvatarMaxBytes+1))
	if rec, obj := profDo(t, mux, "PUT", "/users/me/avatar", ct, body, true); rec.Code != 400 || obj["message"] != service.AvatarRule || f.gotAvatar != nil {
		t.Errorf("2 MB + 1 byte: %d %s", rec.Code, rec.Body)
	}
	body, ct = multipartBody(t, "file", "huge.png", make([]byte, 3*service.AvatarMaxBytes))
	if rec, obj := profDo(t, mux, "PUT", "/users/me/avatar", ct, body, true); rec.Code != 400 || obj["message"] != service.AvatarRule {
		t.Errorf("6 MB: %d %s", rec.Code, rec.Body)
	}
	body, ct = multipartBody(t, "picture", "me.png", []byte("x"))
	if rec, obj := profDo(t, mux, "PUT", "/users/me/avatar", ct, body, true); rec.Code != 400 || !strings.Contains(obj["message"].(string), "field named file") {
		t.Errorf("wrong field name: %d %s", rec.Code, rec.Body)
	}
	if rec, _ := profDo(t, mux, "PUT", "/users/me/avatar", "application/json", strings.NewReader(`{}`), true); rec.Code != 400 {
		t.Errorf("not multipart: %d", rec.Code)
	}
	body, ct = multipartBody(t, "file", "me.png", []byte("x"))
	if rec, _ := profDo(t, mux, "PUT", "/users/me/avatar", ct, body, false); rec.Code != 401 {
		t.Errorf("no token: %d", rec.Code)
	}

	if rec, _ := profDo(t, mux, "DELETE", "/users/me/avatar", "", nil, true); rec.Code != 204 || rec.Body.Len() != 0 || !f.removed {
		t.Errorf("delete: %d %q", rec.Code, rec.Body)
	}

	rec, _ = profDo(t, mux, "GET", "/avatars/known.png", "", nil, false)
	if rec.Code != 200 || rec.Body.String() != "PNGDATA" || rec.Header().Get("Content-Type") != "image/png" ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("serve picture without a token: %d %v", rec.Code, rec.Header())
	}
	if rec, _ := profDo(t, mux, "GET", "/avatars/unknown.png", "", nil, false); rec.Code != 404 {
		t.Errorf("unknown picture: %d", rec.Code)
	}
}

func TestPreferencesRoutes(t *testing.T) {
	f := &profFake{}
	mux := profMux(f, "")

	rec, obj := profDo(t, mux, "GET", "/users/me/preferences", "", nil, true)
	if rec.Code != 200 || obj["text_size"] != "normal" || len(obj) != 9 {
		t.Errorf("GET: %d %s", rec.Code, rec.Body)
	}
	rec, _ = profDo(t, mux, "PATCH", "/users/me/preferences", "application/json", strings.NewReader(`{"text_size": "large", "high_contrast": true}`), true)
	if rec.Code != 200 || string(f.prefsPatch["text_size"]) != `"large"` || string(f.prefsPatch["high_contrast"]) != "true" {
		t.Errorf("PATCH: %d %v", rec.Code, f.prefsPatch)
	}
	if rec, _ := profDo(t, mux, "PATCH", "/users/me/preferences", "application/json", strings.NewReader(`"large"`), true); rec.Code != 400 {
		t.Errorf("not an object: %d", rec.Code)
	}
	f.err = &service.BadRequestError{Message: `text_size must be one of "normal", "large", "xlarge"`}
	if rec, obj := profDo(t, mux, "PATCH", "/users/me/preferences", "application/json", strings.NewReader(`{"text_size": "huge"}`), true); rec.Code != 400 || !strings.Contains(obj["message"].(string), "text_size") {
		t.Errorf("bad value: %d %s", rec.Code, rec.Body)
	}
}

// profAvatars is an AvatarSource that shows person 1's picture to everyone and person 2's only to staff.
type profAvatars struct {
	calls   int
	viewer  int64
	failing bool
}

func (a *profAvatars) VisibleAvatars(_ context.Context, viewer int64, roles []string, ids []int64) (map[int64]string, error) {
	a.calls++
	a.viewer = viewer
	if a.failing {
		return nil, errors.New("database down")
	}
	out := map[int64]string{}
	for _, id := range ids {
		if id == 1 || (id == 2 && len(roles) > 0 && roles[0] == "mentor") {
			out[id] = "p" + string(rune('0'+id)) + ".png"
		}
	}
	return out, nil
}

func TestAvatarSupportFillsPeople(t *testing.T) {
	src := &profAvatars{}
	var a avatarSupport
	req := httptest.NewRequest("GET", "/teams/me", nil)

	// without SetAvatars nothing happens and avatar_url stays null
	members := []dto.UserBrief{{ID: 1}}
	a.fillBriefs(req, nil, members)
	if members[0].AvatarURL != nil || src.calls != 0 {
		t.Fatal("avatars filled without a source")
	}

	a.SetAvatars(src, "https://api.example.com")
	mentor := &dto.UserBrief{ID: 2}
	members = []dto.UserBrief{{ID: 1}, {ID: 3}}
	a.fillBriefs(req, mentor, members)
	if src.calls != 1 || members[0].AvatarURL == nil || *members[0].AvatarURL != "https://api.example.com/avatars/p1.png" ||
		members[1].AvatarURL != nil || mentor.AvatarURL != nil {
		t.Errorf("team as a student: mentor %v members %v %v (calls %d)", mentor.AvatarURL, members[0].AvatarURL, members[1].AvatarURL, src.calls)
	}

	lists := [][]dto.Attendee{{{ID: 1}, {ID: 3}}, {}, {{ID: 1}}}
	a.fillAttendees(req, lists...)
	if src.calls != 2 || lists[0][0].AvatarURL == nil || lists[0][1].AvatarURL != nil || lists[2][0].AvatarURL == nil {
		t.Errorf("attendees: one lookup for every event, got %d calls", src.calls)
	}

	urls := a.avatarURLsFor(req, 2, []string{"mentor"}, 2)
	if src.viewer != 2 || urls[2] == nil {
		t.Errorf("explicit viewer: %v (viewer %d)", urls, src.viewer)
	}

	src.failing = true
	members = []dto.UserBrief{{ID: 1}}
	a.fillBriefs(req, nil, members)
	if members[0].AvatarURL != nil {
		t.Error("a failed lookup must leave avatar_url null, not fail")
	}
}

func TestPublicBaseURL(t *testing.T) {
	plain := httptest.NewRequest("GET", "http://lms.local:8080/users/me", nil)
	if got := publicBaseURL(plain, ""); got != "http://lms.local:8080" {
		t.Errorf("plain: %s", got)
	}
	if got := publicBaseURL(plain, "https://api.school.az/"); got != "https://api.school.az" {
		t.Errorf("configured: %s", got)
	}
	proxied := httptest.NewRequest("GET", "http://10.0.0.5:8080/users/me", nil)
	proxied.Header.Set("X-Forwarded-Proto", "https, http")
	proxied.Header.Set("X-Forwarded-Host", "api.school.az")
	if got := publicBaseURL(proxied, ""); got != "https://api.school.az" {
		t.Errorf("behind a proxy: %s", got)
	}
	odd := httptest.NewRequest("GET", "http://lms.local/users/me", nil)
	odd.Header.Set("X-Forwarded-Proto", "javascript")
	if got := publicBaseURL(odd, ""); got != "http://lms.local" {
		t.Errorf("unknown scheme must be ignored: %s", got)
	}
	secure := httptest.NewRequest("GET", "https://lms.local/users/me", nil)
	secure.TLS = &tls.ConnectionState{}
	if got := publicBaseURL(secure, ""); got != "https://lms.local" {
		t.Errorf("TLS: %s", got)
	}
}
