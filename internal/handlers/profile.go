package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"maps"
	"net/http"
	"slices"
	"strconv"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
)

// maxProfileBody is far above the largest valid profile or settings change.
const maxProfileBody = 16 << 10

// ProfileService is what the profile handlers need for "about me" and the picture.
type ProfileService interface {
	MyProfile(ctx context.Context, userID int64) (*service.Profile, error)
	ProfileFor(ctx context.Context, viewerID int64, viewerRoles []string, userID int64) (*service.Profile, error)
	UpdateMyProfile(ctx context.Context, userID int64, upd service.ProfileUpdate) (*service.Profile, error)
	SetAvatar(ctx context.Context, userID int64, data []byte) (string, error)
	RemoveAvatar(ctx context.Context, userID int64) error
	OpenAvatar(ctx context.Context, name string) (io.ReadCloser, string, error)
}

// PreferencesService is what the profile handlers need for the look and accessibility settings.
type PreferencesService interface {
	Get(ctx context.Context, userID int64) (map[string]any, error)
	Update(ctx context.Context, userID int64, patch map[string]json.RawMessage) (map[string]any, error)
}

// ProfileHandlers serve the profile page: about me, the picture, and the look settings.
type ProfileHandlers struct {
	profiles  ProfileService
	prefs     PreferencesService
	publicURL string
}

// NewProfileHandlers creates the handlers. publicURL is PUBLIC_API_URL (may be empty, see publicBaseURL).
func NewProfileHandlers(profiles ProfileService, prefs PreferencesService, publicURL string) *ProfileHandlers {
	return &ProfileHandlers{profiles: profiles, prefs: prefs, publicURL: publicURL}
}

// @Summary     My profile
// @Description The signed-in person's "about me": picture, bio, contact handles and who may see them
// @Tags        profile
// @Produce     json
// @Security    BearerAuth
// @Success     200 {object} dto.ProfileResponse
// @Failure     401 {object} handlers.ErrorResponse
// @Router      /users/me/profile [get]
//
// GetMyProfile handles GET /users/me/profile.
func (h *ProfileHandlers) GetMyProfile(w http.ResponseWriter, r *http.Request) {
	p, err := h.profiles.MyProfile(r.Context(), middleware.CtxUserID(r.Context()))
	if err != nil {
		h.writeErr(w, "GetMyProfile", err)
		return
	}
	writeJSON(w, http.StatusOK, h.profileResponse(r, p))
}

// @Summary     Someone's profile
// @Description As the caller may see it: a "staff_only" profile comes back empty (avatar_url null) to students
// @Tags        profile
// @Produce     json
// @Security    BearerAuth
// @Param       id path int true "User ID"
// @Success     200 {object} dto.ProfileResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /users/{id}/profile [get]
//
// GetProfile handles GET /users/{id}/profile: someone's profile as the caller may see it.
func (h *ProfileHandlers) GetProfile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user id", nil)
		return
	}
	p, err := h.profiles.ProfileFor(r.Context(), middleware.CtxUserID(r.Context()), middleware.CtxRoles(r.Context()), id)
	if err != nil {
		h.writeErr(w, "GetProfile", err)
		return
	}
	writeJSON(w, http.StatusOK, h.profileResponse(r, p))
}

// @Summary     Change my profile
// @Description Any of bio (max 300), slack_handle, gitlab_handle (max 40, no spaces), visibility (everyone, staff_only). Only sent fields change.
// @Tags        profile
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       request body object true "Fields to change"
// @Success     200 {object} dto.ProfileResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Router      /users/me/profile [patch]
//
// UpdateMyProfile handles PATCH /users/me/profile: any of bio, slack_handle, gitlab_handle, visibility.
func (h *ProfileHandlers) UpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	fields, ok := decodeObject(w, r)
	if !ok {
		return
	}
	var upd service.ProfileUpdate
	targets := map[string]**string{
		"bio": &upd.Bio, "slack_handle": &upd.SlackHandle, "gitlab_handle": &upd.GitlabHandle, "visibility": &upd.Visibility,
	}
	for _, key := range sortedKeys(fields) {
		target, known := targets[key]
		switch {
		case key == "avatar_url":
			writeError(w, http.StatusBadRequest, "bad_request", "avatar_url is changed with PUT /users/me/avatar", nil)
			return
		case !known:
			writeError(w, http.StatusBadRequest, "bad_request", strconv.Quote(key)+" is not a profile field: use bio, slack_handle, gitlab_handle, visibility", nil)
			return
		}
		var v string
		if raw := bytes.TrimSpace(fields[key]); len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &v) != nil {
			writeError(w, http.StatusBadRequest, "bad_request", key+" must be a string", nil)
			return
		}
		*target = &v
	}
	p, err := h.profiles.UpdateMyProfile(r.Context(), middleware.CtxUserID(r.Context()), upd)
	if err != nil {
		h.writeErr(w, "UpdateMyProfile", err)
		return
	}
	writeJSON(w, http.StatusOK, h.profileResponse(r, p))
}

// @Summary     Upload my picture
// @Description JPG, PNG or WebP up to 2 MB (content is checked). Replaces and deletes the old picture.
// @Tags        profile
// @Accept      multipart/form-data
// @Produce     json
// @Security    BearerAuth
// @Param       file formData file true "The picture"
// @Success     200 {object} dto.AvatarResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Router      /users/me/avatar [put]
//
// PutMyAvatar handles PUT /users/me/avatar: multipart/form-data with one "file" field.
func (h *ProfileHandlers) PutMyAvatar(w http.ResponseWriter, r *http.Request) {
	// room for the multipart headers around a 2 MB picture
	r.Body = http.MaxBytesReader(w, r.Body, service.AvatarMaxBytes+64<<10)
	if err := r.ParseMultipartForm(service.AvatarMaxBytes + 64<<10); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusBadRequest, "bad_request", service.AvatarRule, nil)
		} else {
			writeError(w, http.StatusBadRequest, "bad_request", "send the picture as multipart/form-data in a field named file", nil)
		}
		return
	}
	defer r.MultipartForm.RemoveAll()
	f, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "send the picture as multipart/form-data in a field named file", nil)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, service.AvatarMaxBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not read the picture", nil)
		return
	}
	if len(data) > service.AvatarMaxBytes {
		writeError(w, http.StatusBadRequest, "bad_request", service.AvatarRule, nil)
		return
	}
	name, err := h.profiles.SetAvatar(r.Context(), middleware.CtxUserID(r.Context()), data)
	if err != nil {
		h.writeErr(w, "PutMyAvatar", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.AvatarResponse{AvatarURL: avatarURL(r, h.publicURL, name)})
}

// @Summary     Remove my picture
// @Tags        profile
// @Security    BearerAuth
// @Success     204 {string} string "No content"
// @Router      /users/me/avatar [delete]
//
// DeleteMyAvatar handles DELETE /users/me/avatar. 204 also when there was no picture.
func (h *ProfileHandlers) DeleteMyAvatar(w http.ResponseWriter, r *http.Request) {
	if err := h.profiles.RemoveAvatar(r.Context(), middleware.CtxUserID(r.Context())); err != nil {
		h.writeErr(w, "DeleteMyAvatar", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary     A profile picture
// @Description No sign-in: this is the avatar_url an <img> tag loads. Only current pictures are served.
// @Tags        profile
// @Produce     image/jpeg,image/png,image/webp
// @Param       name path string true "File name from avatar_url"
// @Success     200 {file} file
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /avatars/{name} [get]
//
// ServeAvatar handles GET /avatars/{name}, without sign-in so an <img> tag can load it. Only current pictures are
// served; the long random name is what keeps them from being guessed.
func (h *ProfileHandlers) ServeAvatar(w http.ResponseWriter, r *http.Request) {
	rc, contentType, err := h.profiles.OpenAvatar(r.Context(), r.PathValue("name"))
	if errors.Is(err, service.ErrAvatarNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "picture not found", nil)
		return
	}
	if err != nil {
		log.Printf("profile handler: ServeAvatar error: %v", err)
		writeError(w, http.StatusInternalServerError, "server_error", "could not load the picture", nil)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// a picture never changes under its name: a new upload gets a new name
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if _, err := io.Copy(w, rc); err != nil {
		log.Printf("profile handler: ServeAvatar copy error: %v", err)
	}
}

// @Summary     My look and accessibility settings
// @Description Every setting; defaults for a person who never saved any
// @Tags        profile
// @Produce     json
// @Security    BearerAuth
// @Success     200 {object} map[string]interface{}
// @Router      /users/me/preferences [get]
//
// GetMyPreferences handles GET /users/me/preferences: every setting, defaults included.
func (h *ProfileHandlers) GetMyPreferences(w http.ResponseWriter, r *http.Request) {
	prefs, err := h.prefs.Get(r.Context(), middleware.CtxUserID(r.Context()))
	if err != nil {
		h.writeErr(w, "GetMyPreferences", err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

// @Summary     Change my look and accessibility settings
// @Description text_size (normal, large, xlarge), theme (system, light, dark), font (default, dyslexia), spacing (normal, relaxed), density (comfortable, compact), high_contrast, colorblind_safe, reduce_motion, strong_focus (true/false)
// @Tags        profile
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       request body object true "Settings to change"
// @Success     200 {object} map[string]interface{}
// @Failure     400 {object} handlers.ErrorResponse
// @Router      /users/me/preferences [patch]
//
// UpdateMyPreferences handles PATCH /users/me/preferences: only the sent keys change.
func (h *ProfileHandlers) UpdateMyPreferences(w http.ResponseWriter, r *http.Request) {
	fields, ok := decodeObject(w, r)
	if !ok {
		return
	}
	prefs, err := h.prefs.Update(r.Context(), middleware.CtxUserID(r.Context()), fields)
	if err != nil {
		h.writeErr(w, "UpdateMyPreferences", err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (h *ProfileHandlers) profileResponse(r *http.Request, p *service.Profile) dto.ProfileResponse {
	resp := dto.ProfileResponse{Bio: p.Bio, SlackHandle: p.SlackHandle, GitlabHandle: p.GitlabHandle, Visibility: p.Visibility}
	if p.AvatarFile != nil {
		u := avatarURL(r, h.publicURL, *p.AvatarFile)
		resp.AvatarURL = &u
	}
	return resp
}

func (h *ProfileHandlers) writeErr(w http.ResponseWriter, op string, err error) {
	var bad *service.BadRequestError
	switch {
	case errors.As(err, &bad):
		writeError(w, http.StatusBadRequest, "bad_request", bad.Message, nil)
	case errors.Is(err, service.ErrAvatarInvalid):
		writeError(w, http.StatusBadRequest, "bad_request", service.AvatarRule, nil)
	case errors.Is(err, service.ErrProfileUserNotFound):
		writeError(w, http.StatusNotFound, "not_found", "user not found", nil)
	default:
		log.Printf("profile handler: %s error: %v", op, err)
		writeError(w, http.StatusInternalServerError, "server_error", "could not handle the profile", nil)
	}
}

// decodeObject reads one JSON object (at most 16 KB) as raw fields. Anything else is a 400.
func decodeObject(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, bool) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxProfileBody))
	var fields map[string]json.RawMessage
	err := dec.Decode(&fields)
	if err == nil && dec.Decode(&struct{}{}) != io.EOF {
		err = errors.New("trailing data")
	}
	if err == nil && fields == nil {
		err = errors.New("null body")
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "the request is too large", nil)
		} else {
			writeError(w, http.StatusBadRequest, "bad_request", "send one JSON object", nil)
		}
		return nil, false
	}
	return fields, true
}

// sortedKeys keeps the first reported error the same whatever order the client sent the fields in.
func sortedKeys(m map[string]json.RawMessage) []string {
	return slices.Sorted(maps.Keys(m))
}
