package kb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// ParserRegistry selects the first parser that supports a source document.
type ParserRegistry struct {
	parsers []DocumentParser
}

// NewDefaultParserRegistry returns the built-in parser pipeline.
func NewDefaultParserRegistry() ParserRegistry {
	return ParserRegistry{parsers: []DocumentParser{
		MarkdownParser{},
		TextParser{},
		JSONParser{},
		HTMLParser{},
		PDFParser{},
		OfficeParser{},
	}}
}

// Parse normalizes one source document into text and sections.
func (r ParserRegistry) Parse(ctx context.Context, input ParseInput) (*ParsedDocument, error) {
	for _, parser := range r.parsers {
		if parser.Supports(input.Source, input.Filename, input.ContentType) {
			return parser.Parse(ctx, input)
		}
	}
	return nil, fmt.Errorf("no parser supports %s", input.Filename)
}

// MarkdownParser parses Markdown files into heading sections.
type MarkdownParser struct{}

func (MarkdownParser) Supports(_ KnowledgeSource, filename string, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".md" || ext == ".markdown" || strings.Contains(strings.ToLower(contentType), "markdown")
}

func (MarkdownParser) Parse(_ context.Context, input ParseInput) (*ParsedDocument, error) {
	text := strings.TrimSpace(string(input.Content))
	if text == "" {
		return nil, fmt.Errorf("markdown document is empty")
	}
	return parsedTextWithHeadings(text), nil
}

// TextParser parses plain text.
type TextParser struct{}

func (TextParser) Supports(_ KnowledgeSource, filename string, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	ct := strings.ToLower(contentType)
	return ext == ".txt" || strings.HasPrefix(ct, "text/plain")
}

func (TextParser) Parse(_ context.Context, input ParseInput) (*ParsedDocument, error) {
	text := strings.TrimSpace(string(input.Content))
	if text == "" {
		return nil, fmt.Errorf("text document is empty")
	}
	return &ParsedDocument{Text: text, Sections: []ParsedSection{{Text: text}}}, nil
}

// JSONParser parses JSON into stable pretty text.
type JSONParser struct{}

func (JSONParser) Supports(_ KnowledgeSource, filename string, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".json" || strings.Contains(strings.ToLower(contentType), "application/json")
}

func (JSONParser) Parse(_ context.Context, input ParseInput) (*ParsedDocument, error) {
	var value any
	if err := json.Unmarshal(input.Content, &value); err != nil {
		return nil, fmt.Errorf("parse json document: %w", err)
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(raw))
	return &ParsedDocument{Text: text, Sections: []ParsedSection{{Text: text}}, Metadata: map[string]any{"format": "json"}}, nil
}

// HTMLParser extracts visible text from HTML and removes script/style blocks.
type HTMLParser struct{}

func (HTMLParser) Supports(source KnowledgeSource, filename string, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	ct := strings.ToLower(contentType)
	return source.Type == KnowledgeSourceURL || ext == ".html" || ext == ".htm" || strings.Contains(ct, "text/html")
}

func (HTMLParser) Parse(_ context.Context, input ParseInput) (*ParsedDocument, error) {
	raw := string(input.Content)
	raw = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`).ReplaceAllString(raw, " ")
	raw = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`).ReplaceAllString(raw, " ")
	title := ""
	if match := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`).FindStringSubmatch(raw); len(match) == 2 {
		title = cleanWhitespace(stripTags(match[1]))
	}
	text := stripTags(raw)
	text = html.UnescapeString(text)
	text = cleanWhitespace(text)
	if text == "" {
		return nil, fmt.Errorf("html document has no visible text")
	}
	return &ParsedDocument{Title: title, Text: text, Sections: []ParsedSection{{Heading: title, Text: text}}, Metadata: map[string]any{"format": "html"}}, nil
}

// PDFParser performs best-effort extraction for simple uncompressed PDFs.
type PDFParser struct{}

func (PDFParser) Supports(_ KnowledgeSource, filename string, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".pdf" || strings.Contains(strings.ToLower(contentType), "application/pdf")
}

func (PDFParser) Parse(_ context.Context, input ParseInput) (*ParsedDocument, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(input.Content), []byte("%PDF")) {
		return nil, fmt.Errorf("pdf document does not start with %%PDF")
	}
	matches := regexp.MustCompile(`\(([^()]*)\)`).FindAllSubmatch(input.Content, -1)
	parts := make([]string, 0, len(matches))
	for _, match := range matches {
		text := cleanPDFLiteral(string(match[1]))
		if len(text) < 2 || !hasLetterOrDigit(text) {
			continue
		}
		parts = append(parts, text)
	}
	text := cleanWhitespace(strings.Join(parts, " "))
	if text == "" {
		return nil, fmt.Errorf("pdf text extraction found no uncompressed text; scanned or compressed PDF requires an external parser")
	}
	return &ParsedDocument{Text: text, Sections: []ParsedSection{{Text: text}}, Metadata: map[string]any{"format": "pdf", "parser": "best_effort"}}, nil
}

// OfficeParser is a stable skeleton for Office documents.
type OfficeParser struct{}

func (OfficeParser) Supports(_ KnowledgeSource, filename string, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	ct := strings.ToLower(contentType)
	switch ext {
	case ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx":
		return true
	default:
		return strings.Contains(ct, "officedocument") || strings.Contains(ct, "msword") || strings.Contains(ct, "ms-excel") || strings.Contains(ct, "ms-powerpoint")
	}
}

func (OfficeParser) Parse(_ context.Context, input ParseInput) (*ParsedDocument, error) {
	return nil, fmt.Errorf("office document parsing is not supported by the built-in parser: %s", input.Filename)
}

func parsedTextWithHeadings(text string) *ParsedDocument {
	lines := strings.Split(text, "\n")
	title := ""
	sections := []ParsedSection{}
	currentHeading := ""
	var current strings.Builder
	offset := 0
	sectionOffset := 0
	flush := func() {
		body := strings.TrimSpace(current.String())
		if body == "" && currentHeading == "" {
			return
		}
		sections = append(sections, ParsedSection{Heading: currentHeading, Text: body, Offset: sectionOffset})
		current.Reset()
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if heading != "" {
				if title == "" {
					title = heading
				}
				flush()
				currentHeading = heading
				sectionOffset = offset
				offset += len(line) + 1
				continue
			}
		}
		current.WriteString(line)
		current.WriteByte('\n')
		offset += len(line) + 1
	}
	flush()
	if len(sections) == 0 {
		sections = append(sections, ParsedSection{Text: text})
	}
	return &ParsedDocument{Title: title, Text: text, Sections: sections, Metadata: map[string]any{"format": "markdown"}}
}

func stripTags(value string) string {
	return regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(value, " ")
}

func cleanWhitespace(value string) string {
	fields := strings.Fields(value)
	return strings.TrimSpace(strings.Join(fields, " "))
}

func cleanPDFLiteral(value string) string {
	replacer := strings.NewReplacer(`\(`, "(", `\)`, ")", `\\`, `\`, `\n`, " ", `\r`, " ", `\t`, " ")
	return cleanWhitespace(replacer.Replace(value))
}

func hasLetterOrDigit(value string) bool {
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
