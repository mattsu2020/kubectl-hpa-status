package rendutil

import (
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func TestTruncateDisplayWidthPreservesUTF8(t *testing.T) {
	got := TruncateDisplayWidth("日本語abc", 5, "…")
	if !utf8.ValidString(got) {
		t.Fatalf("invalid UTF-8: %q", got)
	}
	if ansi.StringWidth(got) > 5 {
		t.Fatalf("display width exceeded: %q", got)
	}
}

func TestPadDisplayWidth(t *testing.T) {
	got := PadDisplayWidth("日", 4)
	if ansi.StringWidth(got) != 4 {
		t.Fatalf("display width = %d, want 4", ansi.StringWidth(got))
	}
}

func TestPadDisplayWidthNoPaddingWhenAlreadyWide(t *testing.T) {
	if got := PadDisplayWidth("hello", 3); got != "hello" {
		t.Fatalf("PadDisplayWidth() = %q, want unchanged %q", got, "hello")
	}
}

func TestFitDisplayWidth(t *testing.T) {
	got := FitDisplayWidth("hello world", 5)
	if ansi.StringWidth(got) != 5 {
		t.Fatalf("display width = %d, want 5", ansi.StringWidth(got))
	}
	got = FitDisplayWidth("hi", 5)
	if ansi.StringWidth(got) != 5 {
		t.Fatalf("display width = %d, want 5", ansi.StringWidth(got))
	}
}
