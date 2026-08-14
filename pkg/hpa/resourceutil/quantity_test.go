package resourceutil

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestMultiply(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		factor int64
		want   string
	}{
		{name: "fractional CPU", value: "100m", factor: 10, want: "1"},
		{name: "memory", value: "128Mi", factor: 5, want: "640Mi"},
		{name: "zero factor", value: "100m", factor: 0, want: "0"},
		{name: "negative factor", value: "100m", factor: -1, want: "0"},
		{name: "zero quantity", value: "0", factor: 10, want: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Multiply(resource.MustParse(tt.value), tt.factor)
			if got.Cmp(resource.MustParse(tt.want)) != 0 {
				t.Fatalf("Multiply(%s, %d) = %s, want %s", tt.value, tt.factor, got.String(), tt.want)
			}
		})
	}
}
