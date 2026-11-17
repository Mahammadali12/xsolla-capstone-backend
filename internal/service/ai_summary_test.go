package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func geminiBody(text string) string {
	quoted, _ := json.Marshal(text)
	return fmt.Sprintf(`{"candidates":[{"content":{"parts":[{"text":%s}]}}]}`, quoted)
}

func newTestSummaryProvider(url string, timeout time.Duration) *GeminiSummaryProvider {
	p := NewGeminiSummaryProvider("test-key", "gemini-test", timeout)
	p.baseURL = url
	return p
}

func TestGeminiSummaryProviderReturnsText(t *testing.T) {
	var gotSystem, gotData bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SystemInstruction *struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"systemInstruction"`
			Contents []struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("request body: %v", err)
		}
		gotSystem = body.SystemInstruction != nil && len(body.SystemInstruction.Parts) == 1 &&
			body.SystemInstruction.Parts[0].Text == "rules"
		gotData = len(body.Contents) == 1 && body.Contents[0].Parts[0].Text == "<feedback_records>x</feedback_records>"
		_, _ = w.Write([]byte(geminiBody("A plain summary.")))
	}))
	defer srv.Close()

	out, err := newTestSummaryProvider(srv.URL, time.Second).Summarize(context.Background(), "rules", "<feedback_records>x</feedback_records>")
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "A plain summary." || out.Provider != summaryProviderName || out.Model != "gemini-test" {
		t.Fatalf("output: %+v", out)
	}
	if !gotSystem || !gotData {
		t.Fatalf("system=%v data=%v", gotSystem, gotData)
	}
}

func TestGeminiSummaryProviderRetriesOnceOnServerError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(geminiBody("A plain summary.")))
	}))
	defer srv.Close()

	out, err := newTestSummaryProvider(srv.URL, time.Second).Summarize(context.Background(), "rules", "data")
	if err != nil || out.Text != "A plain summary." {
		t.Fatalf("output %+v, error %v", out, err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls: %d", got)
	}
}

func TestGeminiSummaryProviderFallsBackToAnotherModel(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if len(paths) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(geminiBody("A fallback summary.")))
	}))
	defer srv.Close()

	out, err := newTestSummaryProvider(srv.URL, time.Second).Summarize(context.Background(), "rules", "data")
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "A fallback summary." || out.Model != "gemini-3.6-flash" {
		t.Fatalf("output: %+v", out)
	}
	if len(paths) != 2 || paths[0] != "/gemini-test:generateContent" || paths[1] != "/gemini-3.6-flash:generateContent" {
		t.Fatalf("model paths: %v", paths)
	}
}

func TestGeminiSummaryProviderFailures(t *testing.T) {
	t.Run("not configured", func(t *testing.T) {
		p := NewGeminiSummaryProvider("", "gemini-test", time.Second)
		_, err := p.Summarize(context.Background(), "rules", "data")
		if !errors.Is(err, ErrSummaryProviderUnavailable) {
			t.Fatalf("error: %v", err)
		}
	})

	t.Run("client error falls through models", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer srv.Close()

		_, err := newTestSummaryProvider(srv.URL, time.Second).Summarize(context.Background(), "rules", "data")
		if !errors.Is(err, ErrSummaryProviderInvalid) {
			t.Fatalf("error: %v", err)
		}
		if got := calls.Load(); got != int32(len(geminiFallbackModels)+1) {
			t.Fatalf("calls: %d, want one call per model", got)
		}
	})

	t.Run("empty text", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(geminiBody("   ")))
		}))
		defer srv.Close()

		_, err := newTestSummaryProvider(srv.URL, time.Second).Summarize(context.Background(), "rules", "data")
		if !errors.Is(err, ErrSummaryProviderInvalid) {
			t.Fatalf("error: %v", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_, _ = w.Write([]byte(geminiBody("late")))
		}))
		defer srv.Close()

		_, err := newTestSummaryProvider(srv.URL, 20*time.Millisecond).Summarize(context.Background(), "rules", "data")
		if !errors.Is(err, ErrSummaryProviderUnavailable) {
			t.Fatalf("error: %v", err)
		}
	})
}

// A provider that never answers ends in one error after the budget, not after every model and retry has timed out.
func TestSummarizeStopsAfterOneBudget(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(1 * time.Second):
		}
	}))
	defer srv.Close()

	p := newTestSummaryProvider(srv.URL, 300*time.Millisecond)
	started := time.Now()
	_, err := p.Summarize(context.Background(), "instruction", "data")
	if !errors.Is(err, ErrSummaryProviderUnavailable) {
		t.Fatalf("err = %v, want ErrSummaryProviderUnavailable", err)
	}
	if took := time.Since(started); took > 1500*time.Millisecond {
		t.Errorf("took %v for a 300ms budget: the models and retries each waited their own timeout", took)
	}
	if n := calls.Load(); n > 2 {
		t.Errorf("%d calls for one budget, want at most 2 (the first model and its retry)", n)
	}
}

// The models are different from each other (a deprecated name Google routes to another entry would be asked twice), the
// summary starts with its own cheaper model, and the quiz model order starts with the configured one.
func TestGeminiModelOrder(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range geminiFallbackModels {
		if seen[m] {
			t.Errorf("%s is listed twice", m)
		}
		seen[m] = true
	}
	for _, old := range []string{"gemini-3.7-flash", "gemini-3.5-flash", "gemini-3.1-flash-lite"} {
		if seen[old] {
			t.Errorf("%s is deprecated or shuts down soon and must not be a fallback", old)
		}
	}

	summary := NewGeminiSummaryProvider("key", "", 0).orderedModels()
	if summary[0] != "gemini-3.5-flash-lite" {
		t.Errorf("summary starts with %s, want gemini-3.5-flash-lite", summary[0])
	}
	if got := NewGeminiSummaryProvider("key", "", 0).budget; got != 30*time.Second {
		t.Errorf("default budget %v, want 30s", got)
	}
	seenSummary := map[string]bool{}
	for _, m := range summary {
		if seenSummary[m] {
			t.Errorf("summary asks %s twice: %v", m, summary)
		}
		seenSummary[m] = true
	}

	quiz := NewGeminiQuizGenerator("key", "").orderedModels()
	if quiz[0] != "gemini-3.6-flash" {
		t.Errorf("quiz starts with %s, want gemini-3.6-flash", quiz[0])
	}
}
