package hpa

import "testing"

func TestSanitizeTerminalText(t *testing.T) {
	t.Parallel()
	input := "normal\x1b]52;c;Y2xpcGJvYXJk\a\nforged"
	got := SanitizeTerminalText(input)
	want := "normal]52;c;Y2xpcGJvYXJk forged"
	if got != want {
		t.Fatalf("SanitizeTerminalText() = %q, want %q", got, want)
	}
}
