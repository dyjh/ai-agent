package tests

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"local-agent/internal/agent"
	"local-agent/internal/api"
	"local-agent/internal/config"
	"local-agent/internal/core"
	"local-agent/internal/db/repo"
	toolscore "local-agent/internal/tools"
	"local-agent/internal/tools/kb"
	memstore "local-agent/internal/tools/memory"
)

func TestMemoryItemParseRenderAndLegacyCompatibility(t *testing.T) {
	file := core.MemoryFile{
		Path:        "preferences.md",
		Frontmatter: map[string]string{"title": "Preferences", "scope": "user"},
		Body: strings.TrimSpace(`
# Preferences

<!-- memory:item id="mem_1" type="preference" importance="0.9" confidence="1.0" tags="language,go" -->
- 用户偏好中文回答。
<!-- /memory:item -->
`),
	}
	doc := memstore.ParseMemoryDocument(file)
	if len(doc.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(doc.Items))
	}
	if doc.Items[0].Text != "用户偏好中文回答。" || doc.Items[0].Type != memstore.MemoryTypePreference {
		t.Fatalf("parsed item = %+v", doc.Items[0])
	}

	rendered := memstore.RenderMemoryDocument(doc)
	if rendered.Frontmatter["title"] != "Preferences" {
		t.Fatalf("frontmatter not preserved: %+v", rendered.Frontmatter)
	}
	if !strings.Contains(rendered.Body, `memory:item`) || !strings.Contains(rendered.Body, "用户偏好中文回答") {
		t.Fatalf("rendered body missing item: %s", rendered.Body)
	}

	legacy := memstore.ParseMemoryDocument(core.MemoryFile{
		Path:        "long_term.md",
		Frontmatter: map[string]string{"kind": "long_term"},
		Body:        "# Long Term\n\n用户使用 Go。",
	})
	if len(legacy.Items) != 1 || !strings.HasPrefix(legacy.Items[0].ID, "legacy_") {
		t.Fatalf("legacy item not parsed: %+v", legacy.Items)
	}
}

func TestMemoryExtractorReviewConflictAndSensitiveGuard(t *testing.T) {
	root := t.TempDir()
	store := memstore.NewStore(root, nil)
	if _, err := store.CreateItem(context.Background(), memstore.MemoryItemCreateInput{
		Scope:      memstore.MemoryScopeUser,
		Type:       memstore.MemoryTypePreference,
		Text:       "用户偏好中文回答。",
		Importance: 0.9,
	}); err != nil {
		t.Fatalf("CreateItem() error = %v", err)
	}

	candidates := memstore.ExtractCandidates(memstore.MemoryExtractInput{Text: "记住 用户偏好中文回答。"})
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	conflicts, err := store.DetectConflicts(candidates[0])
	if err != nil {
		t.Fatalf("DetectConflicts() error = %v", err)
	}
	if len(conflicts) == 0 || conflicts[0].Type != "duplicate" {
		t.Fatalf("expected duplicate conflict, got %+v", conflicts)
	}

	review, err := store.CreateReview(candidates[0])
	if err != nil {
		t.Fatalf("CreateReview() error = %v", err)
	}
	if review.Status != memstore.MemoryReviewPending || len(review.ConflictIDs) == 0 {
		t.Fatalf("review = %+v", review)
	}
	if _, err := store.RejectReview(review.ReviewID, "duplicate"); err != nil {
		t.Fatalf("RejectReview() error = %v", err)
	}

	if got := memstore.ExtractCandidates(memstore.MemoryExtractInput{Text: "记住 api_key=secret-value"}); len(got) != 0 {
		t.Fatalf("secret candidate extracted: %+v", got)
	}
	sensitiveReview, err := store.CreateReview(memstore.MemoryCandidate{
		ID:         "memcand_secret",
		Scope:      memstore.MemoryScopeUser,
		Type:       memstore.MemoryTypeFact,
		Text:       "password=secret-value",
		Confidence: 0.9,
		Importance: 0.9,
	})
	if err != nil {
		t.Fatalf("CreateReview(sensitive) error = %v", err)
	}
	if sensitiveReview.Status != memstore.MemoryReviewRejected {
		t.Fatalf("sensitive review status = %s, want rejected", sensitiveReview.Status)
	}
	if _, err := store.ApproveReview(context.Background(), sensitiveReview.ReviewID, "no"); err == nil {
		t.Fatalf("expected sensitive review approve to fail")
	}
	if _, err := store.CreateItem(context.Background(), memstore.MemoryItemCreateInput{
		Scope: memstore.MemoryScopeUser,
		Type:  memstore.MemoryTypeFact,
		Text:  "password=secret-value",
	}); err == nil {
		t.Fatalf("expected sensitive memory create to fail")
	}
}

func TestMemoryReviewApproveArchiveRestoreAndContextSelection(t *testing.T) {
	root := t.TempDir()
	index := &mockVectorIndex{}
	store := memstore.NewStore(root, &memstore.Indexer{
		Collection: "memory_chunks",
		Index:      index,
		Embedder:   kb.FakeEmbedder{Dimensions: 16},
	})

	review, err := store.CreateReview(memstore.MemoryCandidate{
		ID:         "memcand_project",
		Scope:      memstore.MemoryScopeProject,
		Type:       memstore.MemoryTypeProject,
		ProjectKey: "agent",
		Text:       "这个项目默认使用 Go 1.23。",
		Confidence: 0.9,
		Importance: 0.8,
		TargetPath: "projects/agent.md",
	})
	if err != nil {
		t.Fatalf("CreateReview() error = %v", err)
	}
	applied, err := store.ApproveReview(context.Background(), review.ReviewID, "ok")
	if err != nil {
		t.Fatalf("ApproveReview() error = %v", err)
	}
	if applied.AppliedItem == nil || applied.Status != memstore.MemoryReviewApplied {
		t.Fatalf("applied review = %+v", applied)
	}
	itemID := applied.AppliedItem.ID

	if _, err := store.ArchiveItem(context.Background(), itemID); err != nil {
		t.Fatalf("ArchiveItem() error = %v", err)
	}
	active, err := store.SearchItems("Go 1.23", 5, "agent")
	if err != nil {
		t.Fatalf("SearchItems() error = %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("archived item returned in context search: %+v", active)
	}
	if _, err := store.RestoreItem(context.Background(), itemID); err != nil {
		t.Fatalf("RestoreItem() error = %v", err)
	}

	past := time.Now().Add(-time.Hour).UTC()
	if _, err := store.CreateItem(context.Background(), memstore.MemoryItemCreateInput{
		Scope:     memstore.MemoryScopeUser,
		Type:      memstore.MemoryTypePreference,
		Text:      "过期偏好不应进入上下文。",
		ExpiresAt: &past,
	}); err != nil {
		t.Fatalf("Create expired item: %v", err)
	}

	repoStore := repo.NewMemoryStore()
	conv := core.Conversation{ID: "conv_memory", Title: "memory", ProjectKey: "agent", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := repoStore.Conversations.CreateConversation(context.Background(), conv); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	builder := agent.ContextBuilder{Store: repoStore, Memory: store, MaxChars: 4000}
	messages, err := builder.Build(context.Background(), conv.ID, "Go 1.23")
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	joined := joinMessages(messages)
	if !strings.Contains(joined, "这个项目默认使用 Go 1.23") {
		t.Fatalf("active project memory missing from context: %s", joined)
	}
	if strings.Contains(joined, "过期偏好") {
		t.Fatalf("expired memory leaked into context: %s", joined)
	}
}

func TestMemoryItemAPIUsesApprovalAndReviewAPIApplies(t *testing.T) {
	root := t.TempDir()
	store := memstore.NewStore(root, nil)
	approvals := agent.NewApprovalCenter()
	registry := toolscore.NewRegistry()
	registry.Register(core.ToolSpec{
		ID:             "memory.item_create",
		Provider:       "local",
		Name:           "memory.item_create",
		DefaultEffects: []string{"fs.write", "memory.modify"},
	}, &memstore.ItemCreateExecutor{Store: store})
	router := toolscore.NewRouter(
		registry,
		agent.NewEffectInferrer(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}),
		agent.NewPolicyEngine(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}),
		approvals,
		nil,
	)
	server := httptest.NewServer(api.NewRouter(api.Dependencies{
		Logger:    slog.Default(),
		Memory:    store,
		Router:    router,
		Approvals: approvals,
	}))
	defer server.Close()

	var route map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/memory/items", map[string]any{
		"scope": "user",
		"type":  "preference",
		"text":  "用户偏好中文回答。",
	}, &route)
	if route["approval"] == nil {
		t.Fatalf("expected approval route response: %+v", route)
	}
	approval := route["approval"].(map[string]any)
	approvalID := approval["id"].(string)
	var approved map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/approvals/"+approvalID+"/approve", map[string]any{}, &approved)
	if approved["result"] == nil {
		t.Fatalf("approval did not execute item create: %+v", approved)
	}
	var list map[string][]map[string]any
	mustRequestJSON(t, http.MethodGet, server.URL+"/v1/memory/items", nil, &list)
	if len(list["items"]) != 1 {
		t.Fatalf("items = %+v", list)
	}

	var extracted map[string][]map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/memory/review/extract", map[string]any{
		"text": "记住 这个项目默认使用 Go 1.23。",
	}, &extracted)
	if len(extracted["items"]) != 1 {
		t.Fatalf("review extract = %+v", extracted)
	}
	reviewID := extracted["items"][0]["review_id"].(string)
	var applied map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/memory/review/"+reviewID+"/approve", map[string]any{}, &applied)
	if applied["status"] != string(memstore.MemoryReviewApplied) {
		t.Fatalf("review approve = %+v", applied)
	}
}

func TestMemoryIndexerSkipsSensitiveAndInactiveItems(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "preferences.md"), []byte(strings.TrimSpace(`
---
scope: user
type: preference
---

# Preferences

<!-- memory:item id="mem_active" type="preference" status="active" -->
- 用户偏好中文回答。
<!-- /memory:item -->

<!-- memory:item id="mem_secret" type="fact" status="active" -->
- api_key=secret-value
<!-- /memory:item -->

<!-- memory:item id="mem_archived" type="fact" status="archived" -->
- archived content
<!-- /memory:item -->
`)), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	index := &capturingVectorIndex{}
	store := memstore.NewStore(root, &memstore.Indexer{
		Collection: "memory_chunks",
		Index:      index,
		Embedder:   kb.FakeEmbedder{Dimensions: 16},
	})
	if err := store.Reindex(context.Background()); err != nil {
		t.Fatalf("Reindex() error = %v", err)
	}
	if len(index.chunks) != 1 {
		t.Fatalf("indexed chunks = %+v", index.chunks)
	}
	if strings.Contains(index.chunks[0].Text, "secret-value") {
		t.Fatalf("secret leaked into vector payload: %+v", index.chunks[0])
	}
}

func TestPlannerMemoryGovernanceIntents(t *testing.T) {
	planner := agent.HeuristicPlanner{}
	plan, err := planner.Plan(context.Background(), agent.PlanInput{
		ConversationID: "conv_1",
		UserMessage:    "记住我喜欢中文回答",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.ToolProposal == nil || plan.ToolProposal.Tool != "memory.extract_candidates" {
		t.Fatalf("expected memory.extract_candidates, got %+v", plan.ToolProposal)
	}
	if queue, _ := plan.ToolProposal.Input["queue"].(bool); !queue {
		t.Fatalf("expected normal memory extraction to queue review")
	}

	secretPlan, err := planner.Plan(context.Background(), agent.PlanInput{
		ConversationID: "conv_1",
		UserMessage:    "记住 api_key=secret-value",
	})
	if err != nil {
		t.Fatalf("Plan(secret) error = %v", err)
	}
	if secretPlan.ToolProposal == nil || secretPlan.ToolProposal.Tool != "memory.extract_candidates" {
		t.Fatalf("expected sensitive memory extraction guard, got %+v", secretPlan.ToolProposal)
	}
	if queue, _ := secretPlan.ToolProposal.Input["queue"].(bool); queue {
		t.Fatalf("sensitive memory extraction must not queue review")
	}

	archivePlan, err := planner.Plan(context.Background(), agent.PlanInput{
		UserMessage: "忘记这条记忆 mem_123",
	})
	if err != nil {
		t.Fatalf("Plan(archive) error = %v", err)
	}
	if archivePlan.ToolProposal == nil || archivePlan.ToolProposal.Tool != "memory.item_archive" {
		t.Fatalf("expected memory.item_archive, got %+v", archivePlan.ToolProposal)
	}
}

func joinMessages(messages []*schema.Message) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		parts = append(parts, msg.Content)
	}
	return strings.Join(parts, "\n")
}

type capturingVectorIndex struct {
	chunks []kb.VectorChunk
}

func (m *capturingVectorIndex) EnsureCollections(_ context.Context) error {
	return nil
}

func (m *capturingVectorIndex) UpsertChunks(_ context.Context, _ string, chunks []kb.VectorChunk) error {
	m.chunks = append([]kb.VectorChunk(nil), chunks...)
	return nil
}

func (m *capturingVectorIndex) DeleteBySourceFile(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *capturingVectorIndex) Search(_ context.Context, _ string, _ string, _ map[string]any, _ int) ([]kb.VectorSearchResult, error) {
	return nil, nil
}

func (m *capturingVectorIndex) Health(_ context.Context) error {
	return nil
}
