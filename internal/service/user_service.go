package service

import (
	"context"
	"errors"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/store"
)

// UserService manages users, roles, and temporary role expiries (admin only).
type UserService struct {
	store store.Store
}

// NewUserService creates a UserService backed by the given store.
func NewUserService(s store.Store) *UserService {
	return &UserService{store: s}
}

var (
	// ErrPasswordTooShort is returned when a password is shorter than 8 chars.
	ErrPasswordTooShort = errors.New("password must be at least 8 characters")
	// ErrLastAdmin is returned when the last admin would be removed.
	ErrLastAdmin = errors.New("cannot remove the last admin")
	// ErrInvalidRoleSet is returned for an invalid role assignment.
	ErrInvalidRoleSet = errors.New("invalid role set")
)

// validRoles is the full set of assignable roles.
var validRoles = map[string]bool{
	"student":   true,
	"mentor":    true,
	"lecturer":  true,
	"admin":     true,
}

// staffRoles are roles that are not student.
var staffRoles = map[string]bool{
	"mentor":    true,
	"lecturer":  true,
	"admin":     true,
}

// ListUsers returns all users filtered by search/role/status, with effective roles.
func (s *UserService) ListUsers(ctx context.Context, search, role, status string) ([]store.User, error) {
	return s.store.GetUsers(ctx, search, role, status)
}

// CreateUser creates an account with an admin-chosen password.
func (s *UserService) CreateUser(ctx context.Context, email, name, password string, roles []string) (int64, []string, error) {
	if len(password) < 8 {
		return 0, nil, ErrPasswordTooShort
	}
	if err := validateRoles(roles); err != nil {
		return 0, nil, err
	}

	hash, err := auth.Hash(password)
	if err != nil {
		return 0, nil, err
	}

	id, err := s.store.CreateUser(ctx, email, name, roles, hash)
	if err != nil {
		if errors.Is(err, store.ErrEmailTaken) {
			return 0, nil, store.ErrEmailTaken
		}
		return 0, nil, err
	}
	return id, roles, nil
}

// UpdateUser updates name, email, and/or active status of a user.
func (s *UserService) UpdateUser(ctx context.Context, id int64, name, email string, active *bool) (*store.User, error) {
	if active != nil {
		if _, err := s.store.SetUserLeftAt(ctx, id, !*active); err != nil {
			return nil, err
		}
	}
	if name != "" || email != "" {
		return s.store.UpdateUser(ctx, id, name, email)
	}
	return s.store.GetUserByID(ctx, id)
}

// DeleteUser soft-deletes a user (deleted_at). Refuses the last admin.
func (s *UserService) DeleteUser(ctx context.Context, id int64) error {
	// Refuse deleting the last admin.
	admins, err := s.store.CountAdmins(ctx)
	if err != nil {
		return err
	}
	user, err := s.store.GetUserByID(ctx, id)
	if err != nil {
		return err
	}
	isAdmin := false
	for _, r := range user.Roles {
		if r == "admin" {
			isAdmin = true
			break
		}
	}
	if isAdmin && admins <= 1 {
		return ErrLastAdmin
	}
	_, err = s.store.SoftDeleteUser(ctx, id)
	return err
}

// ReplaceUserRoles replaces a user's whole role set with optional end dates.
func (s *UserService) ReplaceUserRoles(ctx context.Context, userID int64, roles []string, expiries map[string]time.Time) error {
	if err := validateRoles(roles); err != nil {
		return err
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	// A student cannot also be staff.
	hasStudent := false
	hasStaff := false
	for _, r := range roles {
		if r == "student" {
			hasStudent = true
		} else if staffRoles[r] {
			hasStaff = true
		}
	}
	if hasStudent && hasStaff {
		return ErrInvalidRoleSet
	}

	// Staff keep at least one staff role: a current staff member cannot be
	// demoted to student-only.
	currentlyStaff := false
	for _, r := range user.Roles {
		if staffRoles[r] {
			currentlyStaff = true
			break
		}
	}
	if currentlyStaff && !hasStaff {
		return ErrInvalidRoleSet
	}

	// Expiry keys must be in roles; a past date is invalid.
	now := time.Now()
	for role, exp := range expiries {
		if !validRoles[role] {
			return ErrInvalidRoleSet
		}
		if !exp.IsZero() && !exp.After(now) {
			return ErrInvalidRoleSet
		}
	}

	return s.store.ReplaceUserRoles(ctx, userID, roles, expiries)
}

// validRoleSet reports that roles is a non-empty subset of the valid roles.
func validRoleSet(roles []string) bool {
	if len(roles) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, r := range roles {
		if !validRoles[r] || seen[r] {
			return false
		}
		seen[r] = true
	}
	return true
}

// validateRoles checks that roles is well-formed and that a user is not both
// student and staff.
func validateRoles(roles []string) error {
	if !validRoleSet(roles) {
		return ErrInvalidRoleSet
	}
	hasStudent := false
	hasStaff := false
	for _, r := range roles {
		if r == "student" {
			hasStudent = true
		} else if staffRoles[r] {
			hasStaff = true
		}
	}
	if hasStudent && hasStaff {
		return ErrInvalidRoleSet
	}
	return nil
}
