package normalize

import "strings"

// ExtractWorkspace returns the explicit workspace path when present.
func ExtractWorkspace(value string) string {
	lower := strings.ToLower(value)
	for _, marker := range []string{"workspace:", "workspace：", "workspace=", "工作区:", "工作区：", "工作区="} {
		idx := strings.Index(lower, strings.ToLower(marker))
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(value[idx+len(marker):])
		if rest == "" {
			break
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			break
		}
		workspace := trimToken(fields[0])
		if workspace != "" {
			return workspace
		}
	}
	fields := strings.Fields(value)
	for idx, field := range fields {
		lowerField := strings.ToLower(trimToken(field))
		if (lowerField == "workspace" || lowerField == "工作区") && idx+1 < len(fields) {
			workspace := trimToken(fields[idx+1])
			if workspace != "" {
				return workspace
			}
		}
	}
	return ""
}

func trimToken(value string) string {
	return strings.Trim(value, "`'\"“”‘’「」『』，,。;；:：")
}
