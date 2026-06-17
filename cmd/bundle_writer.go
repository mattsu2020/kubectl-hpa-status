package cmd

import (
	"fmt"
	"io"
)

// bundleWriter wraps an io.Writer and captures the first write error so that
// the bundle rendering functions can keep emitting subsequent sections (to
// preserve ordering/content) while still surfacing a failure to the caller.
//
// Write errors after the first are intentionally ignored: we only need to know
// whether the output succeeded at all, and continuing lets a partially-written
// stream (e.g. a real file that ran out of space) still contain as much of the
// intended report as possible.
type bundleWriter struct {
	w   io.Writer
	err error
}

// Printf writes a formatted record. The error from the underlying writer is
// captured the first time it occurs and never overwrites a previously captured
// error.
func (b *bundleWriter) Printf(format string, args ...any) {
	if b.err != nil {
		return
	}
	_, b.err = fmt.Fprintf(b.w, format, args...)
}

// Print writes its operands with no format directive.
func (b *bundleWriter) Print(args ...any) {
	if b.err != nil {
		return
	}
	_, b.err = fmt.Fprint(b.w, args...)
}

// Println writes its operands followed by a newline.
func (b *bundleWriter) Println(args ...any) {
	if b.err != nil {
		return
	}
	_, b.err = fmt.Fprintln(b.w, args...)
}

// Write writes raw bytes, capturing the first error.
func (b *bundleWriter) Write(p []byte) {
	if b.err != nil {
		return
	}
	_, b.err = b.w.Write(p)
}
