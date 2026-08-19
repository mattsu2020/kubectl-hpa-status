package tui

import tea "charm.land/bubbletea/v2"

func (m Model) handleSimFieldInput(msg tea.KeyMsg) (Model, bool) {
	if m.simState == nil || len(m.simState.fields) == 0 {
		return m, false
	}
	// bubble tea v2: match on the key's String() form for special keys and
	// inspect Key().Code/Text for printable input. Both backspace and ctrl+h
	// are destructive; everything else that carries printable text is filtered
	// to the numeric grammar the simulate fields accept.
	k := msg.Key()
	switch k.Code {
	case tea.KeyBackspace:
		field := &m.simState.fields[m.simState.focusIndex]
		if len(field.Value) > 0 {
			field.Value = field.Value[:len(field.Value)-1]
		}
		return m, true
	default:
		if len(k.Text) == 0 {
			return m, false
		}
		changed := false
		for _, r := range k.Text {
			if (r >= '0' && r <= '9') || r == '-' || r == '.' {
				field := &m.simState.fields[m.simState.focusIndex]
				field.Value += string(r)
				changed = true
			}
		}
		return m, changed
	}
}

func (m Model) handleFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filtering = false
		m.filter = m.filterInput.Value()
		m.filterInput.Blur()
		m.cursor = 0
		return m, nil
	case "esc":
		m.filtering = false
		m.filterInput.Blur()
		return m, nil
	default:
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		return m, cmd
	}
}
