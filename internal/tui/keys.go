package tui

import "charm.land/bubbles/v2/key"

// keyMap defines the keyboard shortcuts.
type keyMap struct {
	Up            key.Binding
	Down          key.Binding
	Enter         key.Binding
	Escape        key.Binding
	Quit          key.Binding
	Refresh       key.Binding
	Pause         key.Binding
	Filter        key.Binding
	Help          key.Binding
	Sort          key.Binding
	JumpProblem   key.Binding
	Metrics       key.Binding
	ToggleSelect  key.Binding
	SelectAll     key.Binding
	DeselectAll   key.Binding
	Simulate      key.Binding
	Fix           key.Binding
	Replay        key.Binding
	MetricMode    key.Binding
	TabField      key.Binding
	ShiftTabField key.Binding
	DryRun        key.Binding
	IntervalUp    key.Binding
	IntervalDown  key.Binding
	BatchAudit    key.Binding
	BatchApply    key.Binding
	History       key.Binding
	Hints         key.Binding
	Overview      key.Binding
}

func defaultKeys() keyMap {
	b := func(keys []string, help, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
	}
	return keyMap{
		Up: b([]string{"up", "k"}, "↑/k", "up"), Down: b([]string{"down", "j"}, "↓/j", "down"),
		Enter: b([]string{"enter"}, "enter", "detail"), Escape: b([]string{"esc"}, "esc", "back"),
		Quit: b([]string{"q", "ctrl+c"}, "q", "quit"), Refresh: b([]string{"r"}, "r", "refresh"),
		Pause: b([]string{"p"}, "p", "pause"), Filter: b([]string{"/"}, "/", "filter"), Help: b([]string{"?"}, "?", "help"),
		Sort: b([]string{"S"}, "S", "sort cycle"), JumpProblem: b([]string{"g"}, "g", "jump to problems"), Metrics: b([]string{"m"}, "m", "metrics detail"),
		ToggleSelect: b([]string{"space", " "}, "space", "toggle select"), SelectAll: b([]string{"a"}, "a", "select all"), DeselectAll: b([]string{"A"}, "A", "deselect all"),
		Simulate: b([]string{"s"}, "s", "simulate"), Fix: b([]string{"f"}, "f", "fix wizard"), Replay: b([]string{"T"}, "T", "replay timeline"),
		MetricMode: b([]string{"M"}, "M", "metric simulation"), TabField: b([]string{"tab"}, "tab", "next field"), ShiftTabField: b([]string{"shift+tab"}, "shift+tab", "previous field"),
		DryRun: b([]string{"d"}, "d", "server dry-run"), IntervalUp: b([]string{"+", "="}, "+/=", "faster refresh"), IntervalDown: b([]string{"-"}, "-", "slower refresh"),
		BatchAudit: b([]string{"B"}, "B", "batch auditor"), BatchApply: b([]string{"x"}, "x", "preview/confirm batch apply"), History: b([]string{"H"}, "H", "history/sparkline"),
		Hints: b([]string{"h"}, "h", "metric hints"), Overview: b([]string{"O"}, "O", "cluster overview"),
	}
}
