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

// ShouldColorize reports true when the caller wants color and the underlying
// writer is connected to a terminal. Rendering layers may wrap writers to
// track write failures, so auto detection follows explicit Unwrap methods.
func ShouldColorize(mode string, out io.Writer) bool {
	switch strings.ToLower(mode) {
	case "always", "true", "yes":
		return true
	case "never", "false", "no":
		return false
	case "", "auto":
		out = unwrapWriter(out)
		file, ok := out.(*os.File)
		return ok && term.IsTerminal(int(file.Fd()))
	default:
		return false
	}
}

type writerUnwrapper interface {
	Unwrap() io.Writer
}

func unwrapWriter(out io.Writer) io.Writer {
	// Bound traversal so a malformed wrapper cycle cannot hang output.
	for range 32 {
		wrapper, ok := out.(writerUnwrapper)
		if !ok {
			return out
		}
		next := wrapper.Unwrap()
		if next == nil {
			return out
		}
		out = next
	}
	return out
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
