package history

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
)

func (s *HealthStore) filePath(namespace, name string) string {
	// Sanitize to prevent path traversal.
	safeNS := sanitizeFilename(namespace)
	safeName := sanitizeFilename(name)
	filename := safeNS + "_" + safeName + ".jsonl"
	// sanitizeFilename truncates individual components. Add an identity hash
	// whenever that happens, even if the combined filename still fits, so two
	// names with the same prefix never share a history stream.
	truncated := len(namespace) > maxFilenameSegmentLength || len(name) > maxFilenameSegmentLength
	if truncated || len(filename) > maxHistoryFilenameLength {
		sum := sha256.Sum256([]byte(namespace + "\x00" + name))
		suffix := fmt.Sprintf("_%x.jsonl", sum[:8])
		prefixLength := maxHistoryFilenameLength - len(suffix)
		prefix := safeNS + "_" + safeName
		if len(prefix) > prefixLength {
			prefix = prefix[:prefixLength]
		}
		filename = prefix + suffix
	}
	return filepath.Join(s.dir, filename)
}

// Keep enough headroom below the common NAME_MAX=255 byte limit. A hash is
// appended whenever truncation is required so two long HPA names cannot share
// one history file.
const maxHistoryFilenameLength = 240

// maxFilenameSegmentLength bounds a single sanitized path segment so a
// pathologically long name cannot blow past filesystem name limits.
const maxFilenameSegmentLength = 200

// sanitizeFilename neutralizes anything that could escape the store
// directory: path separators, parent-directory references, control
// characters, empty inputs, and over-long names. Inputs come from the
// Kubernetes API and are DNS-constrained in practice, so this is
// defense-in-depth.
func sanitizeFilename(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	s = strings.ReplaceAll(b.String(), "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	if s == "" || s == "." || strings.HasPrefix(s, "..") {
		s = "_" + s
	}
	if len(s) > maxFilenameSegmentLength {
		s = s[:maxFilenameSegmentLength]
	}
	return s
}
