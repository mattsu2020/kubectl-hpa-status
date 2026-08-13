package tui

import "testing"

func TestScrollWindow(t *testing.T) {
	tests := []struct {
		name                     string
		scrollPos, total, height int
		wantStart, wantEnd       int
	}{
		{name: "middle scroll anchors window at pos", scrollPos: 5, total: 100, height: 10, wantStart: 5, wantEnd: 15},
		{name: "negative pos clamps to zero", scrollPos: -3, total: 100, height: 10, wantStart: 0, wantEnd: 10},
		{name: "pos exactly at last page start", scrollPos: 90, total: 100, height: 10, wantStart: 90, wantEnd: 100},
		{name: "pos past last page clamps to last full page", scrollPos: 95, total: 100, height: 10, wantStart: 90, wantEnd: 100},
		{name: "pos at last entry clamps to last full page", scrollPos: 99, total: 100, height: 10, wantStart: 90, wantEnd: 100},
		{name: "total shorter than window shows everything", scrollPos: 0, total: 3, height: 10, wantStart: 0, wantEnd: 3},
		{name: "pos beyond total clamps to last full page", scrollPos: 50, total: 3, height: 10, wantStart: 0, wantEnd: 3},
		{name: "zero total returns empty range", scrollPos: 0, total: 0, height: 10, wantStart: 0, wantEnd: 0},
		{name: "exact page fit starts at zero", scrollPos: 0, total: 10, height: 10, wantStart: 0, wantEnd: 10},
		{name: "zero height returns empty range", scrollPos: 5, total: 100, height: 0, wantStart: 5, wantEnd: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStart, gotEnd := scrollWindow(tt.scrollPos, tt.total, tt.height)
			if gotStart != tt.wantStart || gotEnd != tt.wantEnd {
				t.Fatalf("scrollWindow(%d, %d, %d) = (%d, %d), want (%d, %d)",
					tt.scrollPos, tt.total, tt.height, gotStart, gotEnd, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestCursorScrollWindow(t *testing.T) {
	tests := []struct {
		name                  string
		cursor, total, height int
		wantStart, wantEnd    int
	}{
		{name: "cursor inside first page keeps window at top", cursor: 2, total: 100, height: 10, wantStart: 0, wantEnd: 10},
		{name: "cursor on last row of first page stays at top", cursor: 9, total: 100, height: 10, wantStart: 0, wantEnd: 10},
		{name: "cursor below window scrolls keeping cursor last", cursor: 10, total: 100, height: 10, wantStart: 1, wantEnd: 11},
		{name: "cursor at end clamps window to tail", cursor: 99, total: 100, height: 10, wantStart: 90, wantEnd: 100},
		{name: "negative cursor keeps window at top", cursor: -1, total: 100, height: 10, wantStart: 0, wantEnd: 10},
		{name: "total shorter than window shows everything", cursor: 1, total: 3, height: 10, wantStart: 0, wantEnd: 3},
		{name: "cursor beyond short total still shows everything", cursor: 8, total: 3, height: 10, wantStart: 0, wantEnd: 3},
		{name: "zero total returns empty range", cursor: 0, total: 0, height: 10, wantStart: 0, wantEnd: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStart, gotEnd := cursorScrollWindow(tt.cursor, tt.total, tt.height)
			if gotStart != tt.wantStart || gotEnd != tt.wantEnd {
				t.Fatalf("cursorScrollWindow(%d, %d, %d) = (%d, %d), want (%d, %d)",
					tt.cursor, tt.total, tt.height, gotStart, gotEnd, tt.wantStart, tt.wantEnd)
			}
		})
	}
}
