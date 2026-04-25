package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ToolCard is the planner semantic description for one tool. ToolRegistry
// remains the source of truth for tool existence and effects; cards add
// planner-facing descriptions, examples, slot requirements, and defaults.
type ToolCard struct {
	ToolID           string         `json:"tool_id" yaml:"tool_id"`
	Domain           string         `json:"domain" yaml:"domain"`
	Title            string         `json:"title" yaml:"title"`
	Description      string         `json:"description" yaml:"description"`
	DescriptionZH    string         `json:"description_zh,omitempty" yaml:"description_zh,omitempty"`
	WhenToUse        []string       `json:"when_to_use,omitempty" yaml:"when_to_use,omitempty"`
	WhenNotToUse     []string       `json:"when_not_to_use,omitempty" yaml:"when_not_to_use,omitempty"`
	RequiredSlots    []string       `json:"required_slots,omitempty" yaml:"required_slots,omitempty"`
	InputSchema      map[string]any `json:"input_schema,omitempty" yaml:"input_schema,omitempty"`
	Defaults         map[string]any `json:"defaults,omitempty" yaml:"defaults,omitempty"`
	Effects          []string       `json:"effects,omitempty" yaml:"effects,omitempty"`
	RiskLevel        string         `json:"risk_level,omitempty" yaml:"risk_level,omitempty"`
	AutoSelectable   bool           `json:"auto_selectable" yaml:"auto_selectable"`
	Examples         []ToolExample  `json:"examples,omitempty" yaml:"examples,omitempty"`
	NegativeExamples []ToolExample  `json:"negative_examples,omitempty" yaml:"negative_examples,omitempty"`
}

// ToolCardFile is the YAML file shape for planner tool cards.
type ToolCardFile struct {
	ToolCards []ToolCard `json:"tool_cards" yaml:"tool_cards"`
}

// LoadToolCards reads tool cards from YAML. It accepts either a top-level
// tool_cards object or a direct YAML array.
func LoadToolCards(path string) ([]ToolCard, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file ToolCardFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse tool cards: %w", err)
	}
	if len(file.ToolCards) == 0 {
		var direct []ToolCard
		if err := yaml.Unmarshal(data, &direct); err != nil {
			return nil, fmt.Errorf("parse tool cards: %w", err)
		}
		file.ToolCards = direct
	}
	seen := map[string]struct{}{}
	out := make([]ToolCard, 0, len(file.ToolCards))
	for _, card := range file.ToolCards {
		card.ToolID = strings.TrimSpace(card.ToolID)
		if card.ToolID == "" {
			return nil, fmt.Errorf("tool card missing tool_id")
		}
		if _, ok := seen[card.ToolID]; ok {
			return nil, fmt.Errorf("duplicate tool card: %s", card.ToolID)
		}
		seen[card.ToolID] = struct{}{}
		out = append(out, normalizeCard(card))
	}
	return out, nil
}

// LoadDefaultToolCards loads config/planner.tool-cards.yaml when it is
// discoverable from the current working directory or its parents.
func LoadDefaultToolCards() ([]ToolCard, string, error) {
	path := DefaultToolCardPath()
	if path == "" {
		return nil, "", nil
	}
	cards, err := LoadToolCards(path)
	return cards, path, err
}

// DefaultToolCardPath finds the repository-local tool card config.
func DefaultToolCardPath() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(wd, "config", "planner.tool-cards.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			return ""
		}
		wd = parent
	}
}

func normalizeCard(card ToolCard) ToolCard {
	card.Domain = strings.TrimSpace(card.Domain)
	card.Title = strings.TrimSpace(card.Title)
	card.Description = strings.TrimSpace(card.Description)
	card.DescriptionZH = strings.TrimSpace(card.DescriptionZH)
	card.RiskLevel = strings.TrimSpace(card.RiskLevel)
	card.WhenToUse = cleanStrings(card.WhenToUse)
	card.WhenNotToUse = cleanStrings(card.WhenNotToUse)
	card.RequiredSlots = cleanStrings(card.RequiredSlots)
	card.Effects = cleanStrings(card.Effects)
	card.InputSchema = cloneMap(card.InputSchema)
	card.Defaults = cloneMap(card.Defaults)
	card.Examples = cloneExamples(card.Examples)
	card.NegativeExamples = cloneExamples(card.NegativeExamples)
	return card
}

func cleanStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
