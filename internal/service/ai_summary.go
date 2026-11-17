package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// summaryProviderName is the provider recorded with a cached summary.
	summaryProviderName = "gemini"
	// summaryMaxOutputTokens caps what the model may generate for one call.
	summaryMaxOutputTokens = 1024
)

var (
	// ErrSummaryProviderUnavailable means the provider is not configured, timed out, or answered 429/5xx.
	ErrSummaryProviderUnavailable = errors.New("ai summary provider is unavailable")
	// ErrSummaryProviderInvalid means the provider answered with something the service cannot use.
	ErrSummaryProviderInvalid = errors.New("ai summary provider returned an unusable response")
)

// SummaryOutput is one generated summary and the provider metadata stored with it.
type SummaryOutput struct {
	Text     string
	Provider string
	Model    string
}

// SummaryProvider hides the vendor behind one call: a fixed system instruction plus a delimited data payload.
type SummaryProvider interface {
	Summarize(ctx context.Context, systemInstruction, data string) (SummaryOutput, error)
}

// GeminiSummaryProvider calls Gemini's generateContent. It reuses the request and response types of the quiz
// generator; the analytics logic never sees HTTP or the API key.
type GeminiSummaryProvider struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewGeminiSummaryProvider creates the provider. A non-positive timeout falls back to 60 seconds.
func NewGeminiSummaryProvider(apiKey, model string, timeout time.Duration) *GeminiSummaryProvider {
	if strings.TrimSpace(model) == "" {
		model = geminiFallbackModels[0]
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &GeminiSummaryProvider{
		apiKey:     strings.TrimSpace(apiKey),
		model:      strings.TrimSpace(model),
		baseURL:    defaultGeminiBaseURL,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Summarize sends one system instruction and one data payload. It retries once on a network error, 429, or 5xx.
func (g *GeminiSummaryProvider) Summarize(ctx context.Context, systemInstruction, data string) (SummaryOutput, error) {
	if g.apiKey == "" {
		return SummaryOutput{}, fmt.Errorf("%w: GEMINI_API_KEY is not configured", ErrSummaryProviderUnavailable)
	}

	payload := geminiGenerateRequest{
		SystemInstruction: &geminiContent{Parts: []geminiPart{{Text: systemInstruction}}},
		Contents:          []geminiContent{{Role: "user", Parts: []geminiPart{{Text: data}}}},
		GenerationConfig: geminiGenerationConfig{
			ResponseMimeType: "text/plain",
			Temperature:      0.2,
			MaxOutputTokens:  summaryMaxOutputTokens,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return SummaryOutput{}, fmt.Errorf("%w: %v", ErrSummaryProviderInvalid, err)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		text, retryable, callErr := g.call(ctx, body)
		if callErr == nil {
			return SummaryOutput{Text: text, Provider: summaryProviderName, Model: g.model}, nil
		}
		lastErr = callErr
		if !retryable || ctx.Err() != nil {
			break
		}
	}
	return SummaryOutput{}, lastErr
}

// call returns the generated text and whether the failure is worth one retry. Provider bodies are never returned
// to the caller: only the status code is kept, so no feedback text or vendor detail leaks into logs.
func (g *GeminiSummaryProvider) call(ctx context.Context, body []byte) (string, bool, error) {
	endpoint := fmt.Sprintf("%s/%s:generateContent?key=%s", strings.TrimRight(g.baseURL, "/"), g.model, g.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", false, fmt.Errorf("%w: %v", ErrSummaryProviderInvalid, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", true, fmt.Errorf("%w: request failed", ErrSummaryProviderUnavailable)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", true, fmt.Errorf("%w: could not read the response", ErrSummaryProviderUnavailable)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return "", true, fmt.Errorf("%w: status %d", ErrSummaryProviderUnavailable, resp.StatusCode)
	}

	var parsed geminiGenerateResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", false, fmt.Errorf("%w: invalid JSON (status %d)", ErrSummaryProviderInvalid, resp.StatusCode)
	}
	if resp.StatusCode >= 400 || parsed.Error != nil {
		return "", false, fmt.Errorf("%w: status %d", ErrSummaryProviderInvalid, resp.StatusCode)
	}

	var sb strings.Builder
	for _, cand := range parsed.Candidates {
		for _, part := range cand.Content.Parts {
			sb.WriteString(part.Text)
		}
	}
	text := strings.TrimSpace(sb.String())
	if text == "" {
		return "", false, fmt.Errorf("%w: empty text", ErrSummaryProviderInvalid)
	}
	return text, false, nil
}
