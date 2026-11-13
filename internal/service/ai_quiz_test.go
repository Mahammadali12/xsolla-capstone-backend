package service

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/store"
)

func TestParseGeneratedQuizJSON(t *testing.T) {
	var qs string
	for i := 0; i < 10; i++ {
		if i > 0 {
			qs += ","
		}
		qs += fmt.Sprintf(`{"prompt":"Question %d?","options":["A","B","C","D"],"correct":%d}`, i+1, i%4)
	}
	raw := "```json\n{\"title\":\"Go Basics\",\"description\":\"Test quiz\",\"questions\":[" + qs + "]}\n```"

	parsed, err := ParseGeneratedQuizJSON(raw)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if parsed.Title != "Go Basics" {
		t.Fatalf("unexpected title: %q", parsed.Title)
	}
	if len(parsed.Questions) != 10 {
		t.Fatalf("expected 10 questions, got %d", len(parsed.Questions))
	}
	if parsed.Questions[9].Correct != 1 {
		t.Fatalf("expected correct=1, got %d", parsed.Questions[9].Correct)
	}

	shortRaw := `{"title":"Too short","description":"Only 1","questions":[{"prompt":"Q1","options":["A","B","C","D"],"correct":0}]}`
	if _, err := ParseGeneratedQuizJSON(shortRaw); err == nil {
		t.Fatal("expected error for fewer than 10 questions")
	}
}

func TestExtractPPTXAndDOCXText(t *testing.T) {
	var pptxBuf bytes.Buffer
	pw := zip.NewWriter(&pptxBuf)
	f2, _ := pw.Create("ppt/slides/slide2.xml")
	_, _ = f2.Write([]byte(`<p:sld><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Concurrency with goroutines</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`))
	f1, _ := pw.Create("ppt/slides/slide1.xml")
	_, _ = f1.Write([]byte(`<p:sld><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Introduction to Go</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`))
	_ = pw.Close()

	pptxText, err := ExtractPPTXText(pptxBuf.Bytes())
	if err != nil {
		t.Fatalf("pptx extract failed: %v", err)
	}
	if !bytes.Contains([]byte(pptxText), []byte("Slide 1:\nIntroduction to Go")) || !bytes.Contains([]byte(pptxText), []byte("Slide 2:\nConcurrency with goroutines")) {
		t.Fatalf("unexpected pptx output: %q", pptxText)
	}

	var docxBuf bytes.Buffer
	dw := zip.NewWriter(&docxBuf)
	df, _ := dw.Create("word/document.xml")
	_, _ = df.Write([]byte(`<w:document><w:body><w:p><w:r><w:t>Channels connect goroutines.</w:t></w:r></w:p></w:body></w:document>`))
	_ = dw.Close()

	docxText, err := ExtractDOCXText(docxBuf.Bytes())
	if err != nil {
		t.Fatalf("docx extract failed: %v", err)
	}
	if docxText != "Channels connect goroutines." {
		t.Fatalf("unexpected docx text: %q", docxText)
	}
}

type fakeQuizGenerator struct {
	quiz *GeneratedQuiz
	err  error
}

func (f *fakeQuizGenerator) GenerateQuiz(_ context.Context, _ QuizSource) (*GeneratedQuiz, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.quiz, nil
}

type fakeQuizStore struct {
	store.Store
	quizzes      map[int64]*store.Quiz
	attempts     map[string]*store.QuizAttempt
	contentItems map[int64]*store.ContentItem
	nextID       int64
	isTeacher    bool
	isEnrolled   bool
}

func newFakeQuizStore() *fakeQuizStore {
	return &fakeQuizStore{
		quizzes:      make(map[int64]*store.Quiz),
		attempts:     make(map[string]*store.QuizAttempt),
		contentItems: make(map[int64]*store.ContentItem),
		nextID:       100,
	}
}

func (f *fakeQuizStore) IsUserTeacherOfModule(_ context.Context, _ int64, _ int64) (bool, error) {
	return f.isTeacher, nil
}

func (f *fakeQuizStore) IsUserEnrolled(_ context.Context, _ int64, _ int64) (bool, error) {
	return f.isEnrolled, nil
}

func (f *fakeQuizStore) GetContentItemByID(_ context.Context, id int64) (*store.ContentItem, error) {
	ci, ok := f.contentItems[id]
	if !ok {
		return nil, store.ErrContentItemNotFound
	}
	return ci, nil
}

func (f *fakeQuizStore) CreateQuiz(_ context.Context, moduleID int64, sourceContentItemID *int64, title, description string, dueAt *time.Time, status string, isAI bool, createdBy int64, questions []store.QuizQuestionInput) (*store.Quiz, error) {
	f.nextID++
	q := &store.Quiz{
		ID:                  f.nextID,
		ModuleID:            moduleID,
		SourceContentItemID: sourceContentItemID,
		Title:               title,
		Description:         description,
		DueAt:               dueAt,
		Status:              status,
		IsAIGenerated:       isAI,
		CreatedBy:           createdBy,
		CreatedAt:           time.Now(),
	}
	for i, qq := range questions {
		q.Questions = append(q.Questions, store.QuizQuestion{
			ID:           f.nextID*100 + int64(i+1),
			QuizID:       q.ID,
			Position:     i,
			Prompt:       qq.Prompt,
			Options:      qq.Options,
			CorrectIndex: qq.CorrectIndex,
		})
	}
	f.quizzes[q.ID] = q
	return q, nil
}

func (f *fakeQuizStore) GetQuizByID(_ context.Context, id int64) (*store.Quiz, error) {
	q, ok := f.quizzes[id]
	if !ok || q.DeletedAt != nil {
		return nil, store.ErrQuizNotFound
	}
	return q, nil
}

func (f *fakeQuizStore) ListQuizzesForModule(_ context.Context, moduleID int64, readyOnly bool) ([]store.Quiz, error) {
	var out []store.Quiz
	for _, q := range f.quizzes {
		if q.ModuleID != moduleID || q.DeletedAt != nil {
			continue
		}
		if readyOnly && q.Status != "ready" {
			continue
		}
		out = append(out, *q)
	}
	return out, nil
}

func (f *fakeQuizStore) CompleteAIQuiz(_ context.Context, quizID int64, title, description string, questions []store.QuizQuestionInput) error {
	q, ok := f.quizzes[quizID]
	if !ok {
		return store.ErrQuizNotFound
	}
	q.Title = title
	q.Description = description
	q.Status = "ready"
	q.ErrorMessage = ""
	q.Questions = nil
	for i, qq := range questions {
		q.Questions = append(q.Questions, store.QuizQuestion{
			ID:           quizID*100 + int64(i+1),
			QuizID:       quizID,
			Position:     i,
			Prompt:       qq.Prompt,
			Options:      qq.Options,
			CorrectIndex: qq.CorrectIndex,
		})
	}
	return nil
}

func (f *fakeQuizStore) FailAIQuiz(_ context.Context, quizID int64, errMsg string) error {
	if q, ok := f.quizzes[quizID]; ok {
		q.Status = "failed"
		q.ErrorMessage = errMsg
	}
	return nil
}

func (f *fakeQuizStore) UpsertQuizAttempt(_ context.Context, quizID, studentID int64, answers []int, score, total int) (*store.QuizAttempt, error) {
	key := fmt.Sprintf("%d:%d", quizID, studentID)
	a := &store.QuizAttempt{
		ID:          1,
		QuizID:      quizID,
		StudentID:   studentID,
		Answers:     answers,
		Score:       score,
		Total:       total,
		SubmittedAt: time.Now(),
	}
	f.attempts[key] = a
	return a, nil
}

func (f *fakeQuizStore) GetQuizAttempt(_ context.Context, quizID, studentID int64) (*store.QuizAttempt, error) {
	return f.attempts[fmt.Sprintf("%d:%d", quizID, studentID)], nil
}

func (f *fakeQuizStore) ListQuizAttemptsForStudent(_ context.Context, studentID int64, quizIDs []int64) (map[int64]store.QuizAttempt, error) {
	out := make(map[int64]store.QuizAttempt)
	for _, id := range quizIDs {
		if a, ok := f.attempts[fmt.Sprintf("%d:%d", id, studentID)]; ok {
			out[id] = *a
		}
	}
	return out, nil
}

func (f *fakeQuizStore) CountRefreshTokens(_ context.Context) *sql.Row {
	return nil
}

func TestQuizServiceRoleAccessAIAndGrading(t *testing.T) {
	st := newFakeQuizStore()
	st.isTeacher = true
	st.isEnrolled = true
	url := "https://go.dev/doc/effective_go"
	st.contentItems[10] = &store.ContentItem{
		ID:          10,
		ModuleID:    5,
		Type:        "link",
		Title:       "Effective Go",
		Description: "Go best practices",
		Url:         &url,
	}

	var genQuestions []GeneratedQuestion
	for i := 0; i < 10; i++ {
		genQuestions = append(genQuestions, GeneratedQuestion{
			Prompt:  fmt.Sprintf("Go concept %d?", i+1),
			Options: []string{"Opt A", "Opt B", "Opt C", "Opt D"},
			Correct: 1,
		})
	}
	svc := NewQuizService(st, nil, &fakeQuizGenerator{
		quiz: &GeneratedQuiz{
			Title:       "Effective Go Quiz",
			Description: "10 multiple-choice questions on Effective Go.",
			Questions:   genQuestions,
		},
	})

	_, err := svc.CreateQuiz(context.Background(), 2, []string{"admin"}, 5, dto.CreateQuizRequest{Title: "Admin Quiz"})
	if !errors.Is(err, ErrQuizForbidden) {
		t.Fatalf("expected ErrQuizForbidden for non-lecturer, got %v", err)
	}

	_, err = svc.StartAIQuizGeneration(context.Background(), 3, []string{"student"}, 5, dto.GenerateAIQuizRequest{ContentItemID: 10})
	if !errors.Is(err, ErrQuizForbidden) {
		t.Fatalf("expected ErrQuizForbidden for student AI quiz, got %v", err)
	}

	created, err := svc.StartAIQuizGeneration(context.Background(), 7, []string{"lecturer"}, 5, dto.GenerateAIQuizRequest{ContentItemID: 10})
	if err != nil {
		t.Fatalf("start AI quiz failed: %v", err)
	}
	if !created.IsAIGenerated {
		t.Fatal("expected IsAIGenerated to be true")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		listed, err := svc.ListQuizzesForModule(context.Background(), 7, []string{"lecturer"}, 5)
		if err != nil {
			t.Fatalf("list failed: %v", err)
		}
		if len(listed) == 1 && listed[0].Status == "ready" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	studentViewBefore, err := svc.ListQuizzesForModule(context.Background(), 99, []string{"student"}, 5)
	if err != nil {
		t.Fatalf("student list failed: %v", err)
	}
	if len(studentViewBefore) != 1 || studentViewBefore[0].Status != "ready" {
		t.Fatalf("expected 1 ready quiz for student, got %+v", studentViewBefore)
	}
	if len(studentViewBefore[0].Questions) != 10 {
		t.Fatalf("expected 10 questions, got %d", len(studentViewBefore[0].Questions))
	}
	if studentViewBefore[0].Questions[0].Correct != nil {
		t.Fatal("student must NOT receive correct answers before submitting")
	}

	answers := []int{1, 1, 1, 1, 1, 0, 0, 0, 0, 0}
	result, err := svc.SubmitAttempt(context.Background(), 99, []string{"student"}, created.ID, dto.SubmitQuizAttemptRequest{Answers: answers})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if result.Attempt == nil || result.Attempt.Score != 5 || result.Attempt.Total != 10 {
		t.Fatalf("expected 5/10 score, got %+v", result.Attempt)
	}
	if result.Questions[0].Correct == nil || *result.Questions[0].Correct != 1 {
		t.Fatal("expected correct answers to be revealed after submitting quiz")
	}

	studentViewAfter, err := svc.ListQuizzesForModule(context.Background(), 99, []string{"student"}, 5)
	if err != nil {
		t.Fatalf("student list after submit failed: %v", err)
	}
	if studentViewAfter[0].Questions[0].Correct == nil {
		t.Fatal("expected correct answers to remain visible after quiz completion")
	}
}
