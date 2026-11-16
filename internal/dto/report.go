package dto

import "time"

// SendReportRequest is the body of POST /reports.
type SendReportRequest struct {
	Subject string `json:"subject"` // 1 to 200 characters
	Message string `json:"message"` // 1 to 5000 characters
}

// SentReportResponse is what the sender gets back: their own report, without anything the admins add later.
type SentReportResponse struct {
	ID        int64     `json:"id"`
	Subject   string    `json:"subject"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// UpdateReportRequest is the body of PATCH /reports/{id}. Leave a field out to keep it.
type UpdateReportRequest struct {
	Status    *string `json:"status,omitempty"`     // open or resolved
	AdminNote *string `json:"admin_note,omitempty"` // up to 2000 characters; "" removes the note
}

// ReportPerson is someone named on a report.
type ReportPerson struct {
	ID    int64    `json:"id"`
	Name  string   `json:"name"`
	Email string   `json:"email"`
	Roles []string `json:"roles,omitempty"`
}

// ReportResponse is a report as the admins see it.
type ReportResponse struct {
	ID         int64         `json:"id"`
	Subject    string        `json:"subject"`
	Message    string        `json:"message"`
	Status     string        `json:"status"`   // open or resolved
	Reporter   *ReportPerson `json:"reporter"` // null once the reporter's account is deleted
	AdminNote  *string       `json:"admin_note"`
	ResolvedBy *ReportPerson `json:"resolved_by"`
	ResolvedAt *time.Time    `json:"resolved_at"`
	CreatedAt  time.Time     `json:"created_at"`
}

// ReportCountsResponse is the answer of GET /reports/summary.
type ReportCountsResponse struct {
	Open     int `json:"open"`
	Resolved int `json:"resolved"`
	Total    int `json:"total"`
}
