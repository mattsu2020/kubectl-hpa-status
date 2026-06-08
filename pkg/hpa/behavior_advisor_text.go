package hpa

import (
	"fmt"
	"io"
)

// WriteBehaviorAdvisorText renders a BehaviorAdvisorResult as human-readable text.
func WriteBehaviorAdvisorText(w io.Writer, result *BehaviorAdvisorResult, provider LabelProvider) error {
	if result == nil {
		return nil
	}

	labels := resolveLabels(provider)

	if _, err := fmt.Fprintf(w, "%s:\n", labels.BehaviorAdvisor); err != nil {
		return err
	}

	if len(result.Findings) == 0 {
		if _, err := fmt.Fprintf(w, "  No behavior tuning recommendations.\n"); err != nil {
			return err
		}
		return nil
	}

	for _, f := range result.Findings {
		severity := string(f.Severity)
		if _, err := fmt.Fprintf(w, "  [%s] %s\n", severity, f.Message); err != nil {
			return err
		}
		if f.Current != "" {
			if _, err := fmt.Fprintf(w, "    Current: %s", f.Current); err != nil {
				return err
			}
			if f.Recommended != "" {
				if _, err := fmt.Fprintf(w, " → Recommended: %s", f.Recommended); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if f.Patch != "" {
			if _, err := fmt.Fprintf(w, "    Patch: %s\n", f.Patch); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(w, "  Summary: %s\n", result.Summary); err != nil {
		return err
	}

	return nil
}

// AppendBehaviorAdvisorText appends the behavior advisor section to a byte buffer
// (used by the main status text renderer).
func AppendBehaviorAdvisorText(out *[]byte, result *BehaviorAdvisorResult, labels labels) {
	if result == nil {
		return
	}

	*out = append(*out, '\n')
	*out = fmt.Appendf(*out, "%s:\n", labels.BehaviorAdvisor)

	if len(result.Findings) == 0 {
		*out = append(*out, "  No behavior tuning recommendations.\n"...)
		return
	}

	for _, f := range result.Findings {
		severity := string(f.Severity)
		*out = fmt.Appendf(*out, "  [%s] %s\n", severity, f.Message)
		if f.Current != "" {
			*out = fmt.Appendf(*out, "    Current: %s", f.Current)
			if f.Recommended != "" {
				*out = fmt.Appendf(*out, " → Recommended: %s", f.Recommended)
			}
			*out = append(*out, '\n')
		}
		if f.Patch != "" {
			*out = fmt.Appendf(*out, "    Patch: %s\n", f.Patch)
		}
		*out = append(*out, '\n')
	}

	*out = fmt.Appendf(*out, "  Summary: %s\n", result.Summary)
}
