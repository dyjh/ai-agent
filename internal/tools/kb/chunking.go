package kb

import "strings"

// SplitMarkdownChunks splits content on blank lines for MVP indexing.
func SplitMarkdownChunks(content string) []string {
	parts := strings.Split(content, "\n\n")
	chunks := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		chunks = append(chunks, trimmed)
	}
	return chunks
}
