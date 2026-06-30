package output

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestShouldColorize(t *testing.T) {
	tests := []struct {
		name string
		mode string
		out  io.Writer
		want bool
	}{
		{name: "always forces color on any writer", mode: "always", out: &bytes.Buffer{}, want: true},
		{name: "true forces color", mode: "true", out: &bytes.Buffer{}, want: true},
		{name: "yes forces color", mode: "yes", out: &bytes.Buffer{}, want: true},
		{name: "ALWAYS is case-insensitive", mode: "ALWAYS", out: &bytes.Buffer{}, want: true},
		{name: "never disables color on terminal", mode: "never", out: os.Stdout, want: false},
		{name: "false disables color", mode: "false", out: &bytes.Buffer{}, want: false},
		{name: "no disables color", mode: "no", out: &bytes.Buffer{}, want: false},
		{name: "auto on non-terminal writer is false", mode: "auto", out: &bytes.Buffer{}, want: false},
		{name: "empty mode behaves like auto", mode: "", out: &bytes.Buffer{}, want: false},
		{name: "unknown mode defaults to false", mode: "bogus", out: &bytes.Buffer{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldColorize(tt.mode, tt.out); got != tt.want {
				t.Fatalf("ShouldColorize(%q) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

func TestStdinIsTerminal(t *testing.T) {
	t.Run("nil reader returns false", func(t *testing.T) {
		if StdinIsTerminal(nil) {
			t.Fatal("nil reader must not be a terminal")
		}
	})
	t.Run("non-file reader returns false", func(t *testing.T) {
		if StdinIsTerminal(&bytes.Buffer{}) {
			t.Fatal("bytes.Buffer must not be a terminal")
		}
	})
	// We cannot reliably assert true here without a pty, but os.Stdin's
	// result is environment-dependent; just ensure no panic.
	_ = StdinIsTerminal(os.Stdin)
}

func TestLang(t *testing.T) {
	tests := []struct {
		lang, output, want string
	}{
		{"ja", "", "ja"},   // explicit lang wins
		{"EN", "", "en"},   // lang lower-cased
		{"", "ja", "ja"},   // legacy ja output format
		{"", "JA", "ja"},   // legacy ja format case-insensitive
		{"", "json", ""},   // non-ja output yields empty
		{"", "", ""},       // nothing set yields empty
		{"en", "ja", "en"}, // explicit lang overrides output-based ja
	}
	for _, tt := range tests {
		if got := Lang(tt.lang, tt.output); got != tt.want {
			t.Fatalf("Lang(%q, %q) = %q, want %q", tt.lang, tt.output, got, tt.want)
		}
	}
}
