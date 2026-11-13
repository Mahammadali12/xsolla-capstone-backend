package dto

import "time"

type CreateQuizQuestionRequest struct {
	Prompt  string   `json:"prompt"`
	Options []string `json:"options"`
	Correct int      `json:"correct"`
}

type CreateQuizRequest struct {
	Title       string                      `json:"title"`
	Description string                      `json:"description,omitempty"`
	DueAt       *time.Time                  `json:"due_at,omitempty"`
	Questions   []CreateQuizQuestionRequest `json:"questions"`
}

type GenerateAIQuizRequest struct {
	ContentItemID int64      `json:"content_item_id"`
	Title         string     `json:"title,omitempty"`
	DueAt         *time.Time `json:"due_at,omitempty"`
}

type UpdateQuizRequest struct {
	Title            *string                     `json:"title,omitempty"`
	Description      *string                     `json:"description,omitempty"`
	DueAt            *time.Time                  `json:"due_at,omitempty"`
	ClearDueAt       bool                        `json:"clear_due_at,omitempty"`
	Questions        []CreateQuizQuestionRequest `json:"questions,omitempty"`
	ReplaceQuestions bool                        `json:"replace_questions,omitempty"`
}

type SubmitQuizAttemptRequest struct {
	Answers []int `json:"answers"`
}

type QuizQuestionResponse struct {
	ID       int64    `json:"id"`
	Position int      `json:"position"`
	Prompt   string   `json:"prompt"`
	Options  []string `json:"options"`
	Correct  *int     `json:"correct,omitempty"`
}

type QuizAttemptResponse struct {
	ID          int64     `json:"id"`
	QuizID      int64     `json:"quiz_id"`
	StudentID   int64     `json:"student_id"`
	Answers     []int     `json:"answers"`
	Score       int       `json:"score"`
	Total       int       `json:"total"`
	SubmittedAt time.Time `json:"submitted_at"`
}

type QuizResponse struct {
	ID                  int64                  `json:"id"`
	ModuleID            int64                  `json:"module_id"`
	SourceContentItemID *int64                 `json:"source_content_item_id,omitempty"`
	Title               string                 `json:"title"`
	Description         string                 `json:"description,omitempty"`
	DueAt               *time.Time             `json:"due_at,omitempty"`
	Status              string                 `json:"status"`
	ErrorMessage        string                 `json:"error_message,omitempty"`
	IsAIGenerated       bool                   `json:"is_ai_generated"`
	CreatedBy           int64                  `json:"created_by"`
	CreatedAt           time.Time              `json:"created_at"`
	Questions           []QuizQuestionResponse `json:"questions"`
	Attempt             *QuizAttemptResponse   `json:"attempt,omitempty"`
}
