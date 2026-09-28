package event

// Direction classifies a replica-count delta: +1 for a scale-up, -1 for a
// scale-down, and 0 for no change.
func Direction(delta int32) int {
	switch {
	case delta > 0:
		return 1
	case delta < 0:
		return -1
	default:
		return 0
	}
}

// FlipPoints returns the indices i into sizes at which the scaling direction
// reverses: the move sizes[i-1] -> sizes[i] runs opposite to the previous
// non-zero delta. Zero deltas neither count as flips nor reset the tracked
// direction. It is the single shared definition of "direction flip" used by
// the churn, flapping, and timeline analyzers; do not re-implement the walk.
func FlipPoints(sizes []int32) []int {
	var flips []int
	prevDirection := 0
	for i := 1; i < len(sizes); i++ {
		direction := Direction(sizes[i] - sizes[i-1])
		if direction == 0 {
			continue
		}
		if prevDirection != 0 && direction != prevDirection {
			flips = append(flips, i)
		}
		prevDirection = direction
	}
	return flips
}

// CountDirectionFlips returns the number of direction reversals across a
// replica-count sequence. It is FlipPoints without the indices.
func CountDirectionFlips(sizes []int32) int {
	return len(FlipPoints(sizes))
}

// RescaleSizes projects rescale data onto its NewSize sequence for the
// direction helpers above.
func RescaleSizes(rescales []RescaleData) []int32 {
	sizes := make([]int32, len(rescales))
	for i, r := range rescales {
		sizes[i] = r.NewSize
	}
	return sizes
}
