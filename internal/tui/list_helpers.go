package tui

import (
	"slices"
	"strings"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

// filteredItems returns items matching the current filter text.
// Uses a value receiver since it does not mutate state.
func (m Model) filteredItems() []hpaanalysis.ListItem {
	if m.filter == "" {
		return m.items
	}
	filtered := make([]hpaanalysis.ListItem, 0, len(m.items))
	for _, item := range m.items {
		if containsIgnoreCase(item.Name, m.filter) ||
			containsIgnoreCase(item.Namespace, m.filter) ||
			containsIgnoreCase(item.Health, m.filter) ||
			containsIgnoreCase(item.Issue, m.filter) ||
			containsIgnoreCase(item.Summary, m.filter) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func containsIgnoreCase(s, substr string) bool {
	return len(substr) == 0 ||
		(len(s) >= len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	sl := len(s)
	subl := len(substr)
	for i := 0; i <= sl-subl; i++ {
		match := true
		for j := 0; j < subl; j++ {
			sc := s[i+j]
			bc := substr[j]
			if sc >= 'A' && sc <= 'Z' {
				sc += 32
			}
			if bc >= 'A' && bc <= 'Z' {
				bc += 32
			}
			if sc != bc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// sortItems sorts the item list by the current sort field.
func (m *Model) sortItems() {
	if m.sortField == "" {
		return
	}
	slices.SortStableFunc(m.items, func(a, b hpaanalysis.ListItem) int {
		var cmp int
		switch m.sortField {
		case "name":
			cmp = strings.Compare(a.Name, b.Name)
		case "namespace":
			cmp = strings.Compare(a.Namespace, b.Namespace)
		case "health-score":
			cmp = cmpInt(a.HealthScore, b.HealthScore)
		case "issue":
			cmp = strings.Compare(a.Issue, b.Issue)
		}
		if m.sortDescending {
			return -cmp
		}
		return cmp
	})
}

func (m *Model) focusInitialItem() {
	if m.opts.InitialName == "" {
		return
	}
	for i, item := range m.items {
		if item.Name != m.opts.InitialName {
			continue
		}
		if m.opts.InitialNS != "" && item.Namespace != m.opts.InitialNS {
			continue
		}
		m.cursor = i
		if m.opts.StartInDetail {
			m.viewMode = detailView
		}
		return
	}
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
