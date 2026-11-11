package service

import (
	"context"
	"errors"
	"log"
	"strings"
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
	// ErrPasswordTooShort is returned when a password is shorter than 8 characters.
	ErrPasswordTooShort = errors.New("password must be at least 8 characters")
	// ErrLastAdmin is returned when the last admin would be removed.
	ErrLastAdmin = errors.New("cannot remove the last admin")
	// ErrInvalidRoleSet is returned for an invalid role assignment.
	ErrInvalidRoleSet = errors.New("invalid role set")
)

// weakPasswordParts lists common/easy-to-guess password fragments (mirrors
// frontend src/utils/credentials.ts).
var weakPasswordParts = []string{
	"password", "passw0rd", "qwerty", "asdfgh", "zxcvbn", "qazwsx", "letmein",
	"welcome", "iloveyou", "monkey", "dragon", "admin", "login", "secret",
	"xsolla", "school", "student", "mentor", "lecturer", "baku",
	"123456", "234567", "345678", "456789", "987654", "654321", "abcdef",
	"bcdefg", "111111", "000000", "121212", "112233",
}

// minPasswordLength / maxPasswordLength mirror the frontend's rules.
const (
	minPasswordLength = 10
	maxPasswordLength = 72
)

// validateNewPassword checks the candidate against the same rules the
// frontend enforces (length, lower/upper/digit, no spaces, not common,
// not personal to name or email).
func validateNewPassword(password string, name, email string) error {
	if len(password) < minPasswordLength || len(password) > maxPasswordLength {
		return errors.New("use 10 to 72 characters")
	}
	lower := strings.ToLower(password)
	if !hasLower(lower) {
		return errors.New("add a lowercase letter")
	}
	if !hasUpper(password) {
		return errors.New("add an uppercase letter")
	}
	if !hasDigit(password) {
		return errors.New("add a number")
	}
	if strings.ContainsAny(password, " \t\n\r") {
		return errors.New("leave out the spaces")
	}
	for _, weak := range weakPasswordParts {
		if strings.Contains(lower, weak) {
			return errors.New("this password is too easy to guess")
		}
	}
	// four of the same character in a row
	for i := 0; i+3 < len(password); i++ {
		if password[i] == password[i+1] && password[i] == password[i+2] && password[i] == password[i+3] {
			return errors.New("this password is too easy to guess")
		}
	}
	// only 2-3 distinct characters
	distinct := map[rune]bool{}
	for _, r := range lower {
		distinct[r] = true
	}
	if len(distinct) <= 3 {
		return errors.New("this password is too easy to guess")
	}
	// not personal: must not contain name or email local-part fragments (3+ letters)
	for _, part := range personalParts(name, email) {
		if strings.Contains(lower, part) {
			return errors.New("the password must not contain your name or e-mail")
		}
	}
	return nil
}

func hasLower(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return true
		}
	}
	return false
}

func hasUpper(s string) bool {
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

func hasDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

// personalParts returns name/email fragments (3+ letters) to screen out of passwords.
func personalParts(name, email string) []string {
	parts := []string{}
	for _, p := range strings.FieldsFunc(name, func(r rune) bool {
		return r == ' ' || r == '\'' || r == '.' || r == '-'
	}) {
		p = strings.ToLower(strings.TrimSpace(p))
		if len(p) >= 3 {
			parts = append(parts, p)
		}
	}
	local := strings.ToLower(strings.Split(email, "@")[0])
	for _, sep := range []string{".", "_", "+", "-"} {
		local = strings.ReplaceAll(local, sep, " ")
	}
	for _, p := range strings.Fields(local) {
		p = strings.ToLower(strings.TrimSpace(p))
		if len(p) >= 3 {
			parts = append(parts, p)
		}
	}
	// also the whole local part
	if len(local) >= 3 {
		parts = append(parts, local)
	}
	// dedupe
	seen := map[string]bool{}
	out := []string{}
	for _, p := range parts {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// UpdatePassword sets a new password hash for the user and invalidates all
// their refresh tokens (ends sessions on every device). Admin only.
func (s *UserService) UpdatePassword(ctx context.Context, id int64, password string) error {
	user, err := s.store.GetUserByID(ctx, id)
	if err != nil {
		return err
	}
	if err := validateNewPassword(password, user.Name, user.Email); err != nil {
		return err
	}
	hash, err := auth.Hash(password)
	if err != nil {
		return err
	}
	if _, err := s.store.UpdateUserPassword(ctx, id, hash); err != nil {
		return err
	}
	return s.store.DeleteRefreshTokensForUser(ctx, id)
}

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

	// Auto-enroll new students in every existing module, mirroring the
	// auto-enrollment that happens when a module is created (CreateModule).
	// Enrollment is best-effort: the account is already created, so enrollment
	// failures are logged rather than failing user creation.
	if hasRole(roles, "student") {
		if err := s.enrollInAllModules(ctx, id); err != nil {
			log.Printf("user service: auto-enroll user %d into modules: %v", id, err)
		}
	}

	return id, roles, nil
}

// hasRole reports whether roles contains role.
func hasRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

// enrollInAllModules enrolls the user in every non-deleted module (idempotent).
func (s *UserService) enrollInAllModules(ctx context.Context, userID int64) error {
	moduleIDs, err := s.store.GetAllModuleIDs(ctx)
	if err != nil {
		return err
	}
	for _, moduleID := range moduleIDs {
		if _, err := s.store.CreateEnrollment(ctx, moduleID, userID); err != nil && !errors.Is(err, store.ErrAlreadyEnrolled) {
			return err
		}
	}
	return nil
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
