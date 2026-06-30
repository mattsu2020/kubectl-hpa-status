// Package output holds the pure output-routing helpers shared across cmd/
// subcommands. It is the extraction target for the cmd/ split tracked in
// ROADMAP.md: helpers move here first, cmd/ keeps a thin re-export facade so
// existing call sites compile, and callers migrate to this package directly
// over time (mirroring the internal/render and internal/kubeconv pattern).
package output

import (
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ShouldColorize reports true when the caller wants color and the writer is
// connected to a terminal. Lifted verbatim from cmd/output.go so the behavior
// stays identical while the symbol gains a stable import path.
func ShouldColorize(mode string, out io.Writer) bool {
	switch strings.ToLower(mode) {
	case "always", "true", "yes":
		return true
	case "never", "false", "no":
		return false
	case "", "auto":
		file, ok := out.(*os.File)
		return ok && term.IsTerminal(int(file.Fd()))
	default:
		return false
	}
}

// StdinIsTerminal reports whether the given reader is an interactive terminal.
// A nil reader returns false. Lifted verbatim from cmd/output.go.
func StdinIsTerminal(in io.Reader) bool {
	if in == nil {
		return false
	}
	file, ok := in.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

// Lang resolves the effective language code: an explicit lang wins, otherwise
// the legacy "ja" output format maps to ja, otherwise empty. Lifted verbatim
// from cmd/output.go.
func Lang(lang, output string) string {
	if lang != "" {
		return strings.ToLower(lang)
	}
	if strings.EqualFold(output, "ja") {
		return "ja"
	}
	return ""
}
