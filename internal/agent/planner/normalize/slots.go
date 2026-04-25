package normalize

import (
	"regexp"
	"strings"
)

var (
	urlPattern    = regexp.MustCompile(`https?://[^\s"'<>` + "`" + `，。]+`)
	numberPattern = regexp.MustCompile(`[-+]?\d+(?:\.\d+)?`)
	toolIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)+$`)
)

// ExtractURLs returns URL-shaped tokens without interpreting the requested tool.
func ExtractURLs(value string) []string {
	matches := urlPattern.FindAllString(value, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, trimToken(match))
	}
	return uniq(out)
}

// ExtractNumbers returns numeric literals as structural slots.
func ExtractNumbers(value string) []string {
	return uniq(numberPattern.FindAllString(value, -1))
}

// ExtractExplicitToolID returns a tool id only when the user supplied it in a
// structured form such as tool_id: code.search_text.
func ExtractExplicitToolID(value string) string {
	fields := strings.Fields(value)
	for idx, field := range fields {
		raw := trimToken(field)
		lower := strings.ToLower(raw)
		for _, marker := range []string{"tool_id", "tool", "工具"} {
			for _, sep := range []string{":", "：", "="} {
				prefix := marker + sep
				if strings.HasPrefix(lower, prefix) {
					candidate := strings.TrimSpace(raw[len(prefix):])
					if isToolID(candidate) {
						return candidate
					}
				}
			}
			if lower == marker && idx+1 < len(fields) {
				candidate := trimToken(fields[idx+1])
				if isToolID(candidate) {
					return candidate
				}
			}
		}
	}
	return ""
}

func isToolID(value string) bool {
	return toolIDPattern.MatchString(strings.TrimSpace(value))
}
