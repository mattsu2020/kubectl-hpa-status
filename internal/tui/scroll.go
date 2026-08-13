package tui

// scrollWindow returns the half-open range [start, end) of a window of
// visibleHeight rows over total entries, anchored at scrollPos. The window is
// clamped so it never starts before the first entry and never starts past the
// last full page, which keeps a full page on screen whenever entries remain.
// Used by views where scrollPos addresses the first visible row (history,
// replay).
func scrollWindow(scrollPos, total, visibleHeight int) (int, int) {
	start := min(max(scrollPos, 0), max(total-visibleHeight, 0))
	end := min(start+visibleHeight, total)
	return start, end
}

// cursorScrollWindow returns the half-open range [start, end) of a window of
// visibleHeight rows over total entries, scrolled by a cursor position: the
// window stays at the top while the cursor is inside it and scrolls only once
// the cursor moves below the visible area, keeping the cursor on the last
// visible row. Used by views where a selectable cursor drives scrolling
// (list, batch audit).
func cursorScrollWindow(cursor, total, visibleHeight int) (int, int) {
	start := 0
	if cursor >= visibleHeight {
		start = cursor - visibleHeight + 1
	}
	end := min(start+visibleHeight, total)
	return start, end
}
