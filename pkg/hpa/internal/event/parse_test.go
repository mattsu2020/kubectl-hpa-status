package event

import "testing"

func TestParseNewSize(t *testing.T) {
	for _, tc := range []struct {
		message string
		want    int32
		ok      bool
	}{
		{"New size: 5; reason: cpu", 5, true},
		{"new SIZE: 0", 0, true},
		{"no size", 0, false},
		{"New size: 999999999999", 0, false},
	} {
		got, ok := ParseNewSize(tc.message)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("ParseNewSize(%q) = (%d, %v), want (%d, %v)", tc.message, got, ok, tc.want, tc.ok)
		}
	}
}
