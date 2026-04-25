package security

import (
	"encoding/json"
	"regexp"
	"strings"
)

var redactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key\s*[:=]\s*)([^\s"']+)`),
	regexp.MustCompile(`(?i)(bearer\s+)([a-z0-9\-\._~\+\/=]{12,})`),
	regexp.MustCompile(`(?i)(token\s*[:=]\s*)([^\s"']+)`),
	regexp.MustCompile(`(?i)(password\s*[:=]\s*)([^\s"']+)`),
	regexp.MustCompile(`(?i)(secret\s*[:=]\s*)([^\s"']+)`),
	regexp.MustCompile(`(?i)(postgres(?:ql)?://[^:\s/]+:)([^@\s]+)`),
	regexp.MustCompile(`(?i)(mysql://[^:\s/]+:)([^@\s]+)`),
	regexp.MustCompile(`(?is)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)(cookie\s*[:=]\s*)([^\r\n]+)`),
	regexp.MustCompile(`(?i)(session\s*[:=]\s*)([^\s"']+)`),
}

var sensitiveKeyPattern = regexp.MustCompile(`(?i)(api[_-]?key|authorization|bearer|cookie|credential|identity[_-]?file|key[_-]?path|kubeconfig|password|private[_-]?key|secret|session|token)`)

// RedactString masks high-risk secrets before logging or persistence.
func RedactString(value string) string {
	result := RedactText(value)
	for _, pattern := range redactionPatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			parts := pattern.FindStringSubmatch(match)
			if len(parts) >= 3 {
				return parts[1] + "[REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return result
}

// ContainsSensitiveString reports whether RedactString would mask sensitive data.
func ContainsSensitiveString(value string) bool {
	if value == "" {
		return false
	}
	return ScanText(value).HasSecret || RedactString(value) != value
}

// RedactMap redacts a structured payload recursively through JSON round-tripping.
func RedactMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}

	redacted, ok := RedactAny(input).(map[string]any)
	if ok {
		return redacted
	}

	raw, err := json.Marshal(input)
	if err != nil {
		return input
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(RedactString(string(raw))), &out); err != nil {
		return input
	}
	return out
}

// RedactAny redacts JSON-like values. Structs are handled through JSON round-tripping.
func RedactAny(input any) any {
	switch typed := input.(type) {
	case nil:
		return nil
	case map[string]any, []any, string:
		return redactValue(typed)
	default:
		raw, err := json.Marshal(input)
		if err != nil {
			return input
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return input
		}
		return redactValue(decoded)
	}
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if IsSensitiveKey(strings.TrimSpace(key)) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = redactValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for idx, item := range typed {
			out[idx] = redactValue(item)
		}
		return out
	case string:
		return RedactString(typed)
	default:
		return typed
	}
}
