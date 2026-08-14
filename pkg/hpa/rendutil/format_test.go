package rendutil

import (
	"testing"
	"time"
)

func TestDurationFormats(t *testing.T) {
	if got := DurationSpaced(4*time.Minute + 12*time.Second); got != "4m 12s" {
		t.Fatalf("DurationSpaced() = %q", got)
	}
	if got := DurationCompact(25*time.Hour + 3*time.Minute); got != "1d1h" {
		t.Fatalf("DurationCompact() = %q", got)
	}
	if got := DurationCompactHMS(25*time.Hour + 3*time.Minute); got != "25h3m" {
		t.Fatalf("DurationCompactHMS() = %q", got)
	}
}

func TestProgressBar(t *testing.T) {
	if got := ProgressBar(1, 2, 4); got != "██░░" {
		t.Fatalf("ProgressBar() = %q", got)
	}
	if got := ProgressBar(3, 2, 4); got != "████" {
		t.Fatalf("ProgressBar() clamp = %q", got)
	}
	if got := ProgressBarFloor(98, 100, 20); got != "███████████████████░" {
		t.Fatalf("ProgressBarFloor() = %q", got)
	}
}
