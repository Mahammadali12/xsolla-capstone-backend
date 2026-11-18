package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	neturl "net/url"
	"strings"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/store"
)

// ContentService coordinates content item and assignment operations.
type ContentService struct {
	store store.Store
}

// ErrUnsupportedContentType is returned when an unsupported content item type is used.
var ErrUnsupportedContentType = errors.New("unsupported content item type")


// ErrInvalidContentItem is returned for a content item that breaks a rule; the message says which.
var ErrInvalidContentItem = errors.New("invalid content item")

func invalidContent(msg string) error { return fmt.Errorf("%w: %s", ErrInvalidContentItem, msg) }

// validLinkURL accepts only http and https addresses (a javascript: link would run in the reader's browser).
func validLinkURL(raw string) bool {
	u, err := neturl.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// canWriteModule: admins always; anyone else must be the week's creator or an assigned lecturer.
func (s *ContentService) canWriteModule(ctx context.Context, userID int64, roles []string, moduleID int64) error {
	if auth.HasRole(roles, "admin") {
		return nil
	}
	teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, userID)
	if err != nil {
		return err
	}
	if !teacher {
		return store.ErrNotModuleTeacher
	}
	return nil
}

// checkOwnFile: a content item may only show a file its author uploaded (admins may use any file).
func (s *ContentService) checkOwnFile(ctx context.Context, userID int64, roles []string, fileID *int64) error {
	if fileID == nil || auth.HasRole(roles, "admin") {
		return nil
	}
	f, err := s.store.GetFileByID(ctx, *fileID)
	if errors.Is(err, store.ErrFileNotFound) {
		return invalidContent("file not found")
	}
	if err != nil {
		return err
	}
	if f.UploadedBy != userID {
		return invalidContent("you can only attach files you uploaded")
	}
	return nil
}

// materialMimeTypes are the files a lecturer can add as a material: PDF, PowerPoint (.pptx), Markdown and ZIP. The
// upload form offers the same.
var materialMimeTypes = map[string]bool{
	"application/pdf": true,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
	"text/markdown":   true,
	"application/zip": true,
}

// checkMaterialFile refuses a material file that is not one of materialMimeTypes.
func (s *ContentService) checkMaterialFile(ctx context.Context, fileID *int64) error {
	if fileID == nil {
		return nil
	}
	f, err := s.store.GetFileByID(ctx, *fileID)
	if errors.Is(err, store.ErrFileNotFound) {
		return invalidContent("file not found")
	}
	if err != nil {
		return err
	}
	if !materialMimeTypes[f.MimeType] {
		return invalidContent("a material must be a PDF, PPTX, Markdown (.md) or ZIP file")
	}
	return nil
}

// withFileInfo adds each file material's file name and type.
func (s *ContentService) withFileInfo(ctx context.Context, items []store.ContentItem) error {
	for i := range items {
		if items[i].Type != "file" || items[i].FileID == nil {
			continue
		}
		f, err := s.store.GetFileByID(ctx, *items[i].FileID)
		if errors.Is(err, store.ErrFileNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		name, mime := f.OriginalName, f.MimeType
		items[i].FileName, items[i].FileMimeType = &name, &mime
	}
	return nil
}

// withOneFileInfo is withFileInfo for a single item.
func (s *ContentService) withOneFileInfo(ctx context.Context, ci *store.ContentItem) (*store.ContentItem, error) {
	if ci == nil {
		return nil, nil
	}
	items := []store.ContentItem{*ci}
	if err := s.withFileInfo(ctx, items); err != nil {
		return nil, err
	}
	return &items[0], nil
}

// NewContentService creates a ContentService backed by the given store.
func NewContentService(s store.Store) *ContentService {
	return &ContentService{store: s}
}

// CreateContentItem creates a new content item.
// Only file, assignment, and link types are supported (sections are removed).
// Assignments require a non-nil due_at. Links require a non-nil url.
func (s *ContentService) CreateContentItem(ctx context.Context, userID int64, roles []string, moduleID int64, parentID *int64, itemType string, title string, description string, position int, isVisible bool, fileID *int64, dueAt *time.Time, url *string) (*store.ContentItem, error) {
	// Only lecturer or admin can create content items
	if !auth.HasRole(roles, "lecturer") && !auth.HasRole(roles, "admin") {
		return nil, store.ErrNotModuleTeacher
	}
	if err := s.canWriteModule(ctx, userID, roles, moduleID); err != nil {
		return nil, err
	}

	// Only file, assignment, and link types are supported (sections removed)
	if itemType == "section" {
		return nil, ErrUnsupportedContentType
	}

	// If parent_id is set, reject it: sections have been removed and every item is a root item.
	if parentID != nil {
		return nil, invalidContent("parent_id is not supported; sections have been removed")
	}

	// Type-specific validation
	switch itemType {
	case "assignment":
		// no deadline needed: a student's is worked out from their 1:1 (one_on_one_deadline.go)
	case "link":
		if url == nil || *url == "" {
			return nil, invalidContent("link requires a url")
		}
		if !validLinkURL(*url) {
			return nil, invalidContent("url must be an http or https address")
		}
	case "file":
		if fileID == nil {
			return nil, invalidContent("file requires a file_id")
		}
	default:
		return nil, ErrUnsupportedContentType
	}
	if err := s.checkOwnFile(ctx, userID, roles, fileID); err != nil {
		return nil, err
	}
	if itemType == "file" {
		if err := s.checkMaterialFile(ctx, fileID); err != nil {
			return nil, err
		}
	}

	ci, err := s.store.CreateContentItem(ctx, moduleID, parentID, itemType, title, description, position, isVisible, fileID, url)
	if err != nil {
		log.Printf("content service: CreateContentItem store error: %v", err)
		return nil, err
	}

	// the deadline lives in the assignments table (GET reads it from there)
	if itemType == "assignment" {
		a, err := s.store.CreateAssignment(ctx, ci.ID, dueAt)
		if err != nil {
			return nil, err
		}
		ci.DueAt = a.DueAt
	}

	return s.withOneFileInfo(ctx, ci)
}

// GetContentItemByID returns a content item if the user is authorized to view it, with its file's name and type.
func (s *ContentService) GetContentItemByID(ctx context.Context, userID int64, roles []string, itemID int64) (*store.ContentItem, error) {
	ci, err := s.getContentItemByID(ctx, userID, roles, itemID)
	if err != nil {
		return nil, err
	}
	return s.withOneFileInfo(ctx, ci)
}

func (s *ContentService) getContentItemByID(ctx context.Context, userID int64, roles []string, itemID int64) (*store.ContentItem, error) {
	ci, err := s.store.GetContentItemByID(ctx, itemID)
	if err != nil {
		return nil, err
	}

	if auth.HasRole(roles, "admin") {
		return ci, nil
	}

	if auth.HasRole(roles, "mentor") || auth.HasRole(roles, "lecturer") {
		// Mentors and lecturers can view any content item without being enrolled
		if !ci.IsVisible {
			return nil, store.ErrNotEnrolled
		}
		return ci, nil
	}

	enrolled, err := s.store.IsUserEnrolled(ctx, ci.ModuleID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		return nil, store.ErrNotEnrolled
	}

	// If item is not visible, only admin can see it
	if !ci.IsVisible && !auth.HasRole(roles, "admin") {
		return nil, store.ErrNotEnrolled
	}

	// a student's deadline follows their 1:1
	items := []store.ContentItem{*ci}
	if err := s.personalizeDeadlines(ctx, userID, items); err != nil {
		return nil, err
	}
	return &items[0], nil
}

// GetContentItemsForModule returns the content tree for a module, each file material with its file's name and type.
func (s *ContentService) GetContentItemsForModule(ctx context.Context, userID int64, roles []string, moduleID int64) ([]store.ContentItem, error) {
	items, err := s.getContentItemsForModule(ctx, userID, roles, moduleID)
	if err != nil {
		return nil, err
	}
	if err := s.withFileInfo(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *ContentService) getContentItemsForModule(ctx context.Context, userID int64, roles []string, moduleID int64) ([]store.ContentItem, error) {
	if auth.HasRole(roles, "admin") {
		return s.store.GetContentItemsForModule(ctx, moduleID, false)
	}

	if auth.HasRole(roles, "mentor") || auth.HasRole(roles, "lecturer") {
		return s.store.GetContentItemsForModule(ctx, moduleID, true)
	}

	enrolled, err := s.store.IsUserEnrolled(ctx, moduleID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotEnrolled
		}
		return s.store.GetContentItemsForModule(ctx, moduleID, true)
	}

	items, err := s.store.GetContentItemsForModule(ctx, moduleID, true)
	if err != nil {
		return nil, err
	}
	// a student's deadlines follow their 1:1
	if err := s.personalizeDeadlines(ctx, userID, items); err != nil {
		return nil, err
	}
	return items, nil
}

// UpdateContentItem updates a content item if the user is authorized.
// If the item is an assignment and due_at is provided, the assignment's
// deadline is updated too. If the item is a link and url is provided, the link
// url is updated too.
func (s *ContentService) UpdateContentItem(ctx context.Context, userID int64, roles []string, itemID int64, title *string, description *string, position *int, isVisible *bool, parentID *int64, fileID *int64, dueAt *time.Time, url *string) (*store.ContentItem, error) {
	ci, err := s.store.GetContentItemByID(ctx, itemID)
	if err != nil {
		return nil, err
	}

	// only the week's current teachers (and admins): a lecturer taken off the week loses its content, even what they
	// wrote themselves
	if err := s.canWriteModule(ctx, userID, roles, ci.ModuleID); err != nil {
		return nil, err
	}
	if parentID != nil {
		return nil, invalidContent("parent_id is not supported; sections have been removed")
	}
	if url != nil && *url != "" && !validLinkURL(*url) {
		return nil, invalidContent("url must be an http or https address")
	}
	if err := s.checkOwnFile(ctx, userID, roles, fileID); err != nil {
		return nil, err
	}

	if ci.Type == "assignment" && dueAt != nil {
		if _, err := s.store.UpdateAssignment(ctx, itemID, dueAt); err != nil {
			return nil, err
		}
	}

	if ci.Type == "file" {
		if err := s.checkMaterialFile(ctx, fileID); err != nil {
			return nil, err
		}
	}

	updated, err := s.store.UpdateContentItem(ctx, itemID, title, description, position, isVisible, parentID, fileID, dueAt, url)
	if err != nil {
		return nil, err
	}
	return s.withOneFileInfo(ctx, updated)
}

// DeleteContentItem soft-deletes a content item if the user is authorized.
func (s *ContentService) DeleteContentItem(ctx context.Context, userID int64, roles []string, itemID int64) error {
	ci, err := s.store.GetContentItemByID(ctx, itemID)
	if err != nil {
		return err
	}

	if err := s.canWriteModule(ctx, userID, roles, ci.ModuleID); err != nil {
		return err
	}

	return s.store.DeleteContentItem(ctx, itemID)
}

// GetAssignmentByContentItemID returns the assignment for a content item.
func (s *ContentService) GetAssignmentByContentItemID(ctx context.Context, userID int64, roles []string, contentItemID int64) (*store.Assignment, error) {
	ci, err := s.store.GetContentItemByID(ctx, contentItemID)
	if err != nil {
		return nil, err
	}

	if !auth.HasRole(roles, "admin") && !auth.HasRole(roles, "lecturer") {
		enrolled, err := s.store.IsUserEnrolled(ctx, ci.ModuleID, userID)
		if err != nil {
			return nil, err
		}
		if !enrolled {
			teacher, err := s.store.IsUserTeacherOfModule(ctx, ci.ModuleID, userID)
			if err != nil {
				return nil, err
			}
			if !teacher {
				return nil, store.ErrNotEnrolled
			}
		}
	}

	return s.store.GetAssignmentByContentItemID(ctx, contentItemID)
}

// UpdateAssignment updates an assignment if the user is authorized.
func (s *ContentService) UpdateAssignment(ctx context.Context, userID int64, roles []string, contentItemID int64, dueAt *time.Time) (*store.Assignment, error) {
	ci, err := s.store.GetContentItemByID(ctx, contentItemID)
	if err != nil {
		return nil, err
	}

	if !auth.HasRole(roles, "admin") {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, ci.ModuleID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotModuleTeacher
		}
	}

	return s.store.UpdateAssignment(ctx, contentItemID, dueAt)
}
