package candidate

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"local-agent/internal/agent/planner/catalog"
	"local-agent/internal/agent/planner/normalize"
)

const defaultTopK = 8

// SelectionInput is the candidate selector input.
type SelectionInput struct {
	Request normalize.NormalizedRequest
	Catalog catalog.PlanningCatalog
	TopK    int
}

// ToolCandidate is a planner candidate, not a final tool decision.
type ToolCandidate struct {
	ToolID string           `json:"tool_id"`
	Score  float64          `json:"score"`
	Reason string           `json:"reason"`
	Card   catalog.ToolCard `json:"card,omitempty"`
}

// Selector selects candidate tools for semantic planning.
type Selector interface {
	Select(ctx context.Context, input SelectionInput) ([]ToolCandidate, error)
}

// DefaultSelector recalls candidates from structural slots plus Tool Card
// metadata and generic text similarity. It does not contain natural-language
// phrase dictionaries.
type DefaultSelector struct{}

// New returns the default candidate selector.
func New() DefaultSelector {
	return DefaultSelector{}
}

// Select returns TopK candidate tools.
func (DefaultSelector) Select(_ context.Context, input SelectionInput) ([]ToolCandidate, error) {
	topK := input.TopK
	if topK <= 0 {
		topK = defaultTopK
	}
	req := input.Request
	items := input.Catalog.All()
	if len(items) == 0 {
		return nil, nil
	}

	scored := make([]ToolCandidate, 0, len(items))
	for _, spec := range items {
		score, reasons := scoreSpec(req, spec)
		if score <= 0 {
			continue
		}
		scored = append(scored, ToolCandidate{
			ToolID: spec.ToolID,
			Score:  score,
			Reason: strings.Join(reasons, "; "),
			Card:   cardForSpec(spec),
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].ToolID < scored[j].ToolID
		}
		return scored[i].Score > scored[j].Score
	})
	if len(scored) > topK {
		scored = scored[:topK]
	}
	return scored, nil
}

func scoreSpec(req normalize.NormalizedRequest, spec catalog.PlanningToolSpec) (float64, []string) {
	score := TextSimilarity(req.Original, spec.SemanticText()) * 4
	reasons := []string{}
	if score > 0 {
		reasons = append(reasons, fmt.Sprintf("tool-card similarity %.2f", score))
	}
	if req.ExplicitToolID != "" {
		if req.ExplicitToolID == spec.ToolID {
			return score + 100, append(reasons, "explicit tool id")
		}
		return 0, nil
	}
	if req.Workspace != "" && hasAnyInput(spec, "path", "workspace") {
		score += 0.35
		reasons = append(reasons, "workspace slot")
	}
	if len(req.QuotedTexts) > 0 && hasRequiredSlot(spec, "quoted_text") {
		score += 1.5
		reasons = append(reasons, "quoted text slot")
	}
	if len(req.PossibleFiles) > 0 {
		if hasRequiredSlot(spec, "possible_file") {
			score += 3
			reasons = append(reasons, "possible file slot")
		} else if hasAnyInput(spec, "path", "manifest_path") {
			score += 0.4
			reasons = append(reasons, "file path slot")
		}
	}
	if len(req.URLs) > 0 && hasAnyInput(spec, "url", "source_uri") {
		score += 2
		reasons = append(reasons, "url slot")
	}
	if req.HostID != "" && (hasAnyInput(spec, "host_id") || strings.HasPrefix(spec.ToolID, "ops.ssh.")) {
		score += 2
		reasons = append(reasons, "host id slot")
	}
	if req.KBID != "" && (spec.Domain == "rag" || hasAnyInput(spec, "kb_id")) {
		score += 3
		reasons = append(reasons, "kb id slot")
	}
	if req.RunID != "" && (spec.Domain == "run" || hasAnyInput(spec, "run_id")) {
		score += 3
		reasons = append(reasons, "run id slot")
	}
	if req.ApprovalID != "" && (spec.Domain == "approval" || hasAnyInput(spec, "approval_id")) {
		score += 3
		reasons = append(reasons, "approval id slot")
	}
	if len(req.Numbers) > 0 && hasNumericInput(spec) {
		score += 0.2
		reasons = append(reasons, "number slot")
	}
	if !slotsSatisfied(req, spec.RequiredSlots) {
		score *= 0.35
		reasons = append(reasons, "missing preferred slot")
	}
	if !spec.AutoSelectable {
		score *= 0.75
		reasons = append(reasons, "approval-gated candidate")
	}
	return score, reasons
}

func slotsSatisfied(req normalize.NormalizedRequest, slots []string) bool {
	for _, slot := range slots {
		switch strings.TrimSpace(slot) {
		case "workspace":
			if req.Workspace == "" {
				return false
			}
		case "quoted_text":
			if len(req.QuotedTexts) == 0 {
				return false
			}
		case "possible_file", "file_path":
			if len(req.PossibleFiles) == 0 {
				return false
			}
		case "url":
			if len(req.URLs) == 0 {
				return false
			}
		case "host_id":
			if req.HostID == "" {
				return false
			}
		case "kb_id":
			if req.KBID == "" {
				return false
			}
		case "run_id":
			if req.RunID == "" {
				return false
			}
		case "approval_id":
			if req.ApprovalID == "" {
				return false
			}
		case "explicit_tool_id":
			if req.ExplicitToolID == "" {
				return false
			}
		case "number":
			if len(req.Numbers) == 0 {
				return false
			}
		}
	}
	return true
}

func hasRequiredSlot(spec catalog.PlanningToolSpec, slot string) bool {
	for _, item := range spec.RequiredSlots {
		if item == slot {
			return true
		}
	}
	return false
}

func hasAnyInput(spec catalog.PlanningToolSpec, fields ...string) bool {
	for _, field := range fields {
		if _, ok := spec.InputSchema[field]; ok {
			return true
		}
	}
	if props, ok := spec.InputSchema["properties"].(map[string]any); ok {
		for _, field := range fields {
			if _, ok := props[field]; ok {
				return true
			}
		}
	}
	return false
}

func hasNumericInput(spec catalog.PlanningToolSpec) bool {
	for _, typ := range spec.InputSchema {
		if strings.Contains(strings.ToLower(fmt.Sprint(typ)), "number") || strings.Contains(strings.ToLower(fmt.Sprint(typ)), "integer") {
			return true
		}
	}
	return false
}

func cardForSpec(spec catalog.PlanningToolSpec) catalog.ToolCard {
	if spec.Card != nil {
		return *spec.Card
	}
	return catalog.ToolCard{
		ToolID:           spec.ToolID,
		Domain:           spec.Domain,
		Title:            spec.Title,
		Description:      spec.Description,
		DescriptionZH:    spec.DescriptionZH,
		WhenToUse:        append([]string(nil), spec.WhenToUse...),
		WhenNotToUse:     append([]string(nil), spec.WhenNotToUse...),
		RequiredSlots:    append([]string(nil), spec.RequiredSlots...),
		InputSchema:      cloneMap(spec.InputSchema),
		Defaults:         cloneMap(spec.Defaults),
		Effects:          append([]string(nil), spec.DefaultEffects...),
		RiskLevel:        spec.RiskLevel,
		AutoSelectable:   spec.AutoSelectable,
		Examples:         append([]catalog.ToolExample(nil), spec.Examples...),
		NegativeExamples: append([]catalog.ToolExample(nil), spec.NegativeExamples...),
	}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// TextSimilarity is a language-agnostic lexical scorer used for Tool Card
// retrieval and example selection.
func TextSimilarity(a, b string) float64 {
	left := tokenWeights(a)
	right := tokenWeights(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for token, l := range left {
		leftNorm += l * l
		if r, ok := right[token]; ok {
			dot += l * r
		}
	}
	for _, r := range right {
		rightNorm += r * r
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

func tokenWeights(value string) map[string]float64 {
	out := map[string]float64{}
	var ascii []rune
	var han []rune
	flushASCII := func() {
		if len(ascii) == 0 {
			return
		}
		token := strings.ToLower(string(ascii))
		out[token]++
		ascii = ascii[:0]
	}
	flushHan := func() {
		if len(han) == 0 {
			return
		}
		for _, r := range han {
			out[string(r)] += 0.35
		}
		for i := 0; i+1 < len(han); i++ {
			out[string(han[i:i+2])]++
		}
		han = han[:0]
	}
	for _, r := range value {
		switch {
		case r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.'):
			flushHan()
			ascii = append(ascii, unicode.ToLower(r))
		case unicode.Is(unicode.Han, r):
			flushASCII()
			han = append(han, r)
		default:
			flushASCII()
			flushHan()
		}
	}
	flushASCII()
	flushHan()
	return out
}
