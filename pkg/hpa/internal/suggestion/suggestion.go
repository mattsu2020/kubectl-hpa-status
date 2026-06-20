// Package suggestion defines the Suggestion and GuardResult types shared
// across the pkg/hpa analysis domains. These were lifted out of pkg/hpa/types.go
// so leaf sub-packages (policy, etc.) can produce and consume suggestions
// without reaching back into the analysis core.
package suggestion

// Suggestion describes an actionable recommendation for improving HPA behavior.
type Suggestion struct {
	Title         string   `json:"title" yaml:"title"`
	Description   string   `json:"description" yaml:"description"`
	Command       string   `json:"command,omitempty" yaml:"command,omitempty"`
	Patch         string   `json:"patch,omitempty" yaml:"patch,omitempty"`
	Risk          string   `json:"risk,omitempty" yaml:"risk,omitempty"`
	Preconditions []string `json:"preconditions,omitempty" yaml:"preconditions,omitempty"`
	Warnings      []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	Apply         bool     `json:"apply,omitempty" yaml:"apply,omitempty"`
}

// GuardResult holds policy guard decisions for suggested patches.
type GuardResult struct {
	Allowed  []Suggestion   `json:"allowed,omitempty" yaml:"allowed,omitempty"`
	Blocked  []GuardBlocked `json:"blocked,omitempty" yaml:"blocked,omitempty"`
	Warnings []GuardWarning `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// GuardBlocked describes a suggestion blocked by a policy rule.
type GuardBlocked struct {
	Suggestion Suggestion `json:"suggestion" yaml:"suggestion"`
	Reason     string     `json:"reason" yaml:"reason"`
	PolicyRule string     `json:"policyRule" yaml:"policyRule"`
}

// GuardWarning describes a suggestion allowed with a policy warning.
type GuardWarning struct {
	Suggestion Suggestion `json:"suggestion" yaml:"suggestion"`
	Reason     string     `json:"reason" yaml:"reason"`
	PolicyRule string     `json:"policyRule" yaml:"policyRule"`
}
