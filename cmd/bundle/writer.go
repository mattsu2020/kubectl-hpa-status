// Package bundle holds the renderer layer for the HPA investigation bundle
// (markdown sections, zip assembly, redaction). It is the extraction target
// for the bundle_* group tracked in ROADMAP.md: the renderers and their
// shared types moved here from cmd/, while the data-collection orchestrator
// (collectBundleData) stays in cmd/ because it depends on cmd-internal
// status-core helpers.
package bundle

import (
	"fmt"
	"io"
)

// Writer wraps an io.Writer and captures the first write error so that the
// bundle rendering functions can keep emitting subsequent sections (to
// preserve ordering/content) while still surfacing a failure to the caller.
//
// Write errors after the first are intentionally ignored: we only need to know
// whether the output succeeded at all, and continuing lets a partially-written
// stream (e.g. a real file that ran out of space) still contain as much of the
// intended report as possible.
type Writer struct {
	w   io.Writer
	err error
}

// Printf writes a formatted record. The error from the underlying writer is
// captured the first time it occurs and never overwrites a previously captured
// error.
func (b *Writer) Printf(format string, args ...any) {
	if b.err != nil {
		return
	}
	_, b.err = fmt.Fprintf(b.w, format, args...)
}

// Print writes its operands with no format directive.
func (b *Writer) Print(args ...any) {
	if b.err != nil {
		return
	}
	_, b.err = fmt.Fprint(b.w, args...)
}

// Println writes its operands followed by a newline.
func (b *Writer) Println(args ...any) {
	if b.err != nil {
		return
	}
	_, b.err = fmt.Fprintln(b.w, args...)
}

// Write writes raw bytes, capturing the first error.
func (b *Writer) Write(p []byte) {
	if b.err != nil {
		return
	}
	_, b.err = b.w.Write(p)
}

// Err returns the first write error captured, or nil if all writes succeeded.
func (b *Writer) Err() error { return b.err }
