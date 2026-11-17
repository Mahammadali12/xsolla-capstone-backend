package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"xsolla-capstone-api/internal/store"
)

const (
	// StudentSummaryNoFeedback is the deterministic answer when a student has no written feedback in the period.
	StudentSummaryNoFeedback = "There is not enough written feedback to generate a summary yet."
	// MentorSummaryNoFeedback is the deterministic answer when a mentor has no reviews in the period.
	MentorSummaryNoFeedback = "There are not enough submitted mentor reviews to generate a summary yet."

	studentFeedbackRecordsKey = "feedback"
	mentorReviewRecordsKey    = "reviews"
)

// SummaryResult is the service answer behind the API response.
type SummaryResult struct {
	Summary     string
	SourceCount int
	GeneratedAt *time.Time
	Cached      bool
	Period      AnalyticsPeriod
}

// summaryStore is the part of the store the summary service needs.
type summaryStore interface {
	GetAnalyticsPerson(ctx context.Context, id int64, role string) (*store.AnalyticsPerson, error)
	StudentFeedbackHistory(ctx context.Context, studentID int64, from, upper time.Time) ([]store.StudentFeedbackRecord, error)
	MentorReviewHistory(ctx context.Context, mentorID int64, from, upper time.Time) ([]store.MentorReviewRecord, error)
	GetSummaryCache(ctx context.Context, key store.SummaryCacheKey) (*store.SummaryCacheRow, error)
	SaveSummaryCache(ctx context.Context, key store.SummaryCacheKey, row store.SummaryCacheRow) error
}

// AnalyticsSummaryService turns persisted feedback into one cached, plain-text summary.
type AnalyticsSummaryService struct {
	store          summaryStore
	ai             SummaryProvider
	now            func() time.Time
	maxInputChars  int
	maxOutputChars int
	inflight       singleflight.Group
}

// NewAnalyticsSummaryService creates the service. Zero limits fall back to the defaults.
func NewAnalyticsSummaryService(st summaryStore, ai SummaryProvider, maxInputChars, maxOutputChars int) *AnalyticsSummaryService {
	if maxInputChars <= 0 {
		maxInputChars = 60000
	}
	if maxOutputChars <= 0 {
		maxOutputChars = 4000
	}
	return &AnalyticsSummaryService{
		store: st, ai: ai, now: time.Now,
		maxInputChars: maxInputChars, maxOutputChars: maxOutputChars,
	}
}

// Student summarizes the feedback the student received in the period.
func (s *AnalyticsSummaryService) Student(ctx context.Context, studentID int64, from, to *time.Time, force bool) (*SummaryResult, error) {
	p, err := resolveAnalyticsPeriod(s.now(), from, to)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.GetAnalyticsPerson(ctx, studentID, "student"); err != nil {
		return nil, err
	}
	rows, err := s.store.StudentFeedbackHistory(ctx, studentID, p.From, p.Upper())
	if err != nil {
		return nil, err
	}

	records := make([]any, 0, len(rows))
	for _, r := range rows {
		feedback := strings.TrimSpace(r.Feedback)
		if feedback == "" {
			continue
		}
		records = append(records, studentFeedbackPayload{
			Source:     r.Source,
			Module:     strings.TrimSpace(r.ModuleTitle),
			Assignment: strings.TrimSpace(r.Assignment),
			Date:       schoolDay(r.GradedAt),
			Feedback:   feedback,
		})
	}
	if len(records) == 0 {
		return &SummaryResult{Summary: StudentSummaryNoFeedback, Period: p}, nil
	}
	return s.generate(ctx, "student", studentID, p, force,
		studentSummaryPromptVersion, studentSummaryInstruction, studentFeedbackRecordsKey, records, nil)
}

// Mentor summarizes the reviews students submitted about the mentor in the period.
func (s *AnalyticsSummaryService) Mentor(ctx context.Context, mentorID int64, from, to *time.Time, force bool) (*SummaryResult, error) {
	p, err := resolveAnalyticsPeriod(s.now(), from, to)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.GetAnalyticsPerson(ctx, mentorID, "mentor"); err != nil {
		return nil, err
	}
	rows, err := s.store.MentorReviewHistory(ctx, mentorID, p.From, p.Upper())
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return &SummaryResult{Summary: MentorSummaryNoFeedback, Period: p}, nil
	}

	records, aggregates := normalizeMentorReviews(rows)
	return s.generate(ctx, "mentor", mentorID, p, force,
		mentorSummaryPromptVersion, mentorSummaryInstruction, mentorReviewRecordsKey, records,
		map[string]any{"aggregates": aggregates})
}

// generate handles the cache and collapses concurrent identical requests into one model call.
func (s *AnalyticsSummaryService) generate(ctx context.Context, subjectType string, subjectID int64,
	p AnalyticsPeriod, force bool, promptVersion, instruction, recordsKey string, records []any, extra map[string]any,
) (*SummaryResult, error) {
	payload := make(map[string]any, len(extra)+1)
	payload[recordsKey] = records
	for k, v := range extra {
		payload[k] = v
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal summary payload: %w", err)
	}
	fingerprint, err := summaryFingerprint(subjectType, promptVersion, p, payload)
	if err != nil {
		return nil, err
	}
	key := store.SummaryCacheKey{
		SubjectType: subjectType, SubjectID: subjectID,
		PeriodFrom: p.From.Format("2006-01-02"), PeriodTo: p.To.Format("2006-01-02"),
		Fingerprint: fingerprint, PromptVersion: promptVersion,
	}

	if !force {
		cached, err := s.store.GetSummaryCache(ctx, key)
		if err != nil {
			return nil, err
		}
		if cached != nil {
			log.Printf("analytics summary: %s %d cache=hit sources=%d prompt=%s",
				subjectType, subjectID, cached.SourceCount, promptVersion)
			return cachedSummaryResult(cached, p), nil
		}
	}

	flightKey := fmt.Sprintf("%s:%d:%s:%s", subjectType, subjectID, key.PeriodFrom, key.PeriodTo)
	v, err, _ := s.inflight.Do(flightKey, func() (any, error) {
		return s.generateAndStore(ctx, key, force, instruction, recordsKey, records, extra, payloadJSON, p)
	})
	if err != nil {
		return nil, err
	}
	return v.(*SummaryResult), nil
}

func (s *AnalyticsSummaryService) generateAndStore(ctx context.Context, key store.SummaryCacheKey, force bool,
	instruction, recordsKey string, records []any, extra map[string]any, payloadJSON []byte, p AnalyticsPeriod,
) (*SummaryResult, error) {
	started := time.Now()
	if !force {
		cached, err := s.store.GetSummaryCache(ctx, key)
		if err != nil {
			return nil, err
		}
		if cached != nil {
			return cachedSummaryResult(cached, p), nil
		}
	}

	out, err := s.generateText(ctx, instruction, recordsKey, records, extra, payloadJSON)
	if err != nil {
		return nil, err
	}
	text, err := validateSummaryOutput(out.Text, s.maxOutputChars)
	if err != nil {
		return nil, err
	}
	generatedAt := s.now().UTC()
	if err := s.store.SaveSummaryCache(ctx, key, store.SummaryCacheRow{
		SummaryText: text, SourceCount: len(records), Provider: out.Provider, Model: out.Model, GeneratedAt: generatedAt,
	}); err != nil {
		return nil, err
	}
	log.Printf("analytics summary: %s %d cache=miss sources=%d prompt=%s provider=%s model=%s duration=%v",
		key.SubjectType, key.SubjectID, len(records), key.PromptVersion, out.Provider, out.Model, time.Since(started))
	return &SummaryResult{Summary: text, SourceCount: len(records), GeneratedAt: &generatedAt, Period: p}, nil
}

// generateText makes one call when the payload fits, otherwise one call per chronological chunk and a final
// combine call. No record is dropped.
func (s *AnalyticsSummaryService) generateText(ctx context.Context, instruction, recordsKey string,
	records []any, extra map[string]any, payloadJSON []byte,
) (SummaryOutput, error) {
	if len(payloadJSON) <= s.maxInputChars {
		return s.ai.Summarize(ctx, instruction, wrapSummaryData(recordsKey, payloadJSON))
	}

	budget := s.maxInputChars - 512
	if budget < 1024 {
		budget = 1024
	}
	chunks, err := splitSummaryRecords(records, budget)
	if err != nil {
		return SummaryOutput{}, err
	}

	partials := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		chunkJSON, err := json.Marshal(map[string]any{recordsKey: chunk})
		if err != nil {
			return SummaryOutput{}, fmt.Errorf("marshal summary chunk: %w", err)
		}
		out, err := s.ai.Summarize(ctx, instruction, wrapSummaryData(recordsKey, chunkJSON))
		if err != nil {
			return SummaryOutput{}, err
		}
		text, err := validateSummaryOutput(out.Text, s.maxOutputChars)
		if err != nil {
			return SummaryOutput{}, err
		}
		partials = append(partials, text)
	}

	combine := make(map[string]any, len(extra)+2)
	combine["partial_summaries"] = partials
	combine["total_records"] = len(records)
	for k, v := range extra {
		combine[k] = v
	}
	combineJSON, err := json.Marshal(combine)
	if err != nil {
		return SummaryOutput{}, fmt.Errorf("marshal partial summaries: %w", err)
	}
	data := "These are partial summaries of the same records, produced under the same rules. Combine them into " +
		"one final summary. Do not add anything that is not in them.\n<partial_summaries>\n" +
		string(combineJSON) + "\n</partial_summaries>"
	return s.ai.Summarize(ctx, instruction, data)
}

// splitSummaryRecords groups records chronologically without exceeding the payload budget. One record that does
// not fit on its own is an error, never a silent truncation.
func splitSummaryRecords(records []any, budget int) ([][]any, error) {
	var chunks [][]any
	var current []any
	size := 0
	for _, record := range records {
		b, err := json.Marshal(record)
		if err != nil {
			return nil, fmt.Errorf("marshal feedback record: %w", err)
		}
		recordSize := len(b) + 1
		if len(current) > 0 && size+recordSize > budget {
			chunks = append(chunks, current)
			current, size = nil, 0
		}
		if len(current) == 0 && recordSize > budget {
			return nil, fmt.Errorf("%w: a record needs %d characters", ErrSummaryInputTooLarge, recordSize)
		}
		current = append(current, record)
		size += recordSize
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks, nil
}

func summaryFingerprint(subjectType, promptVersion string, p AnalyticsPeriod, payload map[string]any) (string, error) {
	input := struct {
		Type          string         `json:"type"`
		PromptVersion string         `json:"prompt_version"`
		From          string         `json:"from"`
		To            string         `json:"to"`
		Payload       map[string]any `json:"payload"`
	}{subjectType, promptVersion, p.From.Format("2006-01-02"), p.To.Format("2006-01-02"), payload}
	b, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal summary fingerprint: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func wrapSummaryData(recordsKey string, payload []byte) string {
	tag := "feedback_records"
	if recordsKey == mentorReviewRecordsKey {
		tag = "review_records"
	}
	return "<" + tag + ">\n" + string(payload) + "\n</" + tag + ">"
}

func cachedSummaryResult(row *store.SummaryCacheRow, p AnalyticsPeriod) *SummaryResult {
	generatedAt := row.GeneratedAt.UTC()
	return &SummaryResult{
		Summary: row.SummaryText, SourceCount: row.SourceCount,
		GeneratedAt: &generatedAt, Cached: true, Period: p,
	}
}

func schoolDay(t time.Time) string {
	return t.In(SchoolLocation).Format("2006-01-02")
}

// ---- normalized payloads ---------------------------------------------------------------------------------------

type studentFeedbackPayload struct {
	Source     string `json:"source"`
	Module     string `json:"module"`
	Assignment string `json:"assignment,omitempty"`
	Date       string `json:"feedback_date"`
	Feedback   string `json:"feedback"`
}

type mentorRatingsPayload struct {
	Clarity        *int `json:"clarity,omitempty"`
	Engagement     *int `json:"engagement,omitempty"`
	Comfort        *int `json:"comfort,omitempty"`
	TimeManagement *int `json:"time_management,omitempty"`
}

type mentorReviewPayload struct {
	Module          string                `json:"module"`
	SubmittedAt     string                `json:"submitted_at"`
	HadSession      bool                  `json:"had_session"`
	NoSessionReason *string               `json:"no_session_reason,omitempty"`
	Topics          []string              `json:"topics"`
	OtherTopic      *string               `json:"other_topic,omitempty"`
	Ratings         *mentorRatingsPayload `json:"ratings,omitempty"`
	Overall         int                   `json:"overall"`
	Comment         string                `json:"comment,omitempty"`
}

type summaryRatingAggregate struct {
	Count   int      `json:"count"`
	Average *float64 `json:"average"`
}

type mentorSummaryAggregates struct {
	Clarity            summaryRatingAggregate `json:"clarity"`
	Engagement         summaryRatingAggregate `json:"engagement"`
	Comfort            summaryRatingAggregate `json:"comfort"`
	TimeManagement     summaryRatingAggregate `json:"time_management"`
	Overall            summaryRatingAggregate `json:"overall"`
	SessionsReported   int                    `json:"sessions_reported"`
	NoSessionsReported int                    `json:"no_sessions_reported"`
	NoSessionReasons   map[string]int         `json:"no_session_reasons"`
	Topics             map[string]int         `json:"topics"`
}

type ratingTally struct {
	total int
	count int
}

func (t ratingTally) aggregate() summaryRatingAggregate {
	if t.count == 0 {
		return summaryRatingAggregate{}
	}
	avg := float64(t.total) / float64(t.count)
	return summaryRatingAggregate{Count: t.count, Average: roundAverage(&avg)}
}

func normalizeMentorReviews(rows []store.MentorReviewRecord) ([]any, mentorSummaryAggregates) {
	aggregates := mentorSummaryAggregates{
		NoSessionReasons: map[string]int{},
		Topics:           map[string]int{},
	}
	var clarity, engagement, comfort, timeManagement, overall ratingTally

	records := make([]any, 0, len(rows))
	for _, r := range rows {
		topics := r.Topics
		if topics == nil {
			topics = []string{}
		}
		payload := mentorReviewPayload{
			Module:          strings.TrimSpace(r.ModuleTitle),
			SubmittedAt:     schoolDay(r.SubmittedAt),
			HadSession:      r.HadSession,
			NoSessionReason: r.NoSessionReason,
			Topics:          topics,
			OtherTopic:      r.OtherTopic,
			Overall:         r.Overall,
			Comment:         strings.TrimSpace(derefString(r.Comment)),
		}
		if r.Clarity != nil || r.Engagement != nil || r.Comfort != nil || r.Time != nil {
			payload.Ratings = &mentorRatingsPayload{
				Clarity: r.Clarity, Engagement: r.Engagement, Comfort: r.Comfort, TimeManagement: r.Time,
			}
		}
		records = append(records, payload)

		if r.HadSession {
			aggregates.SessionsReported++
		} else {
			aggregates.NoSessionsReported++
			if r.NoSessionReason != nil {
				aggregates.NoSessionReasons[*r.NoSessionReason]++
			}
		}
		for _, topic := range topics {
			aggregates.Topics[topic]++
		}
		if r.Clarity != nil {
			clarity.total += *r.Clarity
			clarity.count++
		}
		if r.Engagement != nil {
			engagement.total += *r.Engagement
			engagement.count++
		}
		if r.Comfort != nil {
			comfort.total += *r.Comfort
			comfort.count++
		}
		if r.Time != nil {
			timeManagement.total += *r.Time
			timeManagement.count++
		}
		overall.total += r.Overall
		overall.count++
	}

	aggregates.Clarity = clarity.aggregate()
	aggregates.Engagement = engagement.aggregate()
	aggregates.Comfort = comfort.aggregate()
	aggregates.TimeManagement = timeManagement.aggregate()
	aggregates.Overall = overall.aggregate()
	return records, aggregates
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
