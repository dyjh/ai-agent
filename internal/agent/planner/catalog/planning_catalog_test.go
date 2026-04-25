package catalog

import (
	"testing"

	"local-agent/internal/core"
	toolscore "local-agent/internal/tools"
)

func TestPlanningCatalogGeneratedFromRegistry(t *testing.T) {
	registry := toolscore.NewRegistry()
	registry.Register(core.ToolSpec{Name: "custom.read", Description: "Custom read", DefaultEffects: []string{"read"}}, nil)
	cat := New(registry)
	if _, ok := cat.Tool("custom.read"); !ok {
		t.Fatalf("custom tool missing")
	}
	if spec, ok := cat.Tool("code.read_file"); !ok || spec.Domain != "code" {
		t.Fatalf("core code tool = %+v ok=%v", spec, ok)
	}
}

func TestPlanningCatalogCoreExamplesAndDangerousTools(t *testing.T) {
	cat := New(nil)
	for _, tool := range []string{"code.read_file", "code.search_text", "ops.local.system_info", "kb.answer", "memory.extract_candidates"} {
		spec, ok := cat.Tool(tool)
		if !ok || spec.Domain == "" || len(spec.Examples) == 0 {
			t.Fatalf("tool %s spec=%+v ok=%v", tool, spec, ok)
		}
	}
	clean, ok := cat.Tool("git.clean")
	if !ok || clean.AutoSelectable {
		t.Fatalf("git.clean spec=%+v, want not auto-selectable", clean)
	}
}

func TestToolCardCatalogLoadsAndMergesDefaults(t *testing.T) {
	cards, path, err := LoadDefaultToolCards()
	if err != nil {
		t.Fatalf("LoadDefaultToolCards() path=%s error=%v", path, err)
	}
	if len(cards) == 0 {
		t.Fatalf("expected default tool cards")
	}
	cat := NewWithToolCards(nil, cards)
	search, ok := cat.Tool("code.search_text")
	if !ok {
		t.Fatalf("code.search_text missing")
	}
	if len(search.RequiredSlots) == 0 || search.Defaults["limit"] == nil || len(search.Examples) == 0 {
		t.Fatalf("search spec = %+v, want required slots/defaults/examples from tool card", search)
	}
	if search.DefaultEffects[0] != "read" {
		t.Fatalf("default effects = %#v, registry/core effects must remain authoritative", search.DefaultEffects)
	}
}

func TestToolCardCatalogWarningsForUnknownTool(t *testing.T) {
	cat := NewWithToolCards(nil, []ToolCard{{
		ToolID:         "missing.tool",
		Description:    "missing",
		AutoSelectable: true,
	}})
	if len(cat.Warnings()) == 0 {
		t.Fatalf("expected warning for unknown tool card")
	}
}
