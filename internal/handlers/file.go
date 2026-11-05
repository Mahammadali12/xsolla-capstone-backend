package handlers

import (
	"log"
	"net/http"
)

type FileHandlers struct{}

// NewFileHandlers creates file handlers.
func NewFileHandlers() *FileHandlers {
	return &FileHandlers{}
}

// POST /files
func (h *FileHandlers) UploadFile(w http.ResponseWriter, r *http.Request) {
	log.Printf("file handler: UploadFile called")

	// TODO: implement file upload handling
	// - parse multipart form
	// - validate file type/size
	// - store file on disk
	// - save metadata to DB
	// - return JSON response

	writeJSON(w, 200, map[string]string{
		"message": "file upload endpoint not yet implemented",
	})
}
