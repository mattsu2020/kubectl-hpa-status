package simulate_test

// The simulate package receives its analysis, metric-identity, and formatting
// dependencies via function injection from the hpa root package's init() (see
// pkg/hpa/simulate_init.go) to break the import cycle. That init only runs in
// binaries that link pkg/hpa, which the simulate package's own test binary
// otherwise does not. Importing pkg/hpa from this external test package links
// it into the test binary so the injection is installed before any test runs,
// letting the internal tests in this directory exercise production behavior
// instead of panicking with "SetAnalyzeFunc must be called".
import (
	_ "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)
