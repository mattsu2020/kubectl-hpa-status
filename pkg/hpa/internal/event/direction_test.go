package event

import "testing"

func TestDirection(t *testing.T) {
	cases := []struct {
		delta int32
		want  int
	}{
		{delta: 5, want: 1},
		{delta: -3, want: -1},
		{delta: 0, want: 0},
	}
	for _, tc := range cases {
		if got := Direction(tc.delta); got != tc.want {
			t.Fatalf("Direction(%d) = %d, want %d", tc.delta, got, tc.want)
		}
	}
}

func TestFlipPoints(t *testing.T) {
	cases := []struct {
		name  string
		sizes []int32
		want  []int
	}{
		{name: "empty", sizes: nil, want: nil},
		{name: "single value", sizes: []int32{3}, want: nil},
		{name: "monotonic up", sizes: []int32{1, 2, 3, 4}, want: nil},
		{name: "one flip", sizes: []int32{1, 3, 2}, want: []int{2}},
		{name: "two flips", sizes: []int32{1, 3, 2, 4}, want: []int{2, 3}},
		{name: "zero deltas do not reset direction", sizes: []int32{1, 3, 3, 2}, want: []int{3}},
		{name: "zero deltas do not create flips", sizes: []int32{1, 2, 2, 3}, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FlipPoints(tc.sizes)
			if len(got) != len(tc.want) {
				t.Fatalf("FlipPoints(%v) = %v, want %v", tc.sizes, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("FlipPoints(%v) = %v, want %v", tc.sizes, got, tc.want)
				}
			}
			if CountDirectionFlips(tc.sizes) != len(tc.want) {
				t.Fatalf("CountDirectionFlips(%v) disagrees with FlipPoints", tc.sizes)
			}
		})
	}
}

func TestRescaleSizes(t *testing.T) {
	rescales := []RescaleData{
		{NewSize: 2},
		{NewSize: 5},
	}
	got := RescaleSizes(rescales)
	if len(got) != 2 || got[0] != 2 || got[1] != 5 {
		t.Fatalf("RescaleSizes = %v, want [2 5]", got)
	}
}
