package normalize

import "strings"

// ExtractQuotedTexts extracts text inside common English and Chinese quotes.
func ExtractQuotedTexts(value string) []string {
	type pair struct {
		open  string
		close string
	}
	pairs := []pair{
		{"`", "`"},
		{"\"", "\""},
		{"'", "'"},
		{"“", "”"},
		{"‘", "’"},
		{"「", "」"},
		{"『", "』"},
		{"《", "》"},
	}
	out := []string{}
	for _, p := range pairs {
		rest := value
		for {
			start := strings.Index(rest, p.open)
			if start < 0 {
				break
			}
			afterOpen := rest[start+len(p.open):]
			end := strings.Index(afterOpen, p.close)
			if end < 0 {
				break
			}
			text := strings.TrimSpace(afterOpen[:end])
			if text != "" {
				out = append(out, text)
			}
			rest = afterOpen[end+len(p.close):]
		}
	}
	return uniq(out)
}
