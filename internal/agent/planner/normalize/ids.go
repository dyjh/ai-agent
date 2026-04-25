package normalize

import "strings"

// ExtractID extracts a token after a known id marker.
func ExtractID(value string, markers []string) string {
	fields := strings.Fields(value)
	for idx, field := range fields {
		lower := strings.ToLower(trimToken(field))
		for _, marker := range markers {
			marker = strings.ToLower(strings.TrimSpace(marker))
			if lower == marker && idx+1 < len(fields) {
				return trimToken(fields[idx+1])
			}
			for _, sep := range []string{":", "：", "="} {
				prefix := marker + sep
				if strings.HasPrefix(lower, prefix) {
					return strings.TrimPrefix(lower, prefix)
				}
			}
		}
	}
	return ""
}
