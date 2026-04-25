package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/security"
)

var (
	memoryItemStart = regexp.MustCompile(`^\s*<!--\s*memory:item\s*([^>]*)-->\s*$`)
	memoryItemEnd   = regexp.MustCompile(`^\s*<!--\s*/memory:item\s*-->\s*$`)
	memoryAttr      = regexp.MustCompile(`([a-zA-Z0-9_-]+)="([^"]*)"`)
)

// ParseMemoryDocument parses item metadata from a Markdown-backed memory file.
func ParseMemoryDocument(file core.MemoryFile) MemoryDocument {
	doc := MemoryDocument{
		Path:        file.Path,
		Frontmatter: cloneStringMap(file.Frontmatter),
		Items:       []MemoryItem{},
	}
	lines := strings.Split(file.Body, "\n")
	var preamble []string
	foundItems := false
	now := time.Now().UTC()

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		match := memoryItemStart.FindStringSubmatch(line)
		if match == nil {
			preamble = append(preamble, line)
			continue
		}
		foundItems = true
		attrs := parseMemoryAttrs(match[1])
		var bodyLines []string
		i++
		for ; i < len(lines); i++ {
			if memoryItemEnd.MatchString(lines[i]) {
				break
			}
			bodyLines = append(bodyLines, lines[i])
		}
		item := memoryItemFromAttrs(file, attrs, strings.Join(bodyLines, "\n"), now)
		if item.Text != "" {
			doc.Items = append(doc.Items, item)
		}
	}

	doc.Preamble = strings.TrimSpace(strings.Join(preamble, "\n"))
	if !foundItems {
		text := legacyMemoryText(file.Body)
		if text != "" {
			doc.Items = append(doc.Items, legacyMemoryItem(file, text, now))
		}
	}
	return doc
}

// RenderMemoryDocument renders a document with item comments. Legacy-only items
// are not re-rendered because their text already exists in the preamble.
func RenderMemoryDocument(doc MemoryDocument) core.MemoryFile {
	var body strings.Builder
	if strings.TrimSpace(doc.Preamble) != "" {
		body.WriteString(strings.TrimSpace(doc.Preamble))
		body.WriteString("\n\n")
	}
	for _, item := range doc.Items {
		if strings.HasPrefix(item.ID, "legacy_") {
			continue
		}
		body.WriteString(RenderMemoryItem(item))
		body.WriteString("\n\n")
	}
	return core.MemoryFile{
		Path:        doc.Path,
		Frontmatter: cloneStringMap(doc.Frontmatter),
		Body:        strings.TrimSpace(body.String()),
	}
}

// RenderMemoryItem renders one item block.
func RenderMemoryItem(item MemoryItem) string {
	attrs := map[string]string{
		"id":         item.ID,
		"scope":      string(defaultScope(item.Scope)),
		"type":       string(defaultType(item.Type)),
		"status":     string(defaultStatus(item.Status)),
		"importance": formatFloat(defaultPositive(item.Importance, 0.5)),
		"confidence": formatFloat(defaultPositive(item.Confidence, 0.7)),
		"updated_at": formatTime(item.UpdatedAt),
	}
	if !item.CreatedAt.IsZero() {
		attrs["created_at"] = formatTime(item.CreatedAt)
	}
	if item.ProjectKey != "" {
		attrs["project_key"] = item.ProjectKey
	}
	if item.Source != "" {
		attrs["source"] = item.Source
	}
	if item.SourceMessageID != "" {
		attrs["source_message_id"] = item.SourceMessageID
	}
	if len(item.Tags) > 0 {
		attrs["tags"] = strings.Join(item.Tags, ",")
	}
	if item.Sensitive {
		attrs["sensitive"] = "true"
	}
	if item.ExpiresAt != nil {
		attrs["expires_at"] = formatTime(*item.ExpiresAt)
	}
	if item.LastUsedAt != nil {
		attrs["last_used_at"] = formatTime(*item.LastUsedAt)
	}
	if item.UseCount > 0 {
		attrs["use_count"] = strconv.Itoa(item.UseCount)
	}
	if item.DecayPolicy != "" {
		attrs["decay_policy"] = item.DecayPolicy
	}

	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	builder.WriteString("<!-- memory:item")
	for _, key := range keys {
		if attrs[key] == "" {
			continue
		}
		builder.WriteString(" ")
		builder.WriteString(key)
		builder.WriteString(`="`)
		builder.WriteString(escapeAttr(attrs[key]))
		builder.WriteString(`"`)
	}
	builder.WriteString(" -->\n")
	text := strings.TrimSpace(item.Text)
	if text != "" {
		if strings.HasPrefix(text, "- ") {
			builder.WriteString(text)
		} else {
			builder.WriteString("- ")
			builder.WriteString(text)
		}
		builder.WriteString("\n")
	}
	builder.WriteString("<!-- /memory:item -->")
	return builder.String()
}

// ScanSensitiveMemory detects memory content that must not be persisted.
func ScanSensitiveMemory(text string) SensitiveMemoryScan {
	scan := security.ScanText(text)
	redacted := scan.RedactedText
	findings := []string{}
	lower := strings.ToLower(text)
	for _, finding := range scan.Findings {
		findings = append(findings, finding.Type)
	}
	if strings.Contains(lower, "-----begin ") && strings.Contains(lower, "private key-----") {
		findings = append(findings, "private_key_block")
	}
	if strings.Contains(lower, ".env") && containsAnyLine(lower, []string{"=", "api_key", "token", "secret", "password"}) {
		findings = append(findings, "env_content")
	}
	if strings.Contains(lower, "aws_access_key_id") || strings.Contains(lower, "aws_secret_access_key") {
		findings = append(findings, "cloud_credential")
	}
	return SensitiveMemoryScan{
		Sensitive: security.MustBlockLongTermStorage(scan) || len(findings) > 0,
		Redacted:  redacted,
		Findings:  uniqueStrings(findings),
	}
}

func memoryItemFromAttrs(file core.MemoryFile, attrs map[string]string, body string, now time.Time) MemoryItem {
	scope := MemoryScope(attrs["scope"])
	if scope == "" {
		scope = inferScope(file)
	}
	itemType := MemoryType(attrs["type"])
	if itemType == "" {
		itemType = inferType(file)
	}
	status := MemoryItemStatus(attrs["status"])
	if status == "" {
		status = MemoryItemStatusActive
	}
	text := cleanMemoryText(body)
	scan := ScanSensitiveMemory(text)
	createdAt := parseTimeOrZero(attrs["created_at"])
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := parseTimeOrZero(attrs["updated_at"])
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	return MemoryItem{
		ID:              nonEmpty(attrs["id"], stableMemoryID(file.Path, text)),
		Scope:           defaultScope(scope),
		Type:            defaultType(itemType),
		ProjectKey:      attrs["project_key"],
		Text:            scan.Redacted,
		Source:          attrs["source"],
		SourceMessageID: attrs["source_message_id"],
		Confidence:      parseFloat(attrs["confidence"], 0.7),
		Importance:      parseFloat(attrs["importance"], 0.5),
		Tags:            splitCSV(attrs["tags"]),
		Sensitive:       parseBool(attrs["sensitive"]) || scan.Sensitive,
		Status:          status,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
		ExpiresAt:       parseOptionalTime(attrs["expires_at"]),
		LastUsedAt:      parseOptionalTime(attrs["last_used_at"]),
		UseCount:        parseInt(attrs["use_count"], 0),
		DecayPolicy:     attrs["decay_policy"],
		Path:            file.Path,
	}
}

func legacyMemoryItem(file core.MemoryFile, text string, now time.Time) MemoryItem {
	scan := ScanSensitiveMemory(text)
	return MemoryItem{
		ID:         stableMemoryID(file.Path, text),
		Scope:      inferScope(file),
		Type:       inferType(file),
		Text:       scan.Redacted,
		Confidence: 0.6,
		Importance: 0.5,
		Sensitive:  scan.Sensitive,
		Status:     MemoryItemStatusActive,
		CreatedAt:  now,
		UpdatedAt:  now,
		Path:       file.Path,
	}
}

func parseMemoryAttrs(raw string) map[string]string {
	out := map[string]string{}
	for _, match := range memoryAttr.FindAllStringSubmatch(raw, -1) {
		if len(match) == 3 {
			out[match[1]] = strings.ReplaceAll(match[2], `\"`, `"`)
		}
	}
	return out
}

func legacyMemoryText(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.EqualFold(trimmed, "pending.") {
			continue
		}
		out = append(out, strings.TrimPrefix(trimmed, "- "))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func cleanMemoryText(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for idx, line := range lines {
		lines[idx] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func stableMemoryID(path, text string) string {
	sum := sha256.Sum256([]byte(path + "\n" + strings.TrimSpace(text)))
	return "legacy_" + hex.EncodeToString(sum[:])[:16]
}

func inferScope(file core.MemoryFile) MemoryScope {
	if value := MemoryScope(file.Frontmatter["scope"]); value != "" {
		return value
	}
	path := strings.ToLower(file.Path)
	kind := strings.ToLower(file.Frontmatter["kind"])
	switch {
	case strings.Contains(path, "project") || kind == "project":
		return MemoryScopeProject
	case strings.Contains(path, "procedure") || kind == "procedure":
		return MemoryScopeProcedure
	default:
		return MemoryScopeUser
	}
}

func inferType(file core.MemoryFile) MemoryType {
	if value := MemoryType(file.Frontmatter["type"]); value != "" {
		return value
	}
	path := strings.ToLower(file.Path)
	kind := strings.ToLower(file.Frontmatter["kind"])
	switch {
	case strings.Contains(path, "preference") || kind == "preferences" || kind == "preference":
		return MemoryTypePreference
	case strings.Contains(path, "project") || kind == "project":
		return MemoryTypeProject
	case strings.Contains(path, "procedure") || kind == "procedure":
		return MemoryTypeProcedure
	default:
		return MemoryTypeFact
	}
}

func defaultScope(scope MemoryScope) MemoryScope {
	switch scope {
	case MemoryScopeUser, MemoryScopeProject, MemoryScopeProcedure, MemoryScopeConversation:
		return scope
	default:
		return MemoryScopeUser
	}
}

func defaultType(itemType MemoryType) MemoryType {
	switch itemType {
	case MemoryTypePreference, MemoryTypeFact, MemoryTypeProcedure, MemoryTypeProject, MemoryTypeEpisode:
		return itemType
	default:
		return MemoryTypeFact
	}
}

func defaultStatus(status MemoryItemStatus) MemoryItemStatus {
	switch status {
	case MemoryItemStatusActive, MemoryItemStatusPending, MemoryItemStatusRejected, MemoryItemStatusExpired, MemoryItemStatusArchived:
		return status
	default:
		return MemoryItemStatusActive
	}
}

func parseOptionalTime(value string) *time.Time {
	parsed := parseTimeOrZero(value)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}

func parseTimeOrZero(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func parseFloat(value string, fallback float64) float64 {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseBool(value string) bool {
	parsed, _ := strconv.ParseBool(strings.TrimSpace(value))
	return parsed
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func defaultPositive(value, fallback float64) float64 {
	if value <= 0 {
		return fallback
	}
	return value
}

func escapeAttr(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func containsAnyLine(value string, needles []string) bool {
	for _, line := range strings.Split(value, "\n") {
		for _, needle := range needles {
			if strings.Contains(line, needle) {
				return true
			}
		}
	}
	return false
}

func uniqueStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func itemExpired(item MemoryItem, now time.Time) bool {
	return item.ExpiresAt != nil && !item.ExpiresAt.IsZero() && item.ExpiresAt.Before(now)
}

func itemActiveForContext(item MemoryItem, now time.Time) bool {
	return item.Status == MemoryItemStatusActive && !item.Sensitive && !itemExpired(item, now)
}

func memoryItemPath(item MemoryItem) string {
	if item.Path != "" {
		return item.Path
	}
	switch item.Scope {
	case MemoryScopeProject:
		if item.ProjectKey != "" {
			return fmt.Sprintf("projects/%s.md", safePathPart(item.ProjectKey))
		}
		return "projects/general.md"
	case MemoryScopeProcedure:
		return "procedures.md"
	case MemoryScopeConversation:
		return "conversation.md"
	default:
		if item.Type == MemoryTypePreference {
			return "preferences.md"
		}
		return "long_term.md"
	}
}

func safePathPart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "general"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	out := strings.Trim(builder.String(), "_")
	if out == "" {
		return "general"
	}
	return out
}
