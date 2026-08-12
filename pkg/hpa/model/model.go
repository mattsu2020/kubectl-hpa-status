// Package model contains the stable, importable value types shared by the
// public HPA analysis subpackages. Keeping these names outside internal lets
// callers use subpackage APIs without depending on the root facade.
package model

import (
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/confidence"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/event"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/hpa/internal/suggestion"
)

type Event = event.Event
type RescaleData = event.RescaleData

type Confidence = confidence.Confidence
type Classification = confidence.Classification
type Severity = confidence.Severity

const (
	ConfidenceHigh   = confidence.High
	ConfidenceMedium = confidence.Medium
	ConfidenceLow    = confidence.Low
	SeverityInfo     = confidence.Info
	SeverityWarning  = confidence.Warning
	SeverityError    = confidence.Error
)

type Suggestion = suggestion.Suggestion
type GuardResult = suggestion.GuardResult
type GuardBlocked = suggestion.GuardBlocked
type GuardWarning = suggestion.GuardWarning
