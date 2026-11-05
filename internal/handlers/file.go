package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
)

type FileHandlers struct {
	svc *service.FileService
}

// NewFileHandlers creates file handlers.
func NewFileHandlers(svc *service.FileService) *FileHandlers {
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

	// Ensure directory exists
	storageDir := filepath.Dir(storageKey)
	if err := os.MkdirAll(filepath.Join("uploads", storageDir), 0755); err != nil {
		log.Printf("file handler: MkdirAll error: %v", err)
		writeError(w, 500, "server_error", "could not create storage directory", nil)
		return
	}

	// Save file to disk
	outFile, err := os.Create(filepath.Join("uploads", storageKey))
	if err != nil {
		log.Printf("file handler: Create error: %v", err)
		writeError(w, 500, "server_error", "could not save file", nil)
		return
	}
	defer outFile.Close()

	fileSize, err := io.Copy(outFile, file)
	if err != nil {
		log.Printf("file handler: copy error: %v", err)
		writeError(w, 500, "server_error", "could not write file", nil)
		return
	}

	f, err := h.svc.CreateFile(r.Context(), userID, header.Filename, storageKey, mimeType, fileSize)
	if err != nil {
		log.Printf("file handler: CreateFile error: %v", err)
		writeError(w, 500, "server_error", "could not save file metadata", nil)
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
