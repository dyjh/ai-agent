package fastpath

import (
	"strings"

	"local-agent/internal/agent/planner/normalize"
)

func workspaceOrDot(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "."
	}
	return workspace
}

func firstQuoted(req normalize.NormalizedRequest) string {
	for _, item := range req.QuotedTexts {
		item = strings.TrimSpace(item)
		if item != "" {
			return item
		}
	}
	return ""
}

func signalSet(signals []string) map[string]bool {
	out := map[string]bool{}
	for _, signal := range signals {
		out[strings.TrimSpace(signal)] = true
	}
	return out
}

func projectKey(message string) string {
	return valueAfterMarker(message, []string{"project:", "project：", "project_key:", "project_key：", "项目:", "项目："})
}

func memoryID(message string) string {
	fields := strings.Fields(message)
	for i := len(fields) - 1; i >= 0; i-- {
		token := strings.Trim(fields[i], "`'\"，,。;；")
		if strings.HasPrefix(token, "mem_") || strings.HasPrefix(token, "memory_") {
			return token
		}
	}
	if quoted := quotedFromRaw(message); quoted != "" {
		return quoted
	}
	if len(fields) > 0 {
		return strings.Trim(fields[len(fields)-1], "`'\"，,。;；")
	}
	return ""
}

func valueAfterMarker(message string, markers []string) string {
	lower := strings.ToLower(message)
	for _, marker := range markers {
		idx := strings.Index(lower, strings.ToLower(marker))
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(message[idx+len(marker):])
		fields := strings.Fields(rest)
		if len(fields) > 0 {
			return strings.Trim(fields[0], "`'\"，,。;；")
		}
	}
	return ""
}

func quotedFromRaw(message string) string {
	items := normalize.ExtractQuotedTexts(message)
	if len(items) > 0 {
		return items[0]
	}
	return ""
}
