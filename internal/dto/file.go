package dto

import (
	"time"

	"xsolla-capstone-api/internal/store"
)

// FileResponse is the file object returned in responses.
type FileResponse struct {
	ID           int64     `json:"id"`
	OriginalName string    `json:"original_name"`
	StorageKey   string    `json:"storage_key"`
	MimeType     string    `json:"mime_type"`
	SizeBytes    int64     `json:"size_bytes"`
	CreatedAt    time.Time `json:"created_at"`
}

// FileList converts a slice of store.Files to a slice of dto.FileResponse.
func FileList(files []store.File) []FileResponse {
	result := make([]FileResponse, len(files))
	for i, f := range files {
		result[i] = FileResponse{
			ID:           f.ID,
			OriginalName: f.OriginalName,
			StorageKey:   f.StorageKey,
			MimeType:     f.MimeType,
			SizeBytes:    f.SizeBytes,
			CreatedAt:    f.CreatedAt,
		}
	}
	return result
}
