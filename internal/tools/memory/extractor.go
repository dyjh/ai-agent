package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"unicode"
)

// ExtractCandidates extracts durable memory candidates without committing them.
func ExtractCandidates(input MemoryExtractInput) []MemoryCandidate {
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return nil
	}
	scan := ScanSensitiveMemory(text)
	if scan.Sensitive {
		return nil
	}
	normalized := strings.ToLower(text)
	if isOneShotQuestion(normalized) && !hasRememberIntent(normalized) {
		return nil
	}
	if !hasRememberIntent(normalized) && !looksDurableMemory(normalized, input.ProjectKey) {
		return nil
	}

	clean := cleanCandidateText(text)
	if clean == "" {
		return nil
	}
	scope := MemoryScopeUser
	itemType := MemoryTypePreference
	reason := "long-term user preference"
	tags := []string{"extracted"}
	if input.ProjectKey != "" || strings.Contains(normalized, "这个项目") || strings.Contains(normalized, "项目") {
		scope = MemoryScopeProject
		itemType = MemoryTypeProject
		reason = "stable project memory"
		tags = append(tags, "project")
	}
	if strings.Contains(normalized, "流程") || strings.Contains(normalized, "步骤") || strings.Contains(normalized, "procedure") || strings.Contains(normalized, "runbook") {
		scope = MemoryScopeProcedure
		itemType = MemoryTypeProcedure
		reason = "procedure memory"
		tags = append(tags, "procedure")
	}
	if strings.Contains(normalized, "不要再") || strings.Contains(normalized, "avoid") || strings.Contains(normalized, "don't") {
		tags = append(tags, "negative_preference")
	}

	candidate := MemoryCandidate{
		ID:         candidateID(clean),
		Scope:      scope,
		Type:       itemType,
		ProjectKey: input.ProjectKey,
		Text:       clean,
		Confidence: 0.82,
		Importance: 0.7,
		Reason:     reason,
		TargetPath: memoryItemPath(MemoryItem{Scope: scope, Type: itemType, ProjectKey: input.ProjectKey}),
		Source:     "conversation",
		SourceID:   input.MessageID,
		Tags:       uniqueStrings(tags),
	}
	if input.MessageID == "" {
		candidate.SourceID = input.ConversationID
	}
	return []MemoryCandidate{candidate}
}

// ExtractAndReview extracts candidates and queues them for review.
func (s *Store) ExtractAndReview(input MemoryExtractInput) ([]MemoryReviewItem, error) {
	candidates := ExtractCandidates(input)
	reviews := make([]MemoryReviewItem, 0, len(candidates))
	for _, candidate := range candidates {
		review, err := s.CreateReview(candidate)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, review)
	}
	return reviews, nil
}

// DetectConflicts returns basic lexical/metadata conflicts for a candidate.
func (s *Store) DetectConflicts(candidate MemoryCandidate) ([]MemoryConflict, error) {
	items, err := s.ListItems(MemoryItemFilter{IncludeArchived: true})
	if err != nil {
		return nil, err
	}
	conflicts := []MemoryConflict{}
	for _, item := range items {
		if item.Status != MemoryItemStatusActive || item.Sensitive {
			continue
		}
		if candidate.ProjectKey != "" && item.ProjectKey != "" && candidate.ProjectKey != item.ProjectKey {
			continue
		}
		if candidate.Scope != "" && item.Scope != candidate.Scope {
			continue
		}
		score := lexicalSimilarity(candidate.Text, item.Text)
		if score >= 0.82 {
			conflicts = append(conflicts, MemoryConflict{
				ConflictID:  "memconf_" + shortHash(candidate.ID+"duplicate"+item.ID),
				Type:        "duplicate",
				CandidateID: candidate.ID,
				ExistingIDs: []string{item.ID},
				Summary:     "candidate is lexically similar to an active memory item",
				Severity:    "low",
			})
			continue
		}
		if basicContradiction(candidate.Text, item.Text) {
			conflicts = append(conflicts, MemoryConflict{
				ConflictID:  "memconf_" + shortHash(candidate.ID+"contradiction"+item.ID),
				Type:        "contradiction",
				CandidateID: candidate.ID,
				ExistingIDs: []string{item.ID},
				Summary:     "candidate appears to contradict an active memory item",
				Severity:    "medium",
			})
		}
	}
	return conflicts, nil
}

// MergeCandidates returns a conservative merge suggestion; it never writes.
func (s *Store) MergeCandidates(candidate MemoryCandidate) (map[string]any, error) {
	conflicts, err := s.DetectConflicts(candidate)
	if err != nil {
		return nil, err
	}
	action := "create_new"
	for _, conflict := range conflicts {
		if conflict.Type == "duplicate" {
			action = "merge_with_existing"
			break
		}
		if conflict.Type == "contradiction" {
			action = "manual_review"
		}
	}
	return map[string]any{
		"candidate": candidate,
		"action":    action,
		"conflicts": conflicts,
	}, nil
}

func hasRememberIntent(value string) bool {
	return strings.Contains(value, "记住") ||
		strings.Contains(value, "记一下") ||
		strings.Contains(value, "以后") ||
		strings.Contains(value, "remember") ||
		strings.Contains(value, "always") ||
		strings.Contains(value, "never")
}

func looksDurableMemory(value, projectKey string) bool {
	return strings.Contains(value, "我喜欢") ||
		strings.Contains(value, "我偏好") ||
		strings.Contains(value, "默认") ||
		strings.Contains(value, "不要再") ||
		strings.Contains(value, "这个项目") ||
		projectKey != ""
}

func isOneShotQuestion(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasSuffix(value, "?") || strings.HasSuffix(value, "？") || strings.Contains(value, "吗")
}

func cleanCandidateText(text string) string {
	text = strings.TrimSpace(text)
	replacers := []string{
		"请记住", "记住", "记一下", "帮我记住", "以后请", "以后",
		"please remember", "remember that", "remember", "always",
	}
	lower := strings.ToLower(text)
	for _, prefix := range replacers {
		if strings.HasPrefix(lower, strings.ToLower(prefix)) {
			text = strings.TrimSpace(text[len(prefix):])
			break
		}
	}
	return strings.Trim(text, " ：:，,。.")
}

func candidateID(text string) string {
	return "memcand_" + shortHash(text+time.Now().UTC().Format(time.RFC3339Nano))
}

func shortHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:16]
}

func lexicalSimilarity(a, b string) float64 {
	aTokens := tokenSet(a)
	bTokens := tokenSet(b)
	if len(aTokens) == 0 || len(bTokens) == 0 {
		return 0
	}
	intersection := 0
	for token := range aTokens {
		if bTokens[token] {
			intersection++
		}
	}
	union := len(aTokens) + len(bTokens) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func tokenSet(value string) map[string]bool {
	out := map[string]bool{}
	var current strings.Builder
	flush := func() {
		token := strings.ToLower(strings.TrimSpace(current.String()))
		current.Reset()
		if token != "" {
			out[token] = true
		}
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func basicContradiction(candidate, existing string) bool {
	candidate = strings.ToLower(candidate)
	existing = strings.ToLower(existing)
	for _, marker := range []string{"不要", "不再", "never", "avoid", "don't", "do not"} {
		if strings.Contains(candidate, marker) && !strings.Contains(existing, marker) {
			return lexicalSimilarity(removeNegation(candidate), existing) >= 0.4
		}
		if strings.Contains(existing, marker) && !strings.Contains(candidate, marker) {
			return lexicalSimilarity(candidate, removeNegation(existing)) >= 0.4
		}
	}
	return false
}

func removeNegation(value string) string {
	for _, marker := range []string{"不要", "不再", "never", "avoid", "don't", "do not", "not"} {
		value = strings.ReplaceAll(value, marker, "")
	}
	return strings.TrimSpace(value)
}
