package rendutil

import (
	"strings"
	"testing"
)

func TestWrapLines(t *testing.T) {
	if got := WrapLines("", 40); got != nil {
		t.Fatalf("WrapLines(\"\") = %v, want nil", got)
	}
	if got := WrapLines("   ", 40); got != nil {
		t.Fatalf("WrapLines(whitespace) = %v, want nil", got)
	}

	got := WrapLines("no wrap needed", 100)
	if len(got) != 1 || got[0] != "no wrap needed" {
		t.Fatalf("WrapLines(short) = %v, want single unwrapped line", got)
	}

	got = WrapLines("one two three four", 7)
	if len(got) < 2 {
		t.Fatalf("WrapLines(long) = %v, want multiple lines for width 7", got)
	}
	for _, line := range got {
		if len(line) > 7 && !strings.Contains(line, " ") {
			t.Errorf("line %q exceeds width 7 with no word boundary to break at", line)
		}
	}
	if strings.Join(got, " ") != "one two three four" {
		t.Fatalf("WrapLines lost or reordered words: %v", got)
	}

	// maxLen=0 still returns each word on its own line rather than looping forever.
	got = WrapLines("anything", 0)
	if len(got) != 1 || got[0] != "anything" {
		t.Fatalf("WrapLines(maxLen=0) = %v, want single word", got)
	}
}
