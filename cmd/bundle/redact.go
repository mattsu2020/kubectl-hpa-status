package bundle

import (
	"encoding/json"
	"net"
	"strings"

	"sigs.k8s.io/yaml"
)

// RedactBytes applies redaction patterns to a byte slice. It replaces IP
// addresses, node names, pod UIDs, and other identifying data with generic
// placeholders. Lifted verbatim from cmd/snapshot.go.
func RedactBytes(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	s := string(data)

	// Parse complete address-shaped tokens so IPv6 prefixes cannot leak and
	// invalid dotted versions are not partially mistaken for IPv4 addresses.
	s = redactIPAddresses(s)

	// Redact node names (heuristic: alphanumeric strings after "node:" or "Node:").
	s = redactNodeNames(s)

	// Redact pod UIDs (UUIDs).
	s = redactUIDs(s)

	// Redact hostnames.
	s = redactHostnames(s)

	return []byte(s)
}

// RedactString applies common redaction patterns to a single string. Lifted
// verbatim from cmd/bundle.go.
func RedactString(s string) string {
	return string(RedactBytes([]byte(s)))
}

// RedactStructuredBytes removes values commonly carrying credentials or
// environment-specific identifiers from YAML/JSON Kubernetes objects, then
// applies the textual address/hostname redactor. It intentionally preserves
// keys and object shape so a support bundle remains useful for diagnosis.
func RedactStructuredBytes(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	var value any
	if err := yaml.Unmarshal(data, &value); err != nil {
		return RedactBytes(data)
	}
	redactStructuredValue(value, "")

	var (
		out []byte
		err error
	)
	if json.Valid(data) {
		out, err = json.MarshalIndent(value, "", "  ")
	} else {
		out, err = yaml.Marshal(value)
	}
	if err != nil {
		return RedactBytes(data)
	}
	return RedactBytes(out)
}

func redactStructuredValue(value any, parentKey string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", ""), "_", ""))
			if shouldRedactStructuredField(normalized, parentKey) {
				typed[key] = redactStructuredFieldValue(child)
				continue
			}
			redactStructuredValue(child, normalized)
		}
	case []any:
		for _, child := range typed {
			redactStructuredValue(child, parentKey)
		}
	}
}

// redactedParentKeys marks containers whose every child value is sensitive.
var redactedParentKeys = map[string]bool{
	"annotations": true,
	"labels":      true,
	"data":        true,
	"stringdata":  true,
}

// redactedFieldKeys marks field names that are sensitive regardless of parent.
var redactedFieldKeys = map[string]bool{
	"data":          true,
	"stringdata":    true,
	"authorization": true,
	"cookie":        true,
}

// redactedKeyMarkers marks substrings that flag a field name as sensitive.
var redactedKeyMarkers = []string{
	"password", "passwd", "token", "secret", "apikey",
	"clientkey", "privatekey", "credential",
}

func shouldRedactStructuredField(key, parentKey string) bool {
	if redactedParentKeys[parentKey] || redactedFieldKeys[key] {
		return true
	}
	if key == "value" && parentKey == "env" {
		return true
	}
	if key == "name" && (strings.Contains(parentKey, "secret") || strings.Contains(parentKey, "configmap")) {
		return true
	}
	for _, marker := range redactedKeyMarkers {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func redactStructuredFieldValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key := range typed {
			out[key] = "[REDACTED]"
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i := range out {
			out[i] = "[REDACTED]"
		}
		return out
	default:
		return "[REDACTED]"
	}
}

// redactIPAddresses replaces complete IPv4 and IPv6 tokens. net.ParseIP is
// deliberately used instead of a permissive regular expression: support
// bundles often contain versions and quantities that look address-like.
func redactIPAddresses(s string) string {
	var result strings.Builder
	for i := 0; i < len(s); {
		if !isIPTokenChar(s[i]) {
			result.WriteByte(s[i])
			i++
			continue
		}
		j := i
		for j < len(s) && isIPTokenChar(s[j]) {
			j++
		}
		candidate := s[i:j]
		if net.ParseIP(candidate) != nil {
			result.WriteString("[REDACTED-IP]")
		} else {
			result.WriteString(candidate)
		}
		i = j
	}
	return result.String()
}

func isIPTokenChar(c byte) bool {
	return (c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'f') ||
		(c >= 'A' && c <= 'F') || c == ':' || c == '.'
}

// redactNodeNames replaces node names after "node:" or "Node:" keywords.
func redactNodeNames(s string) string {
	replacements := []string{
		"node: ", "node: [REDACTED-NODE]",
		"Node: ", "Node: [REDACTED-NODE]",
		"node=", "node=[REDACTED-NODE]",
		"NodeName: ", "NodeName: [REDACTED-NODE]",
	}
	for i := 0; i < len(replacements); i += 2 {
		s = replaceAfterKeyword(s, replacements[i], replacements[i+1])
	}
	return s
}

// replaceAfterKeyword replaces the value after a keyword.
func replaceAfterKeyword(s, keyword, _ string) string {
	var result strings.Builder
	remaining := s
	for {
		idx := strings.Index(remaining, keyword)
		if idx < 0 {
			result.WriteString(remaining)
			return result.String()
		}
		start := idx + len(keyword)
		end := start
		for end < len(remaining) && remaining[end] != ' ' && remaining[end] != '\n' && remaining[end] != '\r' && remaining[end] != '\t' && remaining[end] != ',' && remaining[end] != '}' && remaining[end] != ']' {
			end++
		}
		result.WriteString(remaining[:start])
		if end == start {
			// Empty value: retain the keyword and continue after it to avoid an
			// infinite loop.
			remaining = remaining[start:]
			continue
		}
		result.WriteString("[REDACTED-NODE]")
		remaining = remaining[end:]
	}
}

// redactUIDs replaces UUID-style UIDs.
func redactUIDs(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if i+36 <= len(s) && isUUID(s[i:i+36]) {
			result.WriteString("[REDACTED-UID]")
			i += 36
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

// isUUID checks if a string matches the UUID format (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return false
			}
		}
	}
	return true
}

// redactHostnames replaces hostname-like patterns (e.g., ip-10-0-1-23.ec2.internal).
func redactHostnames(s string) string {
	// Common cloud hostname patterns.
	patterns := []string{
		"ip-",
		"ec2-",
		"gke-",
		"aks-",
		"eks-",
	}
	var result strings.Builder
	i := 0
	for i < len(s) {
		matched := false
		for _, p := range patterns {
			if i+len(p) <= len(s) && s[i:i+len(p)] == p {
				// Find the end of the hostname.
				j := i
				for j < len(s) && s[j] != ' ' && s[j] != '\n' && s[j] != '\r' && s[j] != '\t' && s[j] != ',' && s[j] != '"' && s[j] != '\'' {
					j++
				}
				result.WriteString("[REDACTED-HOSTNAME]")
				i = j
				matched = true
				break
			}
		}
		if !matched {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}
