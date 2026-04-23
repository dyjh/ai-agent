package security

import (
	"encoding/json"
	"regexp"
	"strings"
)

var redactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key\s*[:=]\s*)([^\s"']+)`),
	regexp.MustCompile(`(?i)(bearer\s+)([a-z0-9\-\._~\+\/]+=*)`),
	regexp.MustCompile(`(?i)(token\s*[:=]\s*)([^\s"']+)`),
	regexp.MustCompile(`(?i)(password\s*[:=]\s*)([^\s"']+)`),
	regexp.MustCompile(`(?i)(secret\s*[:=]\s*)([^\s"']+)`),
	regexp.MustCompile(`(?is)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)(cookie\s*[:=]\s*)([^\r\n]+)`),
	regexp.MustCompile(`(?i)(session\s*[:=]\s*)([^\s"']+)`),
}

var sensitiveKeyPattern = regexp.MustCompile(`(?i)(api[_-]?key|authorization|bearer|cookie|password|private[_-]?key|secret|session|token)`)

// RedactString masks high-risk secrets before logging or persistence.
func RedactString(value string) string {
	result := value
	for _, pattern := range redactionPatterns {
		result = pattern.ReplaceAllString(result, `${1}[REDACTED]`)
	}
	return result
}

// RedactMap redacts a structured payload recursively through JSON round-tripping.
func RedactMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}

	redacted, ok := redactValue(input).(map[string]any)
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

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if sensitiveKeyPattern.MatchString(strings.TrimSpace(key)) {
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
