package dto

import "time"

// FileResponse is the file object returned in responses.
type FileResponse struct {
	ID           int64     `json:"id"`
	OriginalName string    `json:"original_name"`
	StorageKey   string    `json:"storage_key"`
	MimeType     string    `json:"mime_type"`
	SizeBytes    int64     `json:"size_bytes"`
	CreatedAt    time.Time `json:"created_at"`
}
