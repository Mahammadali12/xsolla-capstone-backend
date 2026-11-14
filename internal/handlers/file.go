package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/storage"
	"xsolla-capstone-api/internal/store"
)

// FileService is the interface that file handlers depend on.
type FileService interface {
	CreateFile(ctx context.Context, uploadedBy int64, originalName, storageKey, mimeType string, sizeBytes int64, reader io.Reader) (*store.File, error)
	GetFileByID(ctx context.Context, id int64) (*store.File, error)
	CanReadFile(ctx context.Context, userID int64, roles []string, fileID int64) (*store.File, error)
	DeleteFile(ctx context.Context, userID int64, roles []string, id int64) error
	DownloadFile(ctx context.Context, storageKey string) (io.ReadCloser, error)
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

	// 50 MB is the real limit: ParseMultipartForm's number only says how much is kept in memory
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		log.Printf("file handler: ParseMultipartForm error: %v", err)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, 413, "payload_too_large", "the file is larger than 50 MB", nil)
			return
		}
		writeError(w, 400, "bad_request", "file too large or invalid form", nil)
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		log.Printf("file handler: FormFile error: %v", err)
		writeError(w, 400, "bad_request", "no file provided", nil)
		return
	}
	defer file.Close()

	// Validate file type. The fixed list goes first, so .md, .zip, .pptx... work whatever the server's OS knows.
	ext := filepath.Ext(header.Filename)
	mimeType := storage.GetContentTypeFromExtension(header.Filename)
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = mime.TypeByExtension(ext)
	}
	if i := strings.IndexByte(mimeType, ';'); i >= 0 { // "text/plain; charset=utf-8" -> "text/plain"
		mimeType = strings.TrimSpace(mimeType[:i])
	}
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

	// A program renamed to .pdf (or any allowed name) is still a program
	if isExecutable(file) {
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
	// object keys always use "/": filepath.Join would put "\" in the key on Windows, which S3 rejects
	storageKey := path.Join("users", fmt.Sprintf("%d", userID), base64.RawURLEncoding.EncodeToString(randBytes)+ext)

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

	rc, err := h.svc.DownloadFile(r.Context(), f.StorageKey)
	if err != nil {
		log.Printf("file handler: download error: %v", err)
		writeError(w, 500, "server_error", "could not download file", nil)
		return
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		log.Printf("file handler: read download error: %v", err)
		writeError(w, 500, "server_error", "could not read file", nil)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.OriginalName))
	w.Header().Set("Content-Type", f.MimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	http.ServeContent(w, r, f.OriginalName, f.CreatedAt, bytes.NewReader(data))
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

// maxUploadBytes is the largest file POST /files accepts (the front-end allows 20 MB).
const maxUploadBytes = 50 << 20

// isExecutable reports Windows (MZ), Linux (ELF) and macOS (Mach-O) programs by their first bytes, then rewinds.
func isExecutable(f multipart.File) bool {
	head := make([]byte, 4)
	n, _ := io.ReadFull(f, head)
	_, _ = f.Seek(0, io.SeekStart)
	if n < 2 {
		return false
	}
	switch {
	case head[0] == 'M' && head[1] == 'Z':
		return true
	case n == 4 && string(head) == "ELF":
		return true
	case n == 4 && (string(head) == "þíúÎ" || string(head) == "þíúÏ" ||
		string(head) == "Îúíþ" || string(head) == "Ïúíþ"):
		return true
	}
	return false
}
