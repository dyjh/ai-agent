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
