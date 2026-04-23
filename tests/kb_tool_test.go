package tests

import (
	"context"
	"testing"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
	toolscore "local-agent/internal/tools"
	"local-agent/internal/tools/kb"
)

func TestKBSearchTool(t *testing.T) {
	cfg := config.Default()
	cfg.Vector.EmbeddingDimension = 16

	embedder := kb.FakeEmbedder{Dimensions: cfg.Vector.EmbeddingDimension}
	index := kb.NewInMemoryVectorIndex(embedder, kb.VectorRuntimeStatus{VectorBackend: "memory"})
	service := kb.NewService(index, embedder, "kb_chunks")
	registry := toolscore.NewRegistry()
	registry.Register(core.ToolSpec{
		ID:             "kb.search",
		Provider:       "local",
		Name:           "kb.search",
		Description:    "Search local knowledge base by semantic query",
		DefaultEffects: []string{"kb.read"},
	}, &kb.SearchExecutor{Service: service})
	approvals := agent.NewApprovalCenter()
	router := toolscore.NewRouter(
		registry,
		agent.NewEffectInferrer(cfg.Policy),
		agent.NewPolicyEngine(cfg.Policy),
		approvals,
		nil,
	)

	base := service.CreateKB("docs", "local docs")
	if _, err := service.UploadDocument(context.Background(), base.ID, "intro.md", "# Intro\n\nhello world\n\nvector search"); err != nil {
		t.Fatalf("UploadDocument() error = %v", err)
	}

	spec, err := registry.Spec("kb.search")
	if err != nil {
		t.Fatalf("Spec() error = %v", err)
	}
	if len(spec.DefaultEffects) != 1 || spec.DefaultEffects[0] != "kb.read" {
		t.Fatalf("default effects = %v, want [kb.read]", spec.DefaultEffects)
	}

	executor, err := registry.Executor("kb.search")
	if err != nil {
		t.Fatalf("Executor() error = %v", err)
	}
	if _, ok := executor.(toolscore.NotImplementedExecutor); ok {
		t.Fatalf("kb.search should not use NotImplementedExecutor")
	}

	outcome, err := router.Propose(context.Background(), "run_1", "conv_1", core.ToolProposal{
		ID:   "tool_1",
		Tool: "kb.search",
		Input: map[string]any{
			"kb_id": base.ID,
			"query": "hello",
			"limit": 3,
		},
	})
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if outcome.Decision.RequiresApproval {
		t.Fatalf("kb.search should auto execute")
	}
	if !containsString(outcome.Inference.Effects, "kb.read") {
		t.Fatalf("inference effects = %v, want kb.read", outcome.Inference.Effects)
	}
	if outcome.Result == nil {
		t.Fatalf("expected tool result")
	}

	rawResults, ok := outcome.Result.Output["results"].([]map[string]any)
	if !ok {
		t.Fatalf("results output has unexpected type %T", outcome.Result.Output["results"])
	}
	if len(rawResults) == 0 {
		t.Fatalf("expected at least one kb result")
	}
	if rawResults[0]["source_file"] == "" {
		t.Fatalf("expected source_file in result")
	}
	if rawResults[0]["snippet"] == "" {
		t.Fatalf("expected snippet in result")
	}
	if _, ok := rawResults[0]["payload"].(map[string]string); !ok {
		t.Fatalf("expected payload map in result, got %T", rawResults[0]["payload"])
	}
}
