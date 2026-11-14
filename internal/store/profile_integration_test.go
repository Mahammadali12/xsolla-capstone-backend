package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func profPtr(s string) *string { return &s }

func TestUserProfileStore(t *testing.T) {
	database := testDatabase(t)
	st := NewStore(database)
	ctx := context.Background()
	id := insertTestUser(t, database, fmt.Sprintf("profile-%d@example.com", time.Now().UnixNano()), "Profile Owner")

	if _, err := st.GetUserProfile(ctx, id); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("before any save: %v, want ErrProfileNotFound", err)
	}

	p, err := st.UpdateUserProfile(ctx, id, ProfilePatch{Bio: profPtr("Go and cats.")})
	if err != nil || p.Bio != "Go and cats." || p.SlackHandle != "" || p.Visibility != "everyone" || p.AvatarKey != nil {
		t.Fatalf("first save: %+v, %v", p, err)
	}
	p, err = st.UpdateUserProfile(ctx, id, ProfilePatch{SlackHandle: profPtr("rashad.a"), Visibility: profPtr("staff_only")})
	if err != nil || p.Bio != "Go and cats." || p.SlackHandle != "rashad.a" || p.Visibility != "staff_only" {
		t.Fatalf("partial update: %+v, %v", p, err)
	}

	// the table refuses what the service would refuse
	for name, patch := range map[string]ProfilePatch{
		"bio over 300":       {Bio: profPtr(strings.Repeat("ə", 301))},
		"handle over 40":     {GitlabHandle: profPtr("12345678901234567890123456789012345678901")},
		"unknown visibility": {Visibility: profPtr("friends")},
	} {
		if _, err := st.UpdateUserProfile(ctx, id, patch); err == nil {
			t.Errorf("%s: the table accepted it", name)
		}
	}

	// pictures: first set, replace, remove, remove again
	first, second := "avatars/"+fmt.Sprint(time.Now().UnixNano())+"a.png", "avatars/"+fmt.Sprint(time.Now().UnixNano())+"b.jpg"
	if old, err := st.SetUserAvatar(ctx, id, &first); err != nil || old != nil {
		t.Fatalf("first picture: old %v, %v", old, err)
	}
	if old, err := st.SetUserAvatar(ctx, id, &second); err != nil || old == nil || *old != first {
		t.Fatalf("replace: old %v, %v; want %s", old, err, first)
	}
	if ok, _ := st.IsCurrentAvatar(ctx, second); !ok {
		t.Error("the new picture is not current")
	}
	if ok, _ := st.IsCurrentAvatar(ctx, first); ok {
		t.Error("the replaced picture is still current")
	}
	list, err := st.ListAvatars(ctx, []int64{id, id + 1000000})
	if err != nil || len(list) != 1 || list[0].AvatarKey != second || list[0].Visibility != "staff_only" {
		t.Errorf("ListAvatars: %+v, %v", list, err)
	}
	if list, err := st.ListAvatars(ctx, nil); err != nil || len(list) != 0 {
		t.Errorf("ListAvatars of nobody: %+v, %v", list, err)
	}
	if old, err := st.SetUserAvatar(ctx, id, nil); err != nil || old == nil || *old != second {
		t.Errorf("remove: old %v, %v", old, err)
	}
	if old, err := st.SetUserAvatar(ctx, id, nil); err != nil || old != nil {
		t.Errorf("remove again: old %v, %v", old, err)
	}
	p, _ = st.GetUserProfile(ctx, id)
	if p.AvatarKey != nil || p.Bio != "Go and cats." {
		t.Errorf("after removing the picture the rest must stay: %+v", p)
	}

	// removing a picture of someone without a profile creates nothing
	other := insertTestUser(t, database, fmt.Sprintf("profile-other-%d@example.com", time.Now().UnixNano()), "No Profile")
	if old, err := st.SetUserAvatar(ctx, other, nil); err != nil || old != nil {
		t.Errorf("remove without profile: %v, %v", old, err)
	}
	if _, err := st.GetUserProfile(ctx, other); !errors.Is(err, ErrProfileNotFound) {
		t.Errorf("a profile row was created by a removal: %v", err)
	}
}

func TestUserPreferencesStore(t *testing.T) {
	database := testDatabase(t)
	st := NewStore(database)
	ctx := context.Background()
	id := insertTestUser(t, database, fmt.Sprintf("prefs-%d@example.com", time.Now().UnixNano()), "Prefs Owner")

	if saved, err := st.GetUserPreferences(ctx, id); err != nil || saved != nil {
		t.Fatalf("before any save: %s, %v", saved, err)
	}
	if _, err := st.MergeUserPreferences(ctx, id, []byte(`{"text_size": "large", "high_contrast": true}`)); err != nil {
		t.Fatal(err)
	}
	saved, err := st.MergeUserPreferences(ctx, id, []byte(`{"theme": "dark", "high_contrast": false}`))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(saved, &got)
	if got["text_size"] != "large" || got["theme"] != "dark" || got["high_contrast"] != false || len(got) != 3 {
		t.Errorf("merged: %s", saved)
	}
	if again, _ := st.GetUserPreferences(ctx, id); string(again) != string(saved) {
		t.Errorf("read back %s, want %s", again, saved)
	}
	if _, err := st.MergeUserPreferences(ctx, id, []byte(`["not", "an", "object"]`)); err == nil {
		t.Error("the table accepted a JSON array")
	}
}

func TestProfileRowsGoWithTheUser(t *testing.T) {
	database := testDatabase(t)
	st := NewStore(database)
	ctx := context.Background()
	id := insertTestUser(t, database, fmt.Sprintf("profile-gone-%d@example.com", time.Now().UnixNano()), "Leaving")
	key := "avatars/" + fmt.Sprint(time.Now().UnixNano()) + "c.png"
	if _, err := st.SetUserAvatar(ctx, id, &key); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MergeUserPreferences(ctx, id, []byte(`{"theme": "dark"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM users WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.IsCurrentAvatar(ctx, key); ok {
		t.Error("a deleted user's picture is still served")
	}
	if saved, _ := st.GetUserPreferences(ctx, id); saved != nil {
		t.Error("a deleted user's settings are still there")
	}
}
