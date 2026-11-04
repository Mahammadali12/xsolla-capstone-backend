// Package auth implements stateless JWT authentication using HS256.
// The JWT is signed and verified with the standard library only (crypto/hmac,
// crypto/sha256, encoding/base64, encoding/json) — no external JWT library.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

type Claims struct {
	UserID int64  `json:"sub"`
	Role   string `json:"role"`
	Exp    int64  `json:"exp"`
}

// accessSecret and refreshSecret are set from environment at startup.
var (
	AccessSecret  []byte
	RefreshSecret []byte
)

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func b64urlDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func sign(secret []byte, header, payload []byte) (string, error) {
	h := b64url(header)
	p := b64url(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(h + "." + p))
	sig := mac.Sum(nil)
	return h + "." + p + "." + b64url(sig), nil
}

// AccessToken signs a short-lived access token (15 minutes default).
func AccessToken(userID int64, role string, secret []byte, ttl time.Duration) (string, error) {
	now := time.Now()
	h := map[string]any{"alg": "HS256", "typ": "JWT"}
	header, _ := json.Marshal(h)
	p := Claims{UserID: userID, Role: role, Exp: now.Add(ttl).Unix()}
	payload, _ := json.Marshal(p)
	return sign(secret, header, payload)
}

// RefreshToken signs a long-lived refresh token (7 days default).
func RefreshToken(userID int64, secret []byte, ttl time.Duration) (string, error) {
	return AccessToken(userID, "", secret, ttl) // role unused for refresh
}

// Verify parses and verifies a token, returning its claims.
func Verify(tokenString string, secret []byte) (*Claims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}
	if _, err := b64urlDecode(parts[0]); err != nil {
		return nil, ErrInvalidToken
	}
	payloadBytes, err := b64urlDecode(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}
	sigBytes, err := b64urlDecode(parts[2])
	if err != nil {
		return nil, ErrInvalidToken
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	expected := mac.Sum(nil)
	if !hmac.Equal(sigBytes, expected) {
		return nil, ErrInvalidToken
	}

	var claims Claims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, ErrInvalidToken
	}
	if time.Now().Unix() > claims.Exp {
		return nil, ErrExpiredToken
	}
	return &claims, nil
}

// Format wraps the standard JWT error for handler use.
func Format(err error) error {
	switch {
	case errors.Is(err, ErrExpiredToken):
		return fmt.Errorf("token expired: %w", err)
	case errors.Is(err, ErrInvalidToken):
		return fmt.Errorf("invalid token: %w", err)
	default:
		return err
	}
}
