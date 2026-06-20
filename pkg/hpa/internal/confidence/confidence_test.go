package confidence

import "testing"

func TestConfidence_Classify(t *testing.T) {
	cases := []struct {
		name string
		in   Confidence
		want Classification
	}{
		{"high", High, ClassificationObserved},
		{"medium", Medium, ClassificationEstimated},
		{"low falls through default", Low, ClassificationUnknown},
		{"empty falls through default", Confidence(""), ClassificationUnknown},
		{"unknown value falls through default", Confidence("speculative"), ClassificationUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Classify(); got != tc.want {
				t.Fatalf("Classify(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestClassification_Label(t *testing.T) {
	for _, c := range []Classification{ClassificationObserved, ClassificationEstimated, ClassificationUnknown} {
		if got := c.Label(); got != string(c) {
			t.Fatalf("Label() = %q, want %q", got, string(c))
		}
	}
}

func TestSeverity_Constants(t *testing.T) {
	// Sanity check the exported severity vocabulary stays stable; renderers
	// and JSON consumers depend on these exact strings.
	want := map[Severity]string{
		Info:    "info",
		Warning: "warning",
		Error:   "error",
	}
	for sev, wantStr := range want {
		if string(sev) != wantStr {
			t.Fatalf("severity %v = %q, want %q", sev, sev, wantStr)
		}
	}
}
