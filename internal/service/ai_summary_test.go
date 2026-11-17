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
