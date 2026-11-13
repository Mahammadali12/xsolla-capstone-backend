package service

import "testing"

func TestNormalizeMeetingURL(t *testing.T) {
	empty := ""
	invalid := "javascript:alert(1)"
	valid := "https://meet.google.com/abc-defg-hij"
	plain := "http://example.com/meet"

	if got, err := normalizeMeetingURL(nil); got != nil || err != nil {
		t.Fatalf("nil link should stay empty, got %v, %v", got, err)
	}
	if got, err := normalizeMeetingURL(&empty); got != nil || err != nil {
		t.Fatalf("empty link should clear, got %v, %v", got, err)
	}
	if _, err := normalizeMeetingURL(&invalid); err != ErrInvalidMeetingURL {
		t.Fatalf("expected ErrInvalidMeetingURL, got %v", err)
	}
	if got, err := normalizeMeetingURL(&valid); err != nil || got == nil || *got != valid {
		t.Fatalf("https link should pass through, got %v, %v", got, err)
	}
	if got, err := normalizeMeetingURL(&plain); err != nil || got == nil || *got != plain {
		t.Fatalf("http link should pass through, got %v, %v", got, err)
	}
}
