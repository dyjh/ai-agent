package kb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIVectorStoreIndexUpsertSearchDelete(t *testing.T) {
	var attached bool
	var deletedVectorFile bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer openai-key" {
			t.Fatalf("authorization header = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/vector_stores/vs_kb":
			_, _ = w.Write([]byte(`{"id":"vs_kb"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/models":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/files":
			if !strings.HasPrefix(r.Header.Get("content-type"), "multipart/form-data") {
				t.Fatalf("content-type = %q, want multipart", r.Header.Get("content-type"))
			}
			_, _ = w.Write([]byte(`{"id":"file_1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/vector_stores/vs_kb/files":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode attach: %v", err)
			}
			if body["file_id"] != "file_1" {
				t.Fatalf("file_id = %v, want file_1", body["file_id"])
			}
			attrs := body["attributes"].(map[string]any)
			if attrs["source_file"] != "kb/doc.md" || attrs["chunk_id"] != "chunk_1" {
				t.Fatalf("attributes = %+v", attrs)
			}
			attached = true
			_, _ = w.Write([]byte(`{"id":"file_1","status":"in_progress"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/vector_stores/vs_kb/files/file_1":
			_, _ = w.Write([]byte(`{"id":"file_1","status":"completed"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/vector_stores/vs_kb/search":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode search: %v", err)
			}
			if body["query"] != "hello" {
				t.Fatalf("query = %v, want hello", body["query"])
			}
			if body["filters"] == nil {
				t.Fatalf("filters missing")
			}
			_, _ = w.Write([]byte(`{
				"data":[{
					"file_id":"file_1",
					"filename":"doc.md",
					"score":0.95,
					"attributes":{"chunk_id":"chunk_1","kb_id":"kb_1","source_file":"kb/doc.md"},
					"content":[{"type":"text","text":"hello world"}]
				}]
			}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/vector_stores/vs_kb/files/file_1":
			deletedVectorFile = true
			_, _ = w.Write([]byte(`{"deleted":true}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/files/file_1":
			_, _ = w.Write([]byte(`{"deleted":true}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	index, err := NewOpenAIVectorStoreIndex(OpenAIVectorStoreConfig{
		BaseURL:        server.URL,
		APIKey:         "openai-key",
		TimeoutSeconds: 2,
		VectorStores:   map[string]string{"kb": "vs_kb"},
	}, server.Client())
	if err != nil {
		t.Fatalf("NewOpenAIVectorStoreIndex() error = %v", err)
	}
	if err := index.EnsureCollections(context.Background()); err != nil {
		t.Fatalf("EnsureCollections() error = %v", err)
	}
	if err := index.Health(context.Background()); err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if err := index.UpsertChunks(context.Background(), "vs_kb", []VectorChunk{{
		ID:         "chunk_1",
		Text:       "hello world",
		Vector:     []float32{0.1, 0.2},
		SourceFile: "kb/doc.md",
		Payload:    map[string]any{"chunk_id": "chunk_1", "kb_id": "kb_1"},
	}}); err != nil {
		t.Fatalf("UpsertChunks() error = %v", err)
	}
	results, err := index.Search(context.Background(), "vs_kb", "hello", map[string]any{"kb_id": "kb_1"}, 3)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "chunk_1" || results[0].Text != "hello world" {
		t.Fatalf("results = %+v, want chunk_1 hello world", results)
	}
	if err := index.DeleteBySourceFile(context.Background(), "vs_kb", "kb/doc.md"); err != nil {
		t.Fatalf("DeleteBySourceFile() error = %v", err)
	}
	if !attached || !deletedVectorFile {
		t.Fatalf("attached=%v deletedVectorFile=%v, want both true", attached, deletedVectorFile)
	}
}
