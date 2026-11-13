// Package notification decides which recipients and channels an event reaches.
package notification

// Policy is the notification rule for one event type. The event type is the
// registry key, so it is not repeated here.
type Policy struct {
	Enabled    bool
	Recipients []string // recipient roles: student, mentor, lecturer, admin
	Channels   []string // channel names, e.g. email
	Template   string
}

// Registry maps an event type to its policy.
type Registry struct {
	byType map[string]Policy
}

// NewRegistry builds a registry from policies keyed by event type.
func NewRegistry(policies map[string]Policy) *Registry {
	byType := make(map[string]Policy, len(policies))
	for eventType, policy := range policies {
		byType[eventType] = policy
	}
	return &Registry{byType: byType}
}

// Lookup returns the policy for an event type and whether one exists.
func (r *Registry) Lookup(eventType string) (Policy, bool) {
	policy, ok := r.byType[eventType]
	return policy, ok
}

// Len reports how many event types have a policy.
func (r *Registry) Len() int {
	return len(r.byType)
}
