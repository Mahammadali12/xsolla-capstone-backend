package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/store"
)

// fakeUserStore is an in-memory service.UserStore for password tests.
type fakeUserStore struct {
	user *store.User

	updatedID   int64
	updatedHash string
}

func (f *fakeUserStore) GetUserCount(ctx context.Context) (int, error) { return 1, nil }
func (f *fakeUserStore) CreateUser(ctx context.Context, email, name string, roles []string, passwordHash string) (int64, error) {
	return 0, nil
}
func (f *fakeUserStore) GetUserByEmail(ctx context.Context, email string) (*store.User, error) {
	if f.user == nil {
		return nil, store.ErrUserNotFound
	}
	return f.user, nil
}
func (f *fakeUserStore) GetUserRoles(ctx context.Context, userID int64) ([]string, map[string]time.Time, error) {
	return nil, nil, nil
}
func (f *fakeUserStore) GetUserByID(ctx context.Context, id int64) (*store.User, error) {
	if f.user == nil {
		return nil, store.ErrUserNotFound
	}
	return f.user, nil
}
func (f *fakeUserStore) GetUserPasswordHash(ctx context.Context, id int64) (string, error) {
	if f.user == nil {
		return "", store.ErrUserNotFound
	}
	return f.user.PasswordHash, nil
}
func (f *fakeUserStore) UpdateUserPassword(ctx context.Context, id int64, passwordHash string) (sql.Result, error) {
	f.updatedID = id
	f.updatedHash = passwordHash
	return nil, nil
}
func (f *fakeUserStore) SaveGoogleRefreshToken(ctx context.Context, userID int64, encryptedToken string, expiresAt time.Time) error {
	return nil
}
func (f *fakeUserStore) GetGoogleRefreshToken(ctx context.Context, userID int64) (string, time.Time, error) {
	return "", time.Time{}, nil
}
func (f *fakeUserStore) DeleteGoogleRefreshToken(ctx context.Context, userID int64) error { return nil }

// fakeRefreshTokenStore satisfies the interface; password changes leave
// sessions alone, so nothing should be called.
type fakeRefreshTokenStore struct{ deletedForUser bool }

func (f *fakeRefreshTokenStore) SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	return nil
}
func (f *fakeRefreshTokenStore) DeleteRefreshTokensForUser(ctx context.Context, userID int64) error {
	f.deletedForUser = true
	return nil
}
func (f *fakeRefreshTokenStore) FindRefreshTokenByHash(ctx context.Context, tokenHash string) (int64, string, time.Time, error) {
	return 0, "", time.Time{}, nil
}
func (f *fakeRefreshTokenStore) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	return nil
}
func (f *fakeRefreshTokenStore) RetireRefreshToken(context.Context, string, time.Duration) error {
	return nil
}

const oldPassword = "Old-Passw0rd!"

func passwordTestUser(t *testing.T) *store.User {
	t.Helper()
	hash, err := auth.Hash(oldPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	return &store.User{ID: 42, Email: "alice@example.com", Name: "Alice", PasswordHash: hash}
}

func TestChangePassword(t *testing.T) {
	userStore := &fakeUserStore{user: passwordTestUser(t)}
	tokens := &fakeRefreshTokenStore{}
	svc := NewAuthService(userStore, tokens)

	const newPassword = "Str0ng-Passphrase!"
	if err := svc.ChangePassword(context.Background(), 42, oldPassword, newPassword); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if userStore.updatedID != 42 {
		t.Fatalf("updated user %d, want 42", userStore.updatedID)
	}
	if err := auth.Compare(userStore.updatedHash, newPassword); err != nil {
		t.Fatalf("new password does not verify: %v", err)
	}
	if err := auth.Compare(userStore.updatedHash, oldPassword); err == nil {
		t.Fatal("old password still verifies")
	}
	if tokens.deletedForUser {
		t.Fatal("sessions should not be revoked on password change")
	}
}

func TestChangePasswordWrongOldPassword(t *testing.T) {
	userStore := &fakeUserStore{user: passwordTestUser(t)}
	svc := NewAuthService(userStore, &fakeRefreshTokenStore{})

	err := svc.ChangePassword(context.Background(), 42, "not-the-password", "Str0ng-Passphrase!")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
	if userStore.updatedHash != "" {
		t.Fatal("password must not change when the old password is wrong")
	}
}

func TestChangePasswordRejectsWeakPassword(t *testing.T) {
	userStore := &fakeUserStore{user: passwordTestUser(t)}
	svc := NewAuthService(userStore, &fakeRefreshTokenStore{})

	if err := svc.ChangePassword(context.Background(), 42, oldPassword, "short"); err == nil {
		t.Fatal("expected a policy error")
	}
	if userStore.updatedHash != "" {
		t.Fatal("password must not be updated when the policy rejects it")
	}
}

func TestChangePasswordRejectsPersonalPassword(t *testing.T) {
	userStore := &fakeUserStore{user: passwordTestUser(t)}
	svc := NewAuthService(userStore, &fakeRefreshTokenStore{})

	if err := svc.ChangePassword(context.Background(), 42, oldPassword, "Alice-Secret-42"); err == nil {
		t.Fatal("expected a policy error for a password containing the name")
	}
	if userStore.updatedHash != "" {
		t.Fatal("password must not be updated when the policy rejects it")
	}
}
