package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the JPEG decoder for image.DecodeConfig
	_ "image/png"  // registers the PNG decoder for image.DecodeConfig
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/store"
)

// Profile page, part 1 ("about me"): picture, bio and contact handles that other people can see, with the
// owner's choice of who may see them. Requirements: the front-end's backend request for the profile page.

const (
	ProfileMaxBio    = 300
	ProfileMaxHandle = 40
	AvatarMaxBytes   = 2 << 20 // 2 MB

	VisibilityEveryone  = "everyone"
	VisibilityStaffOnly = "staff_only"

	avatarKeyPrefix = "avatars/"
)

// AvatarRule is the message for every refused picture.
const AvatarRule = "Use a JPG, PNG or WebP picture up to 2 MB."

var (
	// ErrProfileUserNotFound is returned for a profile of an unknown or deleted user.
	ErrProfileUserNotFound = errors.New("user not found")
	// ErrAvatarNotFound is returned for a picture name that is not anyone's current picture.
	ErrAvatarNotFound = errors.New("picture not found")
	// ErrAvatarInvalid is returned for a file that is not a JPG, PNG or WebP picture up to 2 MB.
	ErrAvatarInvalid = errors.New(AvatarRule)

	avatarFileName = regexp.MustCompile(`^[0-9a-f]{32}\.(jpg|png|webp)$`)
)

// BadRequestError is a value the client may not send; Message says which rule it breaks.
type BadRequestError struct{ Message string }

func (e *BadRequestError) Error() string { return e.Message }

func badRequest(format string, args ...any) error {
	return &BadRequestError{Message: fmt.Sprintf(format, args...)}
}

// Profile is a profile as one viewer may see it. AvatarFile is the picture's public file name, nil when there
// is none or the viewer may not see it.
type Profile struct {
	AvatarFile   *string
	Bio          string
	SlackHandle  string
	GitlabHandle string
	Visibility   string
}

// ProfileUpdate is what the client sent; nil fields were not sent.
type ProfileUpdate struct {
	Bio          *string
	SlackHandle  *string
	GitlabHandle *string
	Visibility   *string
}

type profileStore interface {
	GetUserByID(ctx context.Context, id int64) (*store.User, error)
	GetUserProfile(ctx context.Context, userID int64) (*store.UserProfile, error)
	UpdateUserProfile(ctx context.Context, userID int64, patch store.ProfilePatch) (*store.UserProfile, error)
	SetUserAvatar(ctx context.Context, userID int64, key *string) (*string, error)
	IsCurrentAvatar(ctx context.Context, key string) (bool, error)
	ListAvatars(ctx context.Context, userIDs []int64) ([]store.AvatarVisibility, error)
}

// ProfileService handles the "about me" part of the profile page and the pictures.
type ProfileService struct {
	store   profileStore
	storage Storage
}

// NewProfileService creates the service. Pictures are kept in the same object storage as uploaded files.
func NewProfileService(s profileStore, st Storage) *ProfileService {
	return &ProfileService{store: s, storage: st}
}

// MyProfile returns the caller's own profile (the defaults when nothing was saved yet).
func (s *ProfileService) MyProfile(ctx context.Context, userID int64) (*Profile, error) {
	p, err := s.load(ctx, userID)
	if err != nil {
		return nil, err
	}
	return toProfile(p), nil
}

// ProfileFor returns another person's profile as the viewer may see it. When the owner chose "staff_only" and
// the viewer is not staff, the picture, bio and handles come back empty (the page then shows initials).
func (s *ProfileService) ProfileFor(ctx context.Context, viewerID int64, viewerRoles []string, userID int64) (*Profile, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if errors.Is(err, store.ErrUserNotFound) || (err == nil && u.DeletedAt != nil) {
		return nil, ErrProfileUserNotFound
	}
	if err != nil {
		return nil, err
	}
	p, err := s.load(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !canSeeProfile(viewerID, viewerRoles, userID, p.Visibility) {
		return &Profile{Visibility: p.Visibility}, nil
	}
	return toProfile(p), nil
}

// UpdateMyProfile changes the fields that were sent and returns the whole profile.
func (s *ProfileService) UpdateMyProfile(ctx context.Context, userID int64, upd ProfileUpdate) (*Profile, error) {
	patch, err := validateProfileUpdate(upd)
	if err != nil {
		return nil, err
	}
	before, err := s.load(ctx, userID)
	if err != nil {
		return nil, err
	}
	after, err := s.store.UpdateUserProfile(ctx, userID, patch)
	if err != nil {
		return nil, err
	}
	// The picture URL is public to whoever knows it. When the owner hides it from students, give the picture a
	// new name so a URL a student saw before stops working.
	if before.Visibility != VisibilityStaffOnly && after.Visibility == VisibilityStaffOnly && after.AvatarKey != nil {
		if newKey, err := s.renameAvatar(ctx, userID, *after.AvatarKey); err != nil {
			log.Printf("profile: could not rename the picture of user %d after hiding it: %v", userID, err)
		} else {
			after.AvatarKey = &newKey
		}
	}
	return toProfile(after), nil
}

// SetAvatar stores a new picture and returns its public file name. The old picture is deleted.
func (s *ProfileService) SetAvatar(ctx context.Context, userID int64, data []byte) (string, error) {
	ext, mime, ok := detectAvatar(data)
	if !ok {
		return "", ErrAvatarInvalid
	}
	key, err := newAvatarKey(ext)
	if err != nil {
		return "", err
	}
	if err := s.storage.Upload(ctx, 0, "avatar."+ext, key, mime, int64(len(data)), bytes.NewReader(data)); err != nil {
		return "", err
	}
	old, err := s.store.SetUserAvatar(ctx, userID, &key)
	if err != nil {
		s.deleteObject(ctx, key) // not saved: do not leave the file behind
		return "", err
	}
	if old != nil {
		s.deleteObject(ctx, *old)
	}
	return strings.TrimPrefix(key, avatarKeyPrefix), nil
}

// RemoveAvatar deletes the picture. Removing when there is none is not an error.
func (s *ProfileService) RemoveAvatar(ctx context.Context, userID int64) error {
	old, err := s.store.SetUserAvatar(ctx, userID, nil)
	if err != nil {
		return err
	}
	if old != nil {
		s.deleteObject(ctx, *old)
	}
	return nil
}

// OpenAvatar opens a current picture by its public file name. Only current pictures are served, so this route
// can never be used to read other stored files (hand-ins, materials).
func (s *ProfileService) OpenAvatar(ctx context.Context, name string) (io.ReadCloser, string, error) {
	m := avatarFileName.FindStringSubmatch(name)
	if m == nil {
		return nil, "", ErrAvatarNotFound
	}
	current, err := s.store.IsCurrentAvatar(ctx, avatarKeyPrefix+name)
	if err != nil {
		return nil, "", err
	}
	if !current {
		return nil, "", ErrAvatarNotFound
	}
	rc, err := s.storage.Download(ctx, avatarKeyPrefix+name)
	if err != nil {
		return nil, "", err
	}
	return rc, avatarMIME[m[1]], nil
}

// VisibleAvatars returns, for the given people, the picture file names the viewer may see. People without a
// picture, or whose picture is hidden from the viewer, are left out.
func (s *ProfileService) VisibleAvatars(ctx context.Context, viewerID int64, viewerRoles []string, userIDs []int64) (map[int64]string, error) {
	seen := map[int64]bool{}
	ids := make([]int64, 0, len(userIDs))
	for _, id := range userIDs {
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	rows, err := s.store.ListAvatars(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(rows))
	for _, a := range rows {
		if canSeeProfile(viewerID, viewerRoles, a.UserID, a.Visibility) {
			out[a.UserID] = strings.TrimPrefix(a.AvatarKey, avatarKeyPrefix)
		}
	}
	return out, nil
}

// canSeeProfile: the owner and staff (mentor, lecturer, admin) see everything; others only "everyone" profiles.
func canSeeProfile(viewerID int64, viewerRoles []string, ownerID int64, visibility string) bool {
	return viewerID == ownerID || visibility != VisibilityStaffOnly ||
		auth.HasRole(viewerRoles, "mentor") || auth.HasRole(viewerRoles, "lecturer") || auth.HasRole(viewerRoles, "admin")
}

func (s *ProfileService) load(ctx context.Context, userID int64) (*store.UserProfile, error) {
	p, err := s.store.GetUserProfile(ctx, userID)
	if errors.Is(err, store.ErrProfileNotFound) {
		return &store.UserProfile{UserID: userID, Visibility: VisibilityEveryone}, nil
	}
	return p, err
}

func (s *ProfileService) renameAvatar(ctx context.Context, userID int64, oldKey string) (string, error) {
	ext := oldKey[strings.LastIndex(oldKey, ".")+1:]
	rc, err := s.storage.Download(ctx, oldKey)
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(rc, AvatarMaxBytes+1))
	rc.Close()
	if err != nil {
		return "", err
	}
	newKey, err := newAvatarKey(ext)
	if err != nil {
		return "", err
	}
	if err := s.storage.Upload(ctx, 0, "avatar."+ext, newKey, avatarMIME[ext], int64(len(data)), bytes.NewReader(data)); err != nil {
		return "", err
	}
	replaced, err := s.store.SetUserAvatar(ctx, userID, &newKey)
	if err != nil {
		s.deleteObject(ctx, newKey)
		return "", err
	}
	if replaced != nil {
		s.deleteObject(ctx, *replaced)
	}
	return newKey, nil
}

func (s *ProfileService) deleteObject(ctx context.Context, key string) {
	if err := s.storage.Delete(ctx, key); err != nil {
		log.Printf("profile: could not delete old picture %s: %v", key, err)
	}
}

func toProfile(p *store.UserProfile) *Profile {
	out := &Profile{Bio: p.Bio, SlackHandle: p.SlackHandle, GitlabHandle: p.GitlabHandle, Visibility: p.Visibility}
	if p.AvatarKey != nil {
		name := strings.TrimPrefix(*p.AvatarKey, avatarKeyPrefix)
		out.AvatarFile = &name
	}
	return out
}

func newAvatarKey(ext string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return avatarKeyPrefix + hex.EncodeToString(b) + "." + ext, nil
}

var avatarMIME = map[string]string{"jpg": "image/jpeg", "png": "image/png", "webp": "image/webp"}

// detectAvatar checks the real content, not the file name: the bytes must be a JPEG, PNG or WebP picture of at
// most 2 MB. JPEG and PNG must also decode as pictures; WebP is checked by its header.
func detectAvatar(data []byte) (ext, mime string, ok bool) {
	if len(data) == 0 || len(data) > AvatarMaxBytes {
		return "", "", false
	}
	switch http.DetectContentType(data) {
	case "image/jpeg":
		ext = "jpg"
	case "image/png":
		ext = "png"
	case "image/webp":
		return "webp", "image/webp", len(data) >= 16
	default:
		return "", "", false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 {
		return "", "", false
	}
	return ext, avatarMIME[ext], true
}

func validateProfileUpdate(upd ProfileUpdate) (store.ProfilePatch, error) {
	var patch store.ProfilePatch
	if upd.Bio != nil {
		bio := strings.TrimSpace(*upd.Bio)
		if n := utf8.RuneCountInString(bio); n > ProfileMaxBio {
			return patch, badRequest("bio must be at most %d characters (it has %d)", ProfileMaxBio, n)
		}
		if hasControl(bio, true) {
			return patch, badRequest("bio contains characters that are not allowed")
		}
		patch.Bio = &bio
	}
	var err error
	if patch.SlackHandle, err = validHandle("slack_handle", upd.SlackHandle); err != nil {
		return patch, err
	}
	if patch.GitlabHandle, err = validHandle("gitlab_handle", upd.GitlabHandle); err != nil {
		return patch, err
	}
	if upd.Visibility != nil {
		v := strings.TrimSpace(*upd.Visibility)
		if v != VisibilityEveryone && v != VisibilityStaffOnly {
			return patch, badRequest("visibility must be %q or %q", VisibilityEveryone, VisibilityStaffOnly)
		}
		patch.Visibility = &v
	}
	return patch, nil
}

// validHandle: a username without spaces, up to 40 characters; a leading "@" is dropped; "" clears it.
func validHandle(field string, v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	h := strings.TrimPrefix(strings.TrimSpace(*v), "@")
	if n := utf8.RuneCountInString(h); n > ProfileMaxHandle {
		return nil, badRequest("%s must be at most %d characters (it has %d)", field, ProfileMaxHandle, n)
	}
	if strings.IndexFunc(h, unicode.IsSpace) >= 0 || hasControl(h, false) {
		return nil, badRequest("%s must be a username without spaces, e.g. rashad.a", field)
	}
	return &h, nil
}

// hasControl reports control characters (a database text value cannot hold NUL). New lines are allowed in a bio.
func hasControl(s string, allowNewlines bool) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		if allowNewlines && (r == '\n' || r == '\r' || r == '\t') {
			return false
		}
		return unicode.IsControl(r)
	}) >= 0
}
