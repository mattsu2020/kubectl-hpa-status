package rendutil

import (
	"strings"
	"testing"
)

func TestEscapeMarkdown(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"no pipes here", "no pipes here"},
		{"a|b", `a\|b`},
		{"|cell|", `\|cell\|`},
		{"||", `\|\|`},
	}
	for _, tc := range cases {
		if got := EscapeMarkdown(tc.in); got != tc.want {
			t.Fatalf("EscapeMarkdown(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHTMLEscape(t *testing.T) {
	in := `<a href="x">Tom & "Jerry" O'Neil</a>`
	got := HTMLEscape(in)
	for _, want := range []string{"&lt;", "&gt;", "&amp;", "&quot;", "&#39;"} {
		if !strings.Contains(got, want) {
			t.Fatalf("HTMLEscape(%q) = %q, missing %q", in, got, want)
		}
	}
	// Raw angle brackets must be gone.
	if strings.ContainsAny(got, "<>") {
		t.Fatalf("HTMLEscape still contains raw angle brackets: %q", got)
	}
	if got := HTMLEscape(""); got != "" {
		t.Fatalf("HTMLEscape(\"\") = %q, want empty", got)
	}
}

func TestHTMLHealthBadge(t *testing.T) {
	cases := []struct {
		health    string
		wantClass string
	}{
		{"OK", "health-ok"},
		{"ERROR", "health-error"},
		{"LIMITED", "health-limited"},
		{"STABILIZED", "health-stabilized"},
		{"WEIRD", "health-ok"}, // unknown falls back to default class
	}
	for _, tc := range cases {
		t.Run(tc.health, func(t *testing.T) {
			got := HTMLHealthBadge(tc.health, 42)
			if !strings.Contains(got, `class="`+tc.wantClass+`"`) {
				t.Fatalf("badge %q missing class %q: %s", tc.health, tc.wantClass, got)
			}
			if !strings.Contains(got, "42/100") {
				t.Fatalf("badge missing score: %s", got)
			}
		})
	}

	// Health string containing HTML-special chars must be escaped to entities.
	got := HTMLHealthBadge("OK<>&", 1)
	if !strings.Contains(got, "OK&lt;&gt;&amp;") {
		t.Fatalf("health value not escaped: %s", got)
	}
	if strings.Contains(got, "OK<") || strings.Contains(got, "OK>") {
		t.Fatalf("health value contains raw angle brackets: %s", got)
	}
}

func TestHTMLCSS_NonEmptyAndContainsRules(t *testing.T) {
	css := HTMLCSS()
	if css == "" {
		t.Fatalf("HTMLCSS() returned empty string")
	}
	for _, want := range []string{"body", "table", ".health-ok", ".health-error", "pre"} {
		if !strings.Contains(css, want) {
			t.Fatalf("HTMLCSS() missing %q", want)
		}
	}
	// Must be deterministic across calls.
	if css != HTMLCSS() {
		t.Fatalf("HTMLCSS() not deterministic")
	}
}
