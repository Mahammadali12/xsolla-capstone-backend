package dto

import "time"

// SignupRequest is the payload for POST /auth/signup.
type SignupRequest struct {
	Email    string `json:"email" example:"alice@xsolla.com"`
	Name     string `json:"name" example:"Alice"`
	Password string `json:"password" example:"secret123"`
}

// LoginRequest is the payload for POST /auth/login.
type LoginRequest struct {
	Login    string `json:"login" example:"alice@xsolla.com"`
	Password string `json:"password" example:"secret123"`
}

// RefreshRequest is the payload for POST /auth/refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// LogoutRequest is the payload for POST /auth/logout.
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// UserResponse is the user object returned in auth responses.
type UserResponse struct {
	ID         int64     `json:"id"`
	Email      string    `json:"email"`
	Name       string    `json:"name"`
	Role       string    `json:"role"`
	CreatedAt  time.Time `json:"created_at"`
}

// AuthResponse is the payload for successful signup/login.
type AuthResponse struct {
	User         UserResponse `json:"user"`
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
}

// TokenResponse is the payload for successful refresh.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}
