package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"local-agent/internal/core"
	"local-agent/internal/tools/kb"
	memstore "local-agent/internal/tools/memory"
)

func TestMarkdownMemoryStore(t *testing.T) {
	root := t.TempDir()
	seedFile := filepath.Join(root, "preferences.md")
	if err := os.WriteFile(seedFile, []byte("---\nkind: preferences\ntitle: Preferences\n---\n\n喜欢简洁回答\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	index := &mockVectorIndex{}
	store := memstore.NewStore(root, &memstore.Indexer{
		Collection: "memory_chunks",
		Index:      index,
		Embedder:   kb.FakeEmbedder{Dimensions: 16},
	})

	file, err := store.ReadFile("preferences.md")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if file.Frontmatter["kind"] != "preferences" {
		t.Fatalf("frontmatter kind = %s", file.Frontmatter["kind"])
	}

	if err := store.WriteFile(core.MemoryFile{
		Path:        "profile.md",
		Frontmatter: map[string]string{"kind": "profile"},
		Body:        "住在上海",
	}); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	patch, err := store.CreatePatch(core.MemoryPatch{
		Path:    "long_term.md",
		Summary: "记录长期记忆",
		Body:    "偏好使用 Go。",
	})
	if err != nil {
		t.Fatalf("CreatePatch() error = %v", err)
	}
	if patch.ID == "" {
		t.Fatalf("expected patch id")
	}

	if err := store.ApplyPatch(context.Background(), patch); err != nil {
		t.Fatalf("ApplyPatch() error = %v", err)
	}

	if index.upsertCalls == 0 {
		t.Fatalf("expected reindex to call vector index")
	}
}

type mockVectorIndex struct {
	upsertCalls int
}

func (m *mockVectorIndex) EnsureCollections(_ context.Context) error {
	return nil
}

func (m *mockVectorIndex) UpsertChunks(_ context.Context, _ string, _ []kb.VectorChunk) error {
	m.upsertCalls++
	return nil
}

func (m *mockVectorIndex) DeleteBySourceFile(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockVectorIndex) Search(_ context.Context, _ string, _ string, _ map[string]any, _ int) ([]kb.VectorSearchResult, error) {
	return nil, nil
}

func (m *mockVectorIndex) Health(_ context.Context) error {
	return nil
}
