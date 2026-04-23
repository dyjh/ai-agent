package security

import (
	"encoding/json"
	"regexp"
)

var redactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key\s*[:=]\s*)([^\s"']+)`),
	regexp.MustCompile(`(?i)(bearer\s+)([a-z0-9\-\._~\+\/]+=*)`),
	regexp.MustCompile(`(?i)(password\s*[:=]\s*)([^\s"']+)`),
	regexp.MustCompile(`(?i)(secret\s*[:=]\s*)([^\s"']+)`),
	regexp.MustCompile(`(?is)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)(cookie\s*[:=]\s*)([^\r\n]+)`),
	regexp.MustCompile(`(?i)(session\s*[:=]\s*)([^\s"']+)`),
}

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

	raw, err := json.Marshal(input)
	if err != nil {
		return input
	}

	redacted := RedactString(string(raw))

	var out map[string]any
	if err := json.Unmarshal([]byte(redacted), &out); err != nil {
		return input
	}

	return out
}
