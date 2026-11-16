package service

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSummaryOutput(t *testing.T) {
	valid := []string{
		"Students describe the mentor as clear and approachable.",
		"Two short paragraphs are fine.\n\nEven with a plain line break.",
		"A plain comparison a < b > c is not HTML.",
	}
	for _, text := range valid {
		if _, err := validateSummaryOutput(text, 4000); err != nil {
			t.Errorf("valid text %q rejected: %v", text, err)
		}
	}

	invalid := []string{
		"",
		"   ",
		"<p>HTML</p>",
		"## Heading",
		"**bold**",
		"```json\n{}\n```",
		`{"summary":"invented"}`,
		"[" + `"a"` + "]",
		strings.Repeat("a", 4001),
	}
	for _, text := range invalid {
		if _, err := validateSummaryOutput(text, 4000); !errors.Is(err, ErrSummaryOutputInvalid) {
			t.Errorf("invalid text %q accepted: %v", text, err)
		}
	}
}
