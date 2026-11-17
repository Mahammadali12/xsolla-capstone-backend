package dto

// ProfileResponse is the "about me" part of a profile (GET/PATCH /users/me/profile, GET /users/{id}/profile).
// AvatarURL is null when there is no picture or the viewer may not see it.
type ProfileResponse struct {
	AvatarURL    *string `json:"avatar_url"`
	Bio          string  `json:"bio"`
	SlackHandle  string  `json:"slack_handle"`
	GitlabHandle string  `json:"gitlab_handle"`
	Visibility   string  `json:"visibility"`
}

// AvatarResponse is the answer of PUT /users/me/avatar.
type AvatarResponse struct {
	AvatarURL string `json:"avatar_url"`
}
