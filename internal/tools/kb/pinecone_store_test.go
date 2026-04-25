package kb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPineconeVectorIndexUpsertSearchDelete(t *testing.T) {
	var sawUpsert bool
	var sawDelete bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("api-key"); got != "pinecone-key" {
			t.Fatalf("api-key header = %q", got)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/describe_index_stats":
			_, _ = w.Write([]byte(`{"dimension":2}`))
		case r.Method == http.MethodPost && r.URL.Path == "/vectors/upsert":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode upsert: %v", err)
			}
			if body["namespace"] != "kb_chunks" {
				t.Fatalf("namespace = %v, want kb_chunks", body["namespace"])
			}
			vectors := body["vectors"].([]any)
			metadata := vectors[0].(map[string]any)["metadata"].(map[string]any)
			if metadata["source_file"] != "kb/doc.md" || metadata["chunk_id"] != "chunk_1" {
				t.Fatalf("metadata = %+v", metadata)
			}
			sawUpsert = true
			_, _ = w.Write([]byte(`{"upsertedCount":1}`))
		case r.Method == http.MethodPost && r.URL.Path == "/query":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode query: %v", err)
			}
			filter := body["filter"].(map[string]any)
			if filter["kb_id"].(map[string]any)["$eq"] != "kb_1" {
				t.Fatalf("filter = %+v", filter)
			}
			_, _ = w.Write([]byte(`{
				"matches":[{
					"id":"chunk_1",
					"score":0.9,
					"metadata":{"text":"hello","chunk_id":"chunk_1","kb_id":"kb_1"}
				}]
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/vectors/delete":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode delete: %v", err)
			}
			filter := body["filter"].(map[string]any)
			if filter["source_file"].(map[string]any)["$eq"] != "kb/doc.md" {
				t.Fatalf("delete filter = %+v", filter)
			}
			sawDelete = true
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	index, err := NewPineconeVectorIndex(PineconeIndexConfig{
		IndexHost:          server.URL,
		APIKey:             "pinecone-key",
		EmbeddingDimension: 2,
		Namespaces:         map[string]string{"kb": "kb_chunks"},
	}, FakeEmbedder{Dimensions: 2}, server.Client())
	if err != nil {
		t.Fatalf("NewPineconeVectorIndex() error = %v", err)
	}
	if err := index.EnsureCollections(context.Background()); err != nil {
		t.Fatalf("EnsureCollections() error = %v", err)
	}
	if err := index.UpsertChunks(context.Background(), "kb_chunks", []VectorChunk{{
		ID:         "chunk_1",
		Text:       "hello",
		Vector:     []float32{0.1, 0.2},
		SourceFile: "kb/doc.md",
		Payload:    map[string]any{"chunk_id": "chunk_1", "kb_id": "kb_1"},
	}}); err != nil {
		t.Fatalf("UpsertChunks() error = %v", err)
	}
	results, err := index.Search(context.Background(), "kb_chunks", "hello", map[string]any{"kb_id": "kb_1"}, 3)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "chunk_1" || results[0].Text != "hello" {
		t.Fatalf("results = %+v, want chunk_1 hello", results)
	}
	if err := index.DeleteBySourceFile(context.Background(), "kb_chunks", "kb/doc.md"); err != nil {
		t.Fatalf("DeleteBySourceFile() error = %v", err)
	}
	if !sawUpsert || !sawDelete {
		t.Fatalf("sawUpsert=%v sawDelete=%v, want both true", sawUpsert, sawDelete)
	}
}
