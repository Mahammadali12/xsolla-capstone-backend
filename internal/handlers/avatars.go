package handlers

import (
	"context"
	"log"
	"net/http"
	"strings"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
)

// AvatarSource gives the picture file names of people, as a viewer is allowed to see them.
type AvatarSource interface {
	VisibleAvatars(ctx context.Context, viewerID int64, viewerRoles []string, userIDs []int64) (map[int64]string, error)
}

// avatarSupport adds avatar_url to the person objects a handler returns. It is embedded in the handlers that
// return people; without SetAvatars every avatar_url stays null.
type avatarSupport struct {
	avatarSrc AvatarSource
	publicURL string
}

// SetAvatars turns on avatar_url in responses. publicURL is the API's own address as browsers reach it
// (PUBLIC_API_URL); when empty it is taken from the request.
func (a *avatarSupport) SetAvatars(src AvatarSource, publicURL string) {
	a.avatarSrc = src
	a.publicURL = publicURL
}

// avatarURLs returns picture URLs for the given people as the signed-in caller may see them.
func (a *avatarSupport) avatarURLs(r *http.Request, userIDs ...int64) map[int64]*string {
	return a.avatarURLsFor(r, middleware.CtxUserID(r.Context()), middleware.CtxRoles(r.Context()), userIDs...)
}

// avatarURLsFor is avatarURLs for an explicit viewer (e.g. the person who just logged in). A failed lookup only
// costs the pictures: it is logged and the response goes out without them.
func (a *avatarSupport) avatarURLsFor(r *http.Request, viewerID int64, viewerRoles []string, userIDs ...int64) map[int64]*string {
	if a.avatarSrc == nil || len(userIDs) == 0 {
		return nil
	}
	names, err := a.avatarSrc.VisibleAvatars(r.Context(), viewerID, viewerRoles, userIDs)
	if err != nil {
		log.Printf("avatars: lookup failed, answering without pictures: %v", err)
		return nil
	}
	out := make(map[int64]*string, len(names))
	for id, name := range names {
		u := avatarURL(r, a.publicURL, name)
		out[id] = &u
	}
	return out
}

// fillAttendees sets avatar_url on every attendee of the given lists, with one lookup for all of them.
func (a *avatarSupport) fillAttendees(r *http.Request, lists ...[]dto.Attendee) {
	var ids []int64
	for _, list := range lists {
		for _, at := range list {
			ids = append(ids, at.ID)
		}
	}
	urls := a.avatarURLs(r, ids...)
	for _, list := range lists {
		for i := range list {
			list[i].AvatarURL = urls[list[i].ID]
		}
	}
}

// fillBriefs sets avatar_url on a team's mentor (may be nil) and members, with one lookup.
func (a *avatarSupport) fillBriefs(r *http.Request, mentor *dto.UserBrief, members []dto.UserBrief) {
	ids := make([]int64, 0, len(members)+1)
	if mentor != nil {
		ids = append(ids, mentor.ID)
	}
	for _, m := range members {
		ids = append(ids, m.ID)
	}
	urls := a.avatarURLs(r, ids...)
	if mentor != nil {
		mentor.AvatarURL = urls[mentor.ID]
	}
	for i := range members {
		members[i].AvatarURL = urls[members[i].ID]
	}
}

// avatarURL is the address an <img> tag can load without a token: GET /avatars/{name}.
func avatarURL(r *http.Request, publicURL, name string) string {
	return publicBaseURL(r, publicURL) + "/avatars/" + name
}

// publicBaseURL is PUBLIC_API_URL when set, otherwise the scheme and host the request came in on (behind a proxy
// the X-Forwarded-Proto and X-Forwarded-Host headers it sets).
func publicBaseURL(r *http.Request, configured string) string {
	if configured != "" {
		return strings.TrimRight(configured, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := firstHeaderValue(r.Header.Get("X-Forwarded-Proto")); p == "http" || p == "https" {
		scheme = p
	}
	host := r.Host
	if h := firstHeaderValue(r.Header.Get("X-Forwarded-Host")); h != "" {
		host = h
	}
	return scheme + "://" + host
}

func firstHeaderValue(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}
