package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	// ErrSummaryOutputInvalid means the model returned empty text, markup, or too much text.
	ErrSummaryOutputInvalid = errors.New("ai summary output is unusable")
	// ErrSummaryInputTooLarge means one feedback record alone does not fit the configured input limit.
	ErrSummaryInputTooLarge = errors.New("one feedback record exceeds the ai summary input limit")
)

var (
	htmlTagPattern   = regexp.MustCompile(`(?i)</?[a-z][^>]*>`)
	markdownPattern  = regexp.MustCompile("(?m)^\\s{0,3}#{1,6}\\s|\\*\\*|__")
	codeFencePattern = regexp.MustCompile("```|~~~")
)

// validateSummaryOutput turns one provider answer into the plain text the API returns. Anything that is not
// prose fails: the client must never receive markup or JSON, and a bad answer is an error, not a fallback.
func validateSummaryOutput(text string, maxChars int) (string, error) {
	trimmed := strings.TrimSpace(text)
	switch {
	case trimmed == "":
		return "", fmt.Errorf("%w: empty text", ErrSummaryOutputInvalid)
	case utf8.RuneCountInString(trimmed) > maxChars:
		return "", fmt.Errorf("%w: longer than %d characters", ErrSummaryOutputInvalid, maxChars)
	case codeFencePattern.MatchString(trimmed):
		return "", fmt.Errorf("%w: contains a code block", ErrSummaryOutputInvalid)
	case htmlTagPattern.MatchString(trimmed):
		return "", fmt.Errorf("%w: contains HTML", ErrSummaryOutputInvalid)
	case markdownPattern.MatchString(trimmed):
		return "", fmt.Errorf("%w: contains Markdown", ErrSummaryOutputInvalid)
	case looksLikeJSON(trimmed):
		return "", fmt.Errorf("%w: contains JSON", ErrSummaryOutputInvalid)
	}
	return trimmed, nil
}

func looksLikeJSON(text string) bool {
	first := text[0]
	if first != '{' && first != '[' {
		return false
	}
	return json.Valid([]byte(text))
}
