package notification

import (
	"os"
	"path/filepath"
	"testing"
)

func writePolicies(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policies.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write policies: %v", err)
	}
	return path
}

func TestLoadPolicies(t *testing.T) {
	path := writePolicies(t, `{
		"mentor_slot.booked": {
			"enabled": true,
			"recipients": ["mentor", "student"],
			"channels": ["email"],
			"template": "booking_confirmation"
		},
		"submission.graded": {
			"enabled": false,
			"recipients": ["student"]
		}
	}`)

	registry, err := LoadPolicies(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if registry.Len() != 2 {
		t.Fatalf("len = %d, want 2", registry.Len())
	}

	policy, ok := registry.Lookup("mentor_slot.booked")
	if !ok || !policy.Enabled || policy.Template != "booking_confirmation" {
		t.Fatalf("unexpected policy: %+v, %v", policy, ok)
	}
	if len(policy.Recipients) != 2 || policy.Recipients[0] != "mentor" || len(policy.Channels) != 1 || policy.Channels[0] != "email" {
		t.Fatalf("unexpected policy contents: %+v", policy)
	}

	disabled, ok := registry.Lookup("submission.graded")
	if !ok || disabled.Enabled {
		t.Fatalf("unexpected disabled policy: %+v, %v", disabled, ok)
	}
}

func TestLoadPoliciesEmptyFile(t *testing.T) {
	registry, err := LoadPolicies(writePolicies(t, `{}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if registry.Len() != 0 {
		t.Fatalf("len = %d, want 0", registry.Len())
	}
}

func TestLoadPoliciesRejectsInvalidFiles(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `{`},
		{"not an object", `[]`},
		{"empty event type", `{"": {"enabled": false}}`},
		{"event type without dot", `{"mentor_slot": {"enabled": false}}`},
		{"unknown role", `{"mentor_slot.booked": {"enabled": false, "recipients": ["teacher"]}}`},
		{"enabled without recipients", `{"mentor_slot.booked": {"enabled": true, "channels": ["email"], "template": "t"}}`},
		{"enabled without channels", `{"mentor_slot.booked": {"enabled": true, "recipients": ["mentor"], "template": "t"}}`},
		{"enabled without template", `{"mentor_slot.booked": {"enabled": true, "recipients": ["mentor"], "channels": ["email"]}}`},
		{"blank channel", `{"mentor_slot.booked": {"enabled": true, "recipients": ["mentor"], "channels": [" "], "template": "t"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := LoadPolicies(writePolicies(t, tc.body)); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestLoadPoliciesDisabledMayBeIncomplete(t *testing.T) {
	registry, err := LoadPolicies(writePolicies(t, `{"mentor_slot.booked": {"enabled": false}}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if policy, ok := registry.Lookup("mentor_slot.booked"); !ok || policy.Enabled {
		t.Fatalf("unexpected policy: %+v, %v", policy, ok)
	}
}

func TestLoadPoliciesMissingFile(t *testing.T) {
	if _, err := LoadPolicies(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("want an error")
	}
}
