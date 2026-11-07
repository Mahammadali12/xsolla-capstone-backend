package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// FileService is the interface that file handlers depend on.
type FileService interface {
	CreateFile(ctx context.Context, uploadedBy int64, originalName, storageKey, mimeType string, sizeBytes int64, reader io.Reader) (*store.File, error)
	GetFileByID(ctx context.Context, id int64) (*store.File, error)
	CanReadFile(ctx context.Context, userID int64, roles []string, fileID int64) (*store.File, error)
	DeleteFile(ctx context.Context, userID int64, roles []string, id int64) error
}

type FileHandlers struct {
	svc FileService
}

// NewFileHandlers creates file handlers backed by the given service.
func NewFileHandlers(svc FileService) *FileHandlers {
	return &FileHandlers{svc: svc}
}

// @Summary     Upload file
// @Description Upload a file (pdf, pptx, md, zip, images) and store it on disk
// @Tags        files
// @Accept      multipart/form-data
// @Produce     json
// @Security    BearerAuth
// @Param       file formData file true "File to upload"
// @Success     201 {object} dto.FileResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     401 {object} handlers.ErrorResponse
// @Failure     413 {object} handlers.ErrorResponse
// @Failure     415 {object} handlers.ErrorResponse
// @Failure     500 {object} handlers.ErrorResponse
// @Router      /files [post]
func (h *FileHandlers) UploadFile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	log.Printf("file handler: UploadFile called by user %d", userID)

	if err := r.ParseMultipartForm(50 << 20); err != nil { // 50 MB max
		log.Printf("file handler: ParseMultipartForm error: %v", err)
		writeError(w, 400, "bad_request", "file too large or invalid form", nil)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		log.Printf("file handler: FormFile error: %v", err)
		writeError(w, 400, "bad_request", "no file provided", nil)
		return
	}
	defer file.Close()

	// Validate file type
	ext := filepath.Ext(header.Filename)
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	allowedTypes := map[string]bool{
		"application/pdf": true,
		"application/msword": true,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
		"application/vnd.ms-powerpoint": true,
		"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
		"text/markdown": true,
		"text/plain": true,
		"application/zip": true,
		"image/png": true,
		"image/jpeg": true,
		"image/gif": true,
		"image/webp": true,
	}
	if !allowedTypes[mimeType] {
		log.Printf("file handler: unsupported mime type: %s", mimeType)
		writeError(w, 415, "unsupported_type", "file type not allowed", nil)
		return
	}

	// Generate unique storage key
	randBytes := make([]byte, 16)
	if _, err := rand.Read(randBytes); err != nil {
		log.Printf("file handler: rand error: %v", err)
		writeError(w, 500, "server_error", "could not generate storage key", nil)
		return
	}
	storageKey := filepath.Join("users", fmt.Sprintf("%d", userID), base64.RawURLEncoding.EncodeToString(randBytes)+ext)

	f, err := h.svc.CreateFile(r.Context(), userID, header.Filename, storageKey, mimeType, header.Size, file)
	if err != nil {
		log.Printf("file handler: CreateFile error: %v", err)
		writeError(w, 500, "server_error", "could not save file", nil)
		return
	}

	writeJSON(w, 201, dto.FileResponse{
		ID:           f.ID,
		OriginalName: f.OriginalName,
		StorageKey:   f.StorageKey,
		MimeType:     f.MimeType,
		SizeBytes:    f.SizeBytes,
		CreatedAt:    f.CreatedAt,
	})
}

// GET /files/:id
func (h *FileHandlers) GetFile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	fileID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid file ID", nil)
		return
	}

	f, err := h.svc.CanReadFile(r.Context(), userID, roles, fileID)
	if err != nil {
		if errors.Is(err, store.ErrFileNotFound) {
			writeError(w, 404, "not_found", "file not found", nil)
		} else {
			writeError(w, 403, "forbidden", "cannot access this file", nil)
		}
		return
	}

	writeJSON(w, 200, dto.FileResponse{
		ID:           f.ID,
		OriginalName: f.OriginalName,
		StorageKey:   f.StorageKey,
		MimeType:     f.MimeType,
		SizeBytes:    f.SizeBytes,
		CreatedAt:    f.CreatedAt,
	})
}

// GET /files/:id/download
func (h *FileHandlers) DownloadFile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	fileID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid file ID", nil)
		return
	}

	f, err := h.svc.CanReadFile(r.Context(), userID, roles, fileID)
	if err != nil {
		if errors.Is(err, store.ErrFileNotFound) {
			writeError(w, 404, "not_found", "file not found", nil)
		} else {
			writeError(w, 403, "forbidden", "cannot download this file", nil)
		}
		return
	}

	path := filepath.Join("uploads", f.StorageKey)
	file, err := os.Open(path)
	if err != nil {
		writeError(w, 404, "not_found", "file not found on disk", nil)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.OriginalName))
	w.Header().Set("Content-Type", f.MimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", f.SizeBytes))
	http.ServeContent(w, r, f.OriginalName, f.CreatedAt, file)
}

// DELETE /files/:id
func (h *FileHandlers) DeleteFile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	fileID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid file ID", nil)
		return
	}

	err = h.svc.DeleteFile(r.Context(), userID, roles, fileID)
	if err != nil {
		if errors.Is(err, store.ErrFileNotFound) {
			writeError(w, 404, "not_found", "file not found", nil)
		} else if errors.Is(err, service.ErrFileReferenced) {
			writeError(w, 409, "conflict", "file is still referenced by a content item or submission", nil)
		} else if strings.Contains(err.Error(), "forbidden") {
			writeError(w, 403, "forbidden", "cannot delete this file", nil)
		} else {
			writeError(w, 500, "server_error", "could not delete file", nil)
		}
		return
	}

	w.WriteHeader(204)
}
