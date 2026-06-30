package bundle

import "strings"

// RedactBytes applies redaction patterns to a byte slice. It replaces IP
// addresses, node names, pod UIDs, and other identifying data with generic
// placeholders. Lifted verbatim from cmd/snapshot.go.
func RedactBytes(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	s := string(data)

	// Redact IPv4 addresses.
	s = redactIPv4(s)

	// Redact IPv6 addresses.
	s = redactIPv6(s)

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

// redactIPv4 replaces IPv4 addresses with redacted placeholders.
func redactIPv4(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		// Try to match an IPv4 address pattern.
		start := i
		octets := 0
		j := i
		for octets < 4 && j < len(s) {
			num := 0
			digits := 0
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				num = num*10 + int(s[j]-'0')
				digits++
				j++
			}
			if digits == 0 || num > 255 {
				break
			}
			octets++
			if octets < 4 {
				if j >= len(s) || s[j] != '.' {
					break
				}
				j++
			}
		}
		if octets == 4 {
			result.WriteString("[REDACTED-IP]")
			i = j
		} else {
			result.WriteByte(s[start])
			i = start + 1
		}
	}
	return result.String()
}

// redactIPv6 replaces IPv6 addresses with redacted placeholders.
func redactIPv6(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == ':' && i > 0 && i < len(s)-1 && looksLikeIPv6(s, i) {
			// Find the end of the IPv6 address.
			j := i + 1
			for j < len(s) && isIPv6Char(s[j]) {
				j++
			}
			result.WriteString("[REDACTED-IP]")
			i = j
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

// looksLikeIPv6 checks if the position looks like part of an IPv6 address.
func looksLikeIPv6(s string, pos int) bool {
	colonCount := 0
	// Check backwards.
	for j := pos - 1; j >= 0 && isIPv6Char(s[j]); j-- {
		if s[j] == ':' {
			colonCount++
		}
	}
	// Check forwards.
	for j := pos + 1; j < len(s) && isIPv6Char(s[j]); j++ {
		if s[j] == ':' {
			colonCount++
		}
	}
	return colonCount >= 2
}

// isIPv6Char checks if a character is valid in an IPv6 address.
func isIPv6Char(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') || c == ':'
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
	idx := strings.Index(s, keyword)
	if idx < 0 {
		return s
	}
	// Find the end of the value (next space, newline, or end of string).
	start := idx + len(keyword)
	end := start
	for end < len(s) && s[end] != ' ' && s[end] != '\n' && s[end] != '\r' && s[end] != '\t' && s[end] != ',' && s[end] != '}' && s[end] != ']' {
		end++
	}
	if end == start {
		return s
	}
	return s[:start] + "[REDACTED-NODE]" + s[end:]
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
