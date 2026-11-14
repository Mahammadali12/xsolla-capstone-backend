package service

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
)

// Profile page, part 2: look and accessibility settings. Only the person sees them; they follow the person to any
// device. Stored as one JSON object, so a new option only needs a line here, no migration.

// preferenceChoices are the settings that take one of a few words; the first word is the default.
var preferenceChoices = map[string][]string{
	"text_size": {"normal", "large", "xlarge"},
	"theme":     {"system", "light", "dark"},
	"font":      {"default", "dyslexia"},
	"spacing":   {"normal", "relaxed"},
	"density":   {"comfortable", "compact"},
}

// preferenceFlags are the on/off settings; all are off by default.
var preferenceFlags = []string{"high_contrast", "colorblind_safe", "reduce_motion", "strong_focus"}

// DefaultPreferences is what a person who never saved anything gets.
func DefaultPreferences() map[string]any {
	out := make(map[string]any, len(preferenceChoices)+len(preferenceFlags))
	for k, choices := range preferenceChoices {
		out[k] = choices[0]
	}
	for _, k := range preferenceFlags {
		out[k] = false
	}
	return out
}

type preferencesStore interface {
	GetUserPreferences(ctx context.Context, userID int64) ([]byte, error)
	MergeUserPreferences(ctx context.Context, userID int64, patch []byte) ([]byte, error)
}

// PreferencesService handles the look and accessibility settings.
type PreferencesService struct {
	store preferencesStore
}

// NewPreferencesService creates the service.
func NewPreferencesService(s preferencesStore) *PreferencesService {
	return &PreferencesService{store: s}
}

// Get returns every setting: the saved value where there is a valid one, the default otherwise.
func (s *PreferencesService) Get(ctx context.Context, userID int64) (map[string]any, error) {
	saved, err := s.store.GetUserPreferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	return withDefaults(saved), nil
}

// Update changes only the sent keys and returns every setting. An unknown key or a value that is not allowed is
// a *BadRequestError naming the key; nothing is saved then.
func (s *PreferencesService) Update(ctx context.Context, userID int64, patch map[string]json.RawMessage) (map[string]any, error) {
	clean, err := validatePreferences(patch)
	if err != nil {
		return nil, err
	}
	if len(clean) == 0 {
		return s.Get(ctx, userID)
	}
	body, err := json.Marshal(clean)
	if err != nil {
		return nil, err
	}
	saved, err := s.store.MergeUserPreferences(ctx, userID, body)
	if err != nil {
		return nil, err
	}
	return withDefaults(saved), nil
}

func validatePreferences(patch map[string]json.RawMessage) (map[string]any, error) {
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	sort.Strings(keys) // the first broken key is always the same one
	clean := make(map[string]any, len(patch))
	for _, k := range keys {
		raw := bytes.TrimSpace(patch[k])
		if choices, ok := preferenceChoices[k]; ok {
			var v string
			if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &v) != nil || !slices.Contains(choices, v) {
				return nil, badRequest("%s must be one of %s", k, quoted(choices))
			}
			clean[k] = v
			continue
		}
		if slices.Contains(preferenceFlags, k) {
			var v bool
			if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &v) != nil {
				return nil, badRequest("%s must be true or false", k)
			}
			clean[k] = v
			continue
		}
		return nil, badRequest("%q is not a setting: use %s", k, strings.Join(preferenceKeys(), ", "))
	}
	return clean, nil
}

// withDefaults reads the saved JSON leniently: unknown keys and values that are no longer allowed are ignored.
func withDefaults(saved []byte) map[string]any {
	out := DefaultPreferences()
	var stored map[string]json.RawMessage
	if len(saved) == 0 || json.Unmarshal(saved, &stored) != nil {
		return out
	}
	for k, raw := range stored {
		if valid, err := validatePreferences(map[string]json.RawMessage{k: raw}); err == nil {
			out[k] = valid[k]
		}
	}
	return out
}

func preferenceKeys() []string {
	keys := make([]string, 0, len(preferenceChoices)+len(preferenceFlags))
	for k := range preferenceChoices {
		keys = append(keys, k)
	}
	keys = append(keys, preferenceFlags...)
	sort.Strings(keys)
	return keys
}

func quoted(list []string) string {
	q := make([]string, len(list))
	for i, v := range list {
		q[i] = `"` + v + `"`
	}
	return strings.Join(q, ", ")
}
