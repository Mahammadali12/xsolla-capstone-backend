package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"xsolla-capstone-api/internal/store"
)

func sp(s string) *string { return &s }

// memProfiles is an in-memory profileStore.
type memProfiles struct {
	users    map[int64]*store.User
	profiles map[int64]*store.UserProfile
	failSet  error
}

func newMemProfiles(ids ...int64) *memProfiles {
	m := &memProfiles{users: map[int64]*store.User{}, profiles: map[int64]*store.UserProfile{}}
	for _, id := range ids {
		m.users[id] = &store.User{ID: id}
	}
	return m
}

func (m *memProfiles) GetUserByID(_ context.Context, id int64) (*store.User, error) {
	if u, ok := m.users[id]; ok {
		return u, nil
	}
	return nil, store.ErrUserNotFound
}
func (m *memProfiles) GetUserProfile(_ context.Context, id int64) (*store.UserProfile, error) {
	if p, ok := m.profiles[id]; ok {
		c := *p
		return &c, nil
	}
	return nil, store.ErrProfileNotFound
}
func (m *memProfiles) row(id int64) *store.UserProfile {
	if _, ok := m.profiles[id]; !ok {
		m.profiles[id] = &store.UserProfile{UserID: id, Visibility: VisibilityEveryone}
	}
	return m.profiles[id]
}
func (m *memProfiles) UpdateUserProfile(_ context.Context, id int64, p store.ProfilePatch) (*store.UserProfile, error) {
	r := m.row(id)
	for dst, src := range map[*string]*string{&r.Bio: p.Bio, &r.SlackHandle: p.SlackHandle, &r.GitlabHandle: p.GitlabHandle, &r.Visibility: p.Visibility} {
		if src != nil {
			*dst = *src
		}
	}
	c := *r
	return &c, nil
}
func (m *memProfiles) SetUserAvatar(_ context.Context, id int64, key *string) (*string, error) {
	if m.failSet != nil {
		return nil, m.failSet
	}
	if _, ok := m.profiles[id]; !ok && key == nil {
		return nil, nil
	}
	r := m.row(id)
	old := r.AvatarKey
	r.AvatarKey = key
	return old, nil
}
func (m *memProfiles) IsCurrentAvatar(_ context.Context, key string) (bool, error) {
	for _, p := range m.profiles {
		if p.AvatarKey != nil && *p.AvatarKey == key {
			return true, nil
		}
	}
	return false, nil
}
func (m *memProfiles) ListAvatars(_ context.Context, ids []int64) ([]store.AvatarVisibility, error) {
	var out []store.AvatarVisibility
	for _, id := range ids {
		if p, ok := m.profiles[id]; ok && p.AvatarKey != nil {
			out = append(out, store.AvatarVisibility{UserID: id, AvatarKey: *p.AvatarKey, Visibility: p.Visibility})
		}
	}
	return out, nil
}

// memStorage is an in-memory object storage.
type memStorage struct {
	objects map[string][]byte
	types   map[string]string
}

func newMemStorage() *memStorage {
	return &memStorage{objects: map[string][]byte{}, types: map[string]string{}}
}
func (s *memStorage) Upload(_ context.Context, _ int64, _, key, mime string, _ int64, r io.Reader) error {
	b, err := io.ReadAll(r)
	s.objects[key], s.types[key] = b, mime
	return err
}
func (s *memStorage) Download(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := s.objects[key]
	if !ok {
		return nil, errors.New("no such key")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (s *memStorage) Delete(_ context.Context, key string) error { delete(s.objects, key); return nil }
func (s *memStorage) PublicURL(key string) string                { return key }

// testPicture makes a small real picture in the given format.
func testPicture(t *testing.T, format string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// webpHeader is the start of a WebP file: enough for content detection.
var webpHeader = append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), make([]byte, 32)...)

var (
	studentRoles = []string{"student"}
	mentorRoles  = []string{"mentor"}
)

func TestProfileDefaultsAndUpdate(t *testing.T) {
	ctx := context.Background()
	svc := NewProfileService(newMemProfiles(1), newMemStorage())

	p, err := svc.MyProfile(ctx, 1)
	if err != nil || p.AvatarFile != nil || p.Bio != "" || p.Visibility != VisibilityEveryone {
		t.Fatalf("defaults: %+v, %v", p, err)
	}

	p, err = svc.UpdateMyProfile(ctx, 1, ProfileUpdate{Bio: sp("  Go and cats.\nAnd tea.  "), SlackHandle: sp(" @rashad.a ")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Bio != "Go and cats.\nAnd tea." || p.SlackHandle != "rashad.a" || p.GitlabHandle != "" || p.Visibility != VisibilityEveryone {
		t.Errorf("after first save: %+v", p)
	}
	// only the sent fields change; "" clears a field
	p, _ = svc.UpdateMyProfile(ctx, 1, ProfileUpdate{GitlabHandle: sp("rashad-a"), SlackHandle: sp("")})
	if p.Bio != "Go and cats.\nAnd tea." || p.SlackHandle != "" || p.GitlabHandle != "rashad-a" {
		t.Errorf("partial update: %+v", p)
	}
}

func TestProfileRules(t *testing.T) {
	cases := map[string]ProfileUpdate{
		"bio 301 characters":         {Bio: sp(strings.Repeat("ə", 301))},
		"bio with a NUL":             {Bio: sp("hi\x00there")},
		"slack handle 41 characters": {SlackHandle: sp(strings.Repeat("a", 41))},
		"slack handle with a space":  {SlackHandle: sp("rashad a")},
		"gitlab handle with a tab":   {GitlabHandle: sp("rashad\ta")},
		"unknown visibility":         {Visibility: sp("friends")},
	}
	for name, upd := range cases {
		_, err := NewProfileService(newMemProfiles(1), newMemStorage()).UpdateMyProfile(context.Background(), 1, upd)
		var bad *BadRequestError
		if !errors.As(err, &bad) {
			t.Errorf("%s: error %v, want a BadRequestError", name, err)
		}
	}
	ok := map[string]ProfileUpdate{
		"bio 300 characters":    {Bio: sp(strings.Repeat("ə", 300))},
		"handle 40 characters":  {SlackHandle: sp(strings.Repeat("a", 40))},
		"staff_only visibility": {Visibility: sp("staff_only")},
		"nothing sent":          {},
	}
	for name, upd := range ok {
		if _, err := NewProfileService(newMemProfiles(1), newMemStorage()).UpdateMyProfile(context.Background(), 1, upd); err != nil {
			t.Errorf("%s: unexpected error %v", name, err)
		}
	}
}

func TestProfileVisibility(t *testing.T) {
	ctx := context.Background()
	st := newMemProfiles(1, 2, 3)
	svc := NewProfileService(st, newMemStorage())
	if _, err := svc.SetAvatar(ctx, 1, testPicture(t, "png")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateMyProfile(ctx, 1, ProfileUpdate{Bio: sp("hello"), SlackHandle: sp("one"), Visibility: sp("staff_only")}); err != nil {
		t.Fatal(err)
	}

	hidden, _ := svc.ProfileFor(ctx, 2, studentRoles, 1)
	if hidden.AvatarFile != nil || hidden.Bio != "" || hidden.SlackHandle != "" || hidden.Visibility != VisibilityStaffOnly {
		t.Errorf("student sees a staff_only profile: %+v", hidden)
	}
	for name, viewer := range map[string]struct {
		id    int64
		roles []string
	}{"mentor": {2, mentorRoles}, "lecturer": {2, []string{"lecturer"}}, "admin": {2, []string{"admin"}}, "owner": {1, studentRoles}} {
		p, _ := svc.ProfileFor(ctx, viewer.id, viewer.roles, 1)
		if p.AvatarFile == nil || p.Bio != "hello" || p.SlackHandle != "one" {
			t.Errorf("%s should see everything: %+v", name, p)
		}
	}

	avatars, _ := svc.VisibleAvatars(ctx, 2, studentRoles, []int64{1, 2, 3, 1, 0})
	if len(avatars) != 0 {
		t.Errorf("student sees hidden pictures: %v", avatars)
	}
	avatars, _ = svc.VisibleAvatars(ctx, 2, mentorRoles, []int64{1, 2, 3})
	if len(avatars) != 1 || avatars[1] == "" {
		t.Errorf("mentor: %v, want only person 1's picture", avatars)
	}

	if _, err := svc.ProfileFor(ctx, 2, studentRoles, 99); !errors.Is(err, ErrProfileUserNotFound) {
		t.Errorf("unknown user: %v", err)
	}
	now := time.Now()
	st.users[3].DeletedAt = &now
	if _, err := svc.ProfileFor(ctx, 2, mentorRoles, 3); !errors.Is(err, ErrProfileUserNotFound) {
		t.Errorf("deleted user: %v", err)
	}
}

func TestSetAvatar(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		data []byte
		ext  string
	}{
		{"png", testPicture(t, "png"), ".png"},
		{"jpeg", testPicture(t, "jpeg"), ".jpg"},
		{"webp", webpHeader, ".webp"},
	} {
		st, files := newMemProfiles(1), newMemStorage()
		name, err := NewProfileService(st, files).SetAvatar(ctx, 1, tc.data)
		if err != nil || !avatarFileName.MatchString(name) || !strings.HasSuffix(name, tc.ext) {
			t.Errorf("%s: name %q, %v", tc.name, name, err)
			continue
		}
		if !bytes.Equal(files.objects["avatars/"+name], tc.data) || files.types["avatars/"+name] != avatarMIME[strings.TrimPrefix(tc.ext, ".")] {
			t.Errorf("%s: stored object or content type wrong (%q)", tc.name, files.types["avatars/"+name])
		}
	}

	bad := map[string][]byte{
		"empty":                     {},
		"text named like a picture": []byte("this is not a picture at all"),
		"PDF":                       []byte("%PDF-1.7\n1 0 obj"),
		"GIF":                       []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00"),
		"PNG header, broken body":   append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 40)...),
		"over 2 MB":                 append(testPicture(t, "png"), make([]byte, AvatarMaxBytes)...),
	}
	for name, data := range bad {
		files := newMemStorage()
		if _, err := NewProfileService(newMemProfiles(1), files).SetAvatar(ctx, 1, data); !errors.Is(err, ErrAvatarInvalid) {
			t.Errorf("%s: error %v, want ErrAvatarInvalid", name, err)
		}
		if len(files.objects) != 0 {
			t.Errorf("%s: a refused file was stored", name)
		}
	}
}

func TestAvatarReplaceRemoveAndServe(t *testing.T) {
	ctx := context.Background()
	st, files := newMemProfiles(1), newMemStorage()
	svc := NewProfileService(st, files)

	first, _ := svc.SetAvatar(ctx, 1, testPicture(t, "png"))
	second, _ := svc.SetAvatar(ctx, 1, testPicture(t, "jpeg"))
	if first == second {
		t.Fatal("a new upload must get a new name")
	}
	if _, ok := files.objects["avatars/"+first]; ok {
		t.Error("the replaced picture was not deleted")
	}
	if _, _, err := svc.OpenAvatar(ctx, first); !errors.Is(err, ErrAvatarNotFound) {
		t.Errorf("old name still served: %v", err)
	}
	rc, mime, err := svc.OpenAvatar(ctx, second)
	if err != nil || mime != "image/jpeg" {
		t.Fatalf("current picture: %q, %v", mime, err)
	}
	rc.Close()

	for _, name := range []string{"", "../submissions/1.pdf", "users/1/abc.png", strings.Repeat("a", 32) + ".gif", strings.ToUpper(second)} {
		if _, _, err := svc.OpenAvatar(ctx, name); !errors.Is(err, ErrAvatarNotFound) {
			t.Errorf("name %q: %v, want not found", name, err)
		}
	}

	if err := svc.RemoveAvatar(ctx, 1); err != nil || len(files.objects) != 0 {
		t.Errorf("remove: %v, objects left %d", err, len(files.objects))
	}
	if err := svc.RemoveAvatar(ctx, 1); err != nil {
		t.Errorf("removing twice: %v", err)
	}
	if err := NewProfileService(newMemProfiles(5), newMemStorage()).RemoveAvatar(ctx, 5); err != nil {
		t.Errorf("removing without a profile: %v", err)
	}

	st.failSet = errors.New("database down")
	if _, err := svc.SetAvatar(ctx, 1, testPicture(t, "png")); err == nil || len(files.objects) != 0 {
		t.Errorf("a failed save must not leave the uploaded file behind (err %v, objects %d)", err, len(files.objects))
	}
}

func TestHidingTheProfileRenamesThePicture(t *testing.T) {
	ctx := context.Background()
	st, files := newMemProfiles(1), newMemStorage()
	svc := NewProfileService(st, files)
	before, _ := svc.SetAvatar(ctx, 1, testPicture(t, "png"))

	p, err := svc.UpdateMyProfile(ctx, 1, ProfileUpdate{Visibility: sp("staff_only")})
	if err != nil || p.AvatarFile == nil || *p.AvatarFile == before {
		t.Fatalf("after hiding: %+v, %v; want a new picture name", p, err)
	}
	if _, ok := files.objects["avatars/"+before]; ok || len(files.objects) != 1 {
		t.Errorf("old name still stored (objects %d)", len(files.objects))
	}
	again, _ := svc.UpdateMyProfile(ctx, 1, ProfileUpdate{Visibility: sp("staff_only"), Bio: sp("x")})
	if *again.AvatarFile != *p.AvatarFile {
		t.Error("saving while already hidden must not rename the picture again")
	}
}

func TestPreferences(t *testing.T) {
	ctx := context.Background()
	st := &memPrefs{}
	svc := NewPreferencesService(st)

	got, err := svc.Get(ctx, 1)
	if err != nil || !reflect.DeepEqual(got, DefaultPreferences()) {
		t.Fatalf("defaults: %v, %v", got, err)
	}
	if got["text_size"] != "normal" || got["theme"] != "system" || got["high_contrast"] != false || len(got) != 9 {
		t.Errorf("defaults have wrong values: %v", got)
	}

	got, err = svc.Update(ctx, 1, rawPrefs(`{"text_size": "large", "font": "dyslexia", "colorblind_safe": true}`))
	if err != nil || got["text_size"] != "large" || got["font"] != "dyslexia" || got["colorblind_safe"] != true || got["theme"] != "system" {
		t.Fatalf("first update: %v, %v", got, err)
	}
	got, _ = svc.Update(ctx, 1, rawPrefs(`{"theme": "dark"}`))
	if got["text_size"] != "large" || got["theme"] != "dark" {
		t.Errorf("keys not sent must keep their value: %v", got)
	}
	if got, _ := svc.Update(ctx, 1, rawPrefs(`{}`)); got["theme"] != "dark" || st.merges != 2 {
		t.Errorf("empty update: %v (merges %d, want no write)", got, st.merges)
	}

	for body, wantKey := range map[string]string{
		`{"text_size": "huge"}`:                    "text_size",
		`{"theme": null}`:                          "theme",
		`{"high_contrast": "yes"}`:                 "high_contrast",
		`{"reduce_motion": null}`:                  "reduce_motion",
		`{"font_size": "large"}`:                   "font_size",
		`{"density": 1}`:                           "density",
		`{"zzz": 1, "spacing": "relaxed", "a": 2}`: `"a"`,
	} {
		_, err := svc.Update(ctx, 1, rawPrefs(body))
		var bad *BadRequestError
		if !errors.As(err, &bad) || !strings.Contains(bad.Message, wantKey) {
			t.Errorf("%s: error %v, want a message naming %s", body, err, wantKey)
		}
	}
	if st.merges != 2 {
		t.Errorf("a refused update was saved (merges %d)", st.merges)
	}

	// reading is lenient: unknown keys and old values are ignored
	st.saved = []byte(`{"text_size": "gigantic", "theme": "light", "old_option": true}`)
	got, _ = svc.Get(ctx, 1)
	if got["text_size"] != "normal" || got["theme"] != "light" || got["old_option"] != nil {
		t.Errorf("lenient read: %v", got)
	}
}

type memPrefs struct {
	saved  []byte
	merges int
}

func (m *memPrefs) GetUserPreferences(context.Context, int64) ([]byte, error) { return m.saved, nil }
func (m *memPrefs) MergeUserPreferences(_ context.Context, _ int64, patch []byte) ([]byte, error) {
	m.merges++
	cur := map[string]any{}
	_ = json.Unmarshal(m.saved, &cur)
	add := map[string]any{}
	_ = json.Unmarshal(patch, &add)
	for k, v := range add {
		cur[k] = v
	}
	m.saved, _ = json.Marshal(cur)
	return m.saved, nil
}

func rawPrefs(body string) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		panic(err)
	}
	return m
}
