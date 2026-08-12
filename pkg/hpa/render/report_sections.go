package render

// reportSection is the canonical cross-format report order. A renderer must
// handle every value; the shared loop prevents Markdown and HTML drift.
type reportSection uint8

const (
	sectionOverview reportSection = iota
	sectionSummary
	sectionConditions
	sectionMetrics
	sectionRecommendations
	sectionSuggestions
	sectionEvents
	sectionPods
	sectionSimulation
	sectionMetricFreshness
	sectionCapacity
	sectionDecisionTrace
	sectionWarnings
)

var reportSections = [...]reportSection{
	sectionOverview, sectionSummary, sectionConditions, sectionMetrics,
	sectionRecommendations, sectionSuggestions, sectionEvents, sectionPods,
	sectionSimulation, sectionMetricFreshness, sectionCapacity,
	sectionDecisionTrace, sectionWarnings,
}
