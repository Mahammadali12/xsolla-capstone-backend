package notification

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"xsolla-notification-service/internal/events"
)

// filePolicy is one policy entry in the JSON policies file.
type filePolicy struct {
	Enabled    bool     `json:"enabled"`
	Recipients []string `json:"recipients"`
	Channels   []string `json:"channels"`
	Template   string   `json:"template"`
}

// knownRoles are the recipient roles defined by the event contract.
var knownRoles = map[string]bool{
	"student":  true,
	"mentor":   true,
	"lecturer": true,
	"admin":    true,
}

// LoadPolicies reads a JSON file of policies keyed by event type.
func LoadPolicies(path string) (*Registry, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries map[string]filePolicy
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	policies := make(map[string]Policy, len(entries))
	for eventType, entry := range entries {
		if err := validatePolicy(eventType, entry); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		policies[eventType] = Policy{
			Enabled:    entry.Enabled,
			Recipients: entry.Recipients,
			Channels:   entry.Channels,
			Template:   entry.Template,
		}
	}
	return NewRegistry(policies), nil
}

func validatePolicy(eventType string, policy filePolicy) error {
	if !events.ValidType(eventType) {
		return fmt.Errorf("policy %q: event type must look like domain.event", eventType)
	}
	for _, role := range policy.Recipients {
		if !knownRoles[role] {
			return fmt.Errorf("policy %q: unknown recipient role %q", eventType, role)
		}
	}
	if !policy.Enabled {
		return nil
	}
	if len(policy.Recipients) == 0 {
		return fmt.Errorf("policy %q: enabled policy needs at least one recipient role", eventType)
	}
	if len(policy.Channels) == 0 {
		return fmt.Errorf("policy %q: enabled policy needs at least one channel", eventType)
	}
	for _, channel := range policy.Channels {
		if strings.TrimSpace(channel) == "" {
			return fmt.Errorf("policy %q: channel must not be blank", eventType)
		}
	}
	if strings.TrimSpace(policy.Template) == "" {
		return fmt.Errorf("policy %q: enabled policy needs a template", eventType)
	}
	return nil
}
