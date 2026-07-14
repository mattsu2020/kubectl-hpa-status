package rendutil

import "testing"

func TestMarkdownCell(t *testing.T) {
	got := MarkdownCell("a|b\r\nc")
	if got != `a\|b<br>c` {
		t.Fatalf("MarkdownCell() = %q", got)
	}
}

func TestHTMLEscapeExact(t *testing.T) {
	got := HTMLEscape(`<script data-x='1'>&"`)
	want := `&lt;script data-x=&#39;1&#39;&gt;&amp;&quot;`
	if got != want {
		t.Fatalf("HTMLEscape() = %q, want %q", got, want)
	}
}
