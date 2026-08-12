// Package model contains the stable, importable value types shared by the
// public HPA analysis subpackages. Keeping these names outside internal lets
// callers use subpackage APIs without depending on the root facade.
package model

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/confidence"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/event"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/suggestion"
)

// Event is an observed Kubernetes event relevant to an HPA.
type Event = event.Event

// RescaleData captures replica changes parsed from an event.
type RescaleData = event.RescaleData

// Confidence represents the confidence assigned to an analysis result.
type Confidence = confidence.Confidence

// Classification describes the confidence class of an analysis result.
type Classification = confidence.Classification

// Severity represents the user-facing severity of a finding.
type Severity = confidence.Severity

const (
	// ConfidenceHigh indicates strong supporting evidence.
	ConfidenceHigh = confidence.High
	// ConfidenceMedium indicates partial supporting evidence.
	ConfidenceMedium = confidence.Medium
	// ConfidenceLow indicates limited supporting evidence.
	ConfidenceLow = confidence.Low
	// SeverityInfo marks an informational finding.
	SeverityInfo = confidence.Info
	// SeverityWarning marks a warning finding.
	SeverityWarning = confidence.Warning
	// SeverityError marks an error finding.
	SeverityError = confidence.Error
)

// Suggestion describes a recommended HPA change.
type Suggestion = suggestion.Suggestion

// GuardResult records policy checks for a suggestion.
type GuardResult = suggestion.GuardResult

// GuardBlocked records a policy check that blocks a suggestion.
type GuardBlocked = suggestion.GuardBlocked

// GuardWarning records a non-blocking policy warning.
type GuardWarning = suggestion.GuardWarning
