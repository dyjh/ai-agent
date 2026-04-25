package candidate

import (
	"context"
	"testing"

	"local-agent/internal/agent/planner/catalog"
	"local-agent/internal/agent/planner/normalize"
	"local-agent/internal/core"
)

func TestSelectorWorkspaceQuotedTextRecallsSearch(t *testing.T) {
	req := normalize.New().Normalize("Find files containing `hello` in workspace /tmp/demo")
	candidates, err := New().Select(context.Background(), SelectionInput{Request: req, Catalog: catalog.New(nil), TopK: 5})
	if err != nil {
		t.Fatalf("Select error = %v", err)
	}
	if !hasCandidate(candidates, "code.search_text") {
		t.Fatalf("candidates = %+v, want code.search_text", candidates)
	}
}

func TestSelectorWorkspacePossibleFileRecallsRead(t *testing.T) {
	req := normalize.New().Normalize("Open `index.html` in workspace /tmp/demo")
	candidates, err := New().Select(context.Background(), SelectionInput{Request: req, Catalog: catalog.New(nil), TopK: 5})
	if err != nil {
		t.Fatalf("Select error = %v", err)
	}
	if len(candidates) == 0 || candidates[0].ToolID != "code.read_file" {
		t.Fatalf("candidates = %+v, want code.read_file first", candidates)
	}
}

func TestSelectorKBIDRecallsKnowledgeTools(t *testing.T) {
	req := normalize.New().Normalize("kb_id: kb_1 question")
	candidates, err := New().Select(context.Background(), SelectionInput{Request: req, Catalog: catalog.New(nil), TopK: 8})
	if err != nil {
		t.Fatalf("Select error = %v", err)
	}
	if !hasCandidate(candidates, "kb.answer") || !hasCandidate(candidates, "kb.retrieve") {
		t.Fatalf("candidates = %+v, want kb.answer and kb.retrieve", candidates)
	}
}

func TestSelectorHostIDRecallsHostScopedOps(t *testing.T) {
	reg := staticRegistry{specs: []core.ToolSpec{{
		Name:           "ops.ssh.processes",
		Description:    "Read SSH process list",
		InputSchema:    map[string]any{"host_id": "string"},
		DefaultEffects: []string{"read", "ssh.read"},
	}}}
	cat := catalog.NewWithToolCards(reg, []catalog.ToolCard{{
		ToolID:         "ops.ssh.processes",
		Domain:         "ops",
		Title:          "SSH Processes",
		Description:    "Read process list for a host profile.",
		InputSchema:    map[string]any{"host_id": "string"},
		AutoSelectable: true,
	}})
	req := normalize.New().Normalize("host_id: remote")
	candidates, err := New().Select(context.Background(), SelectionInput{Request: req, Catalog: cat, TopK: 3})
	if err != nil {
		t.Fatalf("Select error = %v", err)
	}
	if len(candidates) == 0 || candidates[0].ToolID != "ops.ssh.processes" {
		t.Fatalf("candidates = %+v, want ops.ssh.processes", candidates)
	}
}

func TestSelectorUsesToolCardsWithoutSemanticSignals(t *testing.T) {
	req := normalize.New().Normalize("Get local machine system overview")
	for _, signal := range req.Signals {
		if signal == "system_overview" {
			t.Fatalf("signals = %#v, must not contain semantic signal", req.Signals)
		}
	}
	candidates, err := New().Select(context.Background(), SelectionInput{Request: req, Catalog: catalog.New(nil), TopK: 3})
	if err != nil {
		t.Fatalf("Select error = %v", err)
	}
	if len(candidates) == 0 || candidates[0].ToolID != "ops.local.system_info" {
		t.Fatalf("candidates = %+v, want ops.local.system_info from Tool Card text", candidates)
	}
}

type staticRegistry struct {
	specs []core.ToolSpec
}

func (s staticRegistry) List() []core.ToolSpec {
	return append([]core.ToolSpec(nil), s.specs...)
}

func hasCandidate(candidates []ToolCandidate, tool string) bool {
	for _, candidate := range candidates {
		if candidate.ToolID == tool {
			return true
		}
	}
	return false
}
