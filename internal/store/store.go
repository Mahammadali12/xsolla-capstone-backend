package store

import (
	"context"
	"database/sql"
	"time"
)

// Store is the interface that all services depend on for database access.
// Concrete implementation is *StoreImpl in this package.
type Store interface {
	// User methods
	CreateUser(ctx context.Context, email string, name string, roles []string, passwordHash string) (int64, error)
	GetUserByEmail(ctx context.Context, email string) (int64, string, []string, error)
	GetUserCount(ctx context.Context) (int, error)
	GetUserByID(ctx context.Context, id int64) (int64, string, string, []string, time.Time, error)

	// Refresh token methods
	SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	DeleteRefreshTokensForUser(ctx context.Context, userID int64) error
	FindRefreshTokenByHash(ctx context.Context, tokenHash string) (int64, string, time.Time, error)
	DeleteRefreshToken(ctx context.Context, tokenHash string) error
	CountRefreshTokens(ctx context.Context) *sql.Row

	// Module methods
	CreateModule(ctx context.Context, title string, description string, createdBy int64) (*Module, error)
	GetModuleByID(ctx context.Context, id int64) (*Module, error)
	GetModulesForUser(ctx context.Context, userID int64, roles []string) ([]Module, error)
	PatchModule(ctx context.Context, id int64, title string, description string, createdBy int64, isAdmin bool) (*Module, error)
	DeleteModule(ctx context.Context, id int64, createdBy int64, isAdmin bool) error

	// Enrollment methods
	CreateEnrollment(ctx context.Context, moduleID int64, userID int64) (*Enrollment, error)
	DeleteEnrollment(ctx context.Context, moduleID int64, userID int64) error
	GetEnrollmentsForModule(ctx context.Context, moduleID int64) ([]Enrollment, error)
	IsUserEnrolled(ctx context.Context, moduleID int64, userID int64) (bool, error)
	IsUserTeacherOfModule(ctx context.Context, moduleID int64, userID int64) (bool, error)

	// Content item methods
	CreateContentItem(ctx context.Context, moduleID int64, parentID *int64, itemType string, title string, description string, position int, isVisible bool, fileID *int64, url *string) (*ContentItem, error)
	GetContentItemByID(ctx context.Context, id int64) (*ContentItem, error)
	GetContentItemsForModule(ctx context.Context, moduleID int64, visibleOnly bool) ([]ContentItem, error)
	UpdateContentItem(ctx context.Context, id int64, title string, description string, position *int, isVisible *bool, parentID *int64, fileID *int64, dueAt *time.Time, url *string) (*ContentItem, error)
	DeleteContentItem(ctx context.Context, id int64) error

	// Assignment methods
	CreateAssignment(ctx context.Context, contentItemID int64, dueAt *time.Time, maxPoints int) (*Assignment, error)
	GetAssignmentByContentItemID(ctx context.Context, contentItemID int64) (*Assignment, error)
	UpdateAssignment(ctx context.Context, contentItemID int64, dueAt *time.Time, maxPoints int) (*Assignment, error)
	DeleteAssignment(ctx context.Context, contentItemID int64) error

	// File methods
	CreateFile(ctx context.Context, uploadedBy int64, originalName, storageKey, mimeType string, sizeBytes int64) (*File, error)
	GetFileByID(ctx context.Context, id int64) (*File, error)
	GetFilesForUser(ctx context.Context, uploadedBy int64) ([]File, error)
	DeleteFile(ctx context.Context, id int64) error
	GetContentItemForFile(ctx context.Context, fileID int64) (*ContentItemRef, error)
	GetSubmissionForFile(ctx context.Context, fileID int64) (*SubmissionRef, error)
	FileReferencedCount(ctx context.Context, fileID int64) (int64, error)

	// Submission methods
	CreateSubmission(ctx context.Context, contentItemID int64, studentID int64, textBody string) (*Submission, error)
	GetSubmissionByID(ctx context.Context, id int64) (*Submission, error)
	GetSubmissionByStudentAndAssignment(ctx context.Context, studentID int64, contentItemID int64) (*Submission, error)
	GetSubmissionsByStudentAndAssignment(ctx context.Context, studentID int64, contentItemID int64) ([]Submission, error)
	GetSubmissionsForAssignment(ctx context.Context, contentItemID int64) ([]Submission, error)
	UpdateSubmissionGrade(ctx context.Context, id int64, grade *float64, feedback string, gradedBy int64) (*Submission, error)
	DeleteSubmission(ctx context.Context, id int64) error
	CreateSubmissionFile(ctx context.Context, submissionID int64, fileID int64) (sql.Result, error)
	DeleteSubmissionFiles(ctx context.Context, submissionID int64) (sql.Result, error)
	GetSubmissionFiles(ctx context.Context, submissionID int64) ([]File, error)

	// Event methods
	CreateEvent(ctx context.Context, title string, kind string, startsAt time.Time, endsAt time.Time, allDay bool, location *string, description *string, moduleID *int64) (*Event, error)
	GetEventByID(ctx context.Context, id int64) (*Event, error)
	ListEventsByRange(ctx context.Context, from time.Time, to time.Time, userID int64, roles []string) ([]Event, error)
	UpdateEvent(ctx context.Context, id int64, title string, kind string, startsAt time.Time, endsAt time.Time, allDay bool, location *string, description *string, moduleID *int64, userID int64, isAdmin bool) (*Event, error)
	DeleteEvent(ctx context.Context, id int64, userID int64, isAdmin bool) error
}

// Store wraps the database connection and provides access to queries.
type StoreImpl struct {
	db *sql.DB
}

// NewStore creates a Store from an existing *sql.DB.
func NewStore(db *sql.DB) *StoreImpl {
	return &StoreImpl{db: db}
}

// DB returns a Query bound to this store's connection (works in transactions too).
func (s *StoreImpl) DB() *Query {
	return &Query{DBTX: s.db}
}

// WithTx returns a Query bound to the given transaction.
func WithTx(tx *sql.Tx) *Query {
	return &Query{DBTX: tx}
}

// Compile-time check that *StoreImpl implements Store.
var _ Store = (*StoreImpl)(nil)

// CountRefreshTokens returns the count of active refresh tokens.
func (s *StoreImpl) CountRefreshTokens(ctx context.Context) *sql.Row {
	return s.DB().CountRefreshTokens(ctx)
}
