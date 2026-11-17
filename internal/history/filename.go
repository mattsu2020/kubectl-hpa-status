package history

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// filePath maps a snapshot key onto its JSONL file. The readable prefix keeps
// the stream identifiable during debugging; the trailing identity hash is
// what actually separates streams, so any key difference — a long name that
// truncates, a second cluster, or a recreated HPA (new UID) — lands in its
// own file even when the sanitized prefixes collide.
func (s *HealthStore) filePath(key SnapshotKey) string {
	prefix := sanitizeFilename(key.Cluster) + "_" + sanitizeFilename(key.Namespace) + "_" + sanitizeFilename(key.Name)
	sum := sha256.Sum256([]byte(key.Cluster + "\x00" + key.Namespace + "\x00" + key.Name + "\x00" + key.UID))
	suffix := "_" + hex.EncodeToString(sum[:6]) + ".jsonl"
	prefix = truncateUTF8Bytes(prefix, maxHistoryFilenameLength-len(suffix))
	return filepath.Join(s.dir, prefix+suffix)
}

func truncateUTF8Bytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	for len(s) > maxBytes {
		_, size := utf8.DecodeLastRuneInString(s)
		if size <= 0 {
			break
		}
		s = s[:len(s)-size]
	}
	return s
}

// Keep enough headroom below the common NAME_MAX=255 byte limit.
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
	return truncateUTF8Bytes(s, maxFilenameSegmentLength)
}
