package handlers

import (
	"encoding/json"
	"net/http"
)

// ErrorDetail describes a single field-level validation error.
type ErrorDetail struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ErrorResponse is the standard error body returned by every endpoint.
type ErrorResponse struct {
	Error   string        `json:"error"`
	Message string        `json:"message"`
	Status  int           `json:"status"`
	Details []ErrorDetail `json:"details,omitempty"`
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a standard error JSON response.
func writeError(w http.ResponseWriter, status int, code, message string, details []ErrorDetail) {
	writeJSON(w, status, ErrorResponse{
		Error:   code,
		Message: message,
		Status:  status,
		Details: details,
	})
}
