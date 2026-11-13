package notification

import (
	"encoding/json"
	"errors"
	"fmt"

	"xsolla-notification-service/internal/events"
)

// ErrNoRecipients means an enabled policy matched nobody in the event payload.
var ErrNoRecipients = errors.New("no matching recipients")

// resolveRecipients keeps the recipients whose role the policy names, once per
// user id even when several roles match.
func resolveRecipients(roles []string, data json.RawMessage) ([]events.Recipient, error) {
	var payload struct {
		Recipients []events.Recipient `json:"recipients"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode recipients: %w", err)
	}

	wanted := make(map[string]bool, len(roles))
	for _, role := range roles {
		wanted[role] = true
	}

	seen := make(map[int64]bool, len(payload.Recipients))
	var resolved []events.Recipient
	for _, recipient := range payload.Recipients {
		if !wanted[recipient.Role] || seen[recipient.ID] {
			continue
		}
		seen[recipient.ID] = true
		resolved = append(resolved, recipient)
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("%w for roles %v", ErrNoRecipients, roles)
	}
	return resolved, nil
}
