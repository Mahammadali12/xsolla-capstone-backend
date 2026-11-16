package dto

import "time"

// AnalyticsSummaryRequest is the body of POST /analytics/students/{id}/ai-summary and the mentor equivalent.
// Dates are optional YYYY-MM-DD school dates, inclusive like the rest of the analytics routes.
type AnalyticsSummaryRequest struct {
	From         string `json:"from"`
	To           string `json:"to"`
	ForceRefresh bool   `json:"force_refresh"`
}

// AnalyticsSummaryResponse is the AI summary for one student or mentor. GeneratedAt is null when there was not
// enough feedback to generate anything.
type AnalyticsSummaryResponse struct {
	Summary     string                  `json:"summary"`
	SourceCount int                     `json:"source_count"`
	GeneratedAt *time.Time              `json:"generated_at"`
	Cached      bool                    `json:"cached"`
	Period      AnalyticsPeriodResponse `json:"period"`
}
