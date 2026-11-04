package middleware

import (
	"context"
	"net/http"
	"strings"

	"xsolla-capstone-api/internal/auth"
)

// contextKey is a private type for context values to avoid collisions.
type contextKey int

const (
	userIDKey contextKey = iota
	roleKey
)

// UserID returns the authenticated user id from the request context.
func UserID(r *http.Request) int64 {
	v, _ := r.Context().Value(userIDKey).(int64)
	return v
}

// Role returns the authenticated user role from the request context.
func Role(r *http.Request) string {
	v, _ := r.Context().Value(roleKey).(string)
	return v
}

// CtxUserID returns the user id stored in context by Authenticator.Middleware.
func CtxUserID(ctx context.Context) int64 {
	v, _ := ctx.Value(userIDKey).(int64)
	return v
}

// Authenticator wraps JWT verification and sets user context.
type Authenticator struct {
	Secret []byte
}

// Middleware returns an http.Handler that requires a valid access token.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == r.Header.Get("Authorization") {
			http.Error(w, `{"error":"missing token","message":"Authorization header required","status":401}`, http.StatusUnauthorized)
			return
		}
		claims, err := auth.Verify(token, a.Secret)
		if err != nil {
			http.Error(w, `{"error":"invalid token","message":"`+auth.Format(err).Error()+`","status":401}`, http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
		ctx = context.WithValue(ctx, roleKey, claims.Role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalAuth sets user context when a valid token is present, but allows
// the request through without one. Useful for public endpoints that behave
// differently for authenticated users.
func (a *Authenticator) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token != authHeader {
				if claims, err := auth.Verify(token, a.Secret); err == nil {
					ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
					ctx = context.WithValue(ctx, roleKey, claims.Role)
					r = r.WithContext(ctx)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole wraps a handler so only users with the given role may pass.
func (a *Authenticator) RequireRole(role string, next http.Handler) http.Handler {
	return a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if middlewareRole(r) != role {
			http.Error(w, `{"error":"forbidden","message":"insufficient role","status":403}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func middlewareRole(r *http.Request) string {
	v, _ := r.Context().Value(roleKey).(string)
	return v
}
