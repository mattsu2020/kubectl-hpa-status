package rendutil

import "strings"

// WrapLines splits text into lines of at most maxLen characters, breaking at
// word boundaries when possible. Shared by blocker, capacity-plan,
// gitops-review, and rollout text renderers.
func WrapLines(text string, maxLen int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	var lines []string
	var current strings.Builder

	for _, word := range words {
		if current.Len() > 0 && current.Len()+1+len(word) > maxLen {
			lines = append(lines, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(word)
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}
