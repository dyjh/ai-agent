package kb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestQdrantEnsureCollections(t *testing.T) {
	var created bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/kb_chunks":
			http.NotFound(w, r)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/kb_chunks":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			vectors := body["vectors"].(map[string]any)
			if vectors["size"] != float64(16) {
				t.Fatalf("vector size = %v, want 16", vectors["size"])
			}
			if vectors["distance"] != "Cosine" {
				t.Fatalf("distance = %v, want Cosine", vectors["distance"])
			}
			created = true
			_, _ = w.Write([]byte(`{"status":"ok","result":true}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	index, err := NewQdrantVectorIndex(QdrantIndexConfig{
		URL:                server.URL,
		EmbeddingDimension: 16,
		Distance:           "cosine",
		Collections:        map[string]string{"kb": "kb_chunks"},
	}, FakeEmbedder{Dimensions: 16}, server.Client())
	if err != nil {
		t.Fatalf("NewQdrantVectorIndex() error = %v", err)
	}
	if err := index.EnsureCollections(context.Background()); err != nil {
		t.Fatalf("EnsureCollections() error = %v", err)
	}
	if !created {
		t.Fatalf("expected collection creation request")
	}
}

func TestQdrantEnsureCollectionsAcceptsConfiguredDimension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/kb_chunks":
			_, _ = w.Write([]byte(qdrantCollectionInfoResponse(16, 3)))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	index, err := NewQdrantVectorIndex(QdrantIndexConfig{
		URL:                server.URL,
		EmbeddingDimension: 16,
		Distance:           "cosine",
		Collections:        map[string]string{"kb": "kb_chunks"},
	}, FakeEmbedder{Dimensions: 16}, server.Client())
	if err != nil {
		t.Fatalf("NewQdrantVectorIndex() error = %v", err)
	}
	if err := index.EnsureCollections(context.Background()); err != nil {
		t.Fatalf("EnsureCollections() error = %v", err)
	}
}

func TestQdrantEnsureCollectionsRecreatesEmptyDimensionMismatch(t *testing.T) {
	var deleted bool
	var created bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/kb_chunks":
			_, _ = w.Write([]byte(qdrantCollectionInfoResponse(1536, 0)))
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/kb_chunks":
			deleted = true
			_, _ = w.Write([]byte(`{"status":"ok","result":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/collections/kb_chunks":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			vectors := body["vectors"].(map[string]any)
			if vectors["size"] != float64(1024) {
				t.Fatalf("vector size = %v, want 1024", vectors["size"])
			}
			created = true
			_, _ = w.Write([]byte(`{"status":"ok","result":true}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	index, err := NewQdrantVectorIndex(QdrantIndexConfig{
		URL:                server.URL,
		EmbeddingDimension: 1024,
		Distance:           "cosine",
		Collections:        map[string]string{"kb": "kb_chunks"},
	}, FakeEmbedder{Dimensions: 1024}, server.Client())
	if err != nil {
		t.Fatalf("NewQdrantVectorIndex() error = %v", err)
	}
	if err := index.EnsureCollections(context.Background()); err != nil {
		t.Fatalf("EnsureCollections() error = %v", err)
	}
	if !deleted || !created {
		t.Fatalf("deleted=%v created=%v, want both true", deleted, created)
	}
}

func TestQdrantEnsureCollectionsRejectsNonEmptyDimensionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/kb_chunks":
			_, _ = w.Write([]byte(qdrantCollectionInfoResponse(1536, 2)))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	index, err := NewQdrantVectorIndex(QdrantIndexConfig{
		URL:                server.URL,
		EmbeddingDimension: 1024,
		Distance:           "cosine",
		Collections:        map[string]string{"kb": "kb_chunks"},
	}, FakeEmbedder{Dimensions: 1024}, server.Client())
	if err != nil {
		t.Fatalf("NewQdrantVectorIndex() error = %v", err)
	}
	err = index.EnsureCollections(context.Background())
	if err == nil {
		t.Fatalf("EnsureCollections() error = nil, want dimension mismatch")
	}
	if !strings.Contains(err.Error(), `collection "kb_chunks"`) ||
		!strings.Contains(err.Error(), "configured=1024") ||
		!strings.Contains(err.Error(), "existing=1536") ||
		!strings.Contains(err.Error(), "points=2") {
		t.Fatalf("EnsureCollections() error = %v, want dimension mismatch details", err)
	}
}

func TestQdrantUpsertChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/collections/kb_chunks/points" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("api-key"); got != "secret" {
			t.Fatalf("api-key header = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upsert body: %v", err)
		}
		points := body["points"].([]any)
		point := points[0].(map[string]any)
		pointID, _ := point["id"].(string)
		if pointID == "chunk_1" {
			t.Fatalf("qdrant point id used raw chunk id")
		}
		if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(pointID) {
			t.Fatalf("qdrant point id = %q, want UUID", pointID)
		}
		payload := point["payload"].(map[string]any)
		if payload["source_file"] != "kb_1/intro.md" {
			t.Fatalf("source_file = %v", payload["source_file"])
		}
		if payload["chunk_id"] != "chunk_1" {
			t.Fatalf("chunk_id payload = %v, want chunk_1", payload["chunk_id"])
		}
		if payload["text"] != "hello world" {
			t.Fatalf("text = %v", payload["text"])
		}
		_, _ = w.Write([]byte(`{"status":"ok","result":{"status":"acknowledged"}}`))
	}))
	defer server.Close()

	index, err := NewQdrantVectorIndex(QdrantIndexConfig{
		URL:                server.URL,
		APIKey:             "secret",
		EmbeddingDimension: 16,
		Distance:           "cosine",
		Collections:        map[string]string{"kb": "kb_chunks"},
	}, FakeEmbedder{Dimensions: 16}, server.Client())
	if err != nil {
		t.Fatalf("NewQdrantVectorIndex() error = %v", err)
	}
	err = index.UpsertChunks(context.Background(), "kb_chunks", []VectorChunk{{
		ID:         "chunk_1",
		Text:       "hello world",
		Vector:     []float32{0.1, 0.2},
		SourceFile: "kb_1/intro.md",
		Payload: map[string]any{
			"chunk_id":     "chunk_1",
			"kb_id":        "kb_1",
			"document_id":  "doc_1",
			"content_hash": "hash",
			"ignored":      "drop-me",
		},
	}})
	if err != nil {
		t.Fatalf("UpsertChunks() error = %v", err)
	}
}

func qdrantCollectionInfoResponse(vectorSize int, pointsCount int64) string {
	return fmt.Sprintf(`{
		"status":"ok",
		"result":{
			"points_count":%d,
			"config":{
				"params":{
					"vectors":{"size":%d,"distance":"Cosine"}
				}
			}
		}
	}`, pointsCount, vectorSize)
}

func TestQdrantDeleteBySourceFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/kb_chunks/points/delete" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode delete body: %v", err)
		}
		filter := body["filter"].(map[string]any)
		must := filter["must"].([]any)
		match := must[0].(map[string]any)["match"].(map[string]any)
		if match["value"] != "kb_1/intro.md" {
			t.Fatalf("delete filter value = %v", match["value"])
		}
		_, _ = w.Write([]byte(`{"status":"ok","result":{"status":"acknowledged"}}`))
	}))
	defer server.Close()

	index, err := NewQdrantVectorIndex(QdrantIndexConfig{
		URL:                server.URL,
		EmbeddingDimension: 16,
		Distance:           "cosine",
		Collections:        map[string]string{"kb": "kb_chunks"},
	}, FakeEmbedder{Dimensions: 16}, server.Client())
	if err != nil {
		t.Fatalf("NewQdrantVectorIndex() error = %v", err)
	}
	if err := index.DeleteBySourceFile(context.Background(), "kb_chunks", "kb_1/intro.md"); err != nil {
		t.Fatalf("DeleteBySourceFile() error = %v", err)
	}
}

func TestQdrantSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/kb_chunks/points/query" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode search body: %v", err)
		}
		filter := body["filter"].(map[string]any)
		must := filter["must"].([]any)
		if len(must) != 2 {
			t.Fatalf("must filters = %d, want 2", len(must))
		}
		_, _ = w.Write([]byte(`{
			"status":"ok",
			"result":{
				"points":[
					{
						"id":"chunk_1",
						"score":0.99,
						"payload":{
							"text":"hello world",
							"kb_id":"kb_1",
							"source_file":"kb_1/intro.md"
						}
					}
				]
			}
		}`))
	}))
	defer server.Close()

	index, err := NewQdrantVectorIndex(QdrantIndexConfig{
		URL:                server.URL,
		EmbeddingDimension: 16,
		Distance:           "cosine",
		Collections:        map[string]string{"kb": "kb_chunks"},
	}, FakeEmbedder{Dimensions: 16}, server.Client())
	if err != nil {
		t.Fatalf("NewQdrantVectorIndex() error = %v", err)
	}
	results, err := index.Search(context.Background(), "kb_chunks", "hello", map[string]any{
		"kb_id":       "kb_1",
		"source_file": "kb_1/intro.md",
	}, 3)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].ID != "chunk_1" || results[0].Text != "hello world" {
		t.Fatalf("unexpected result: %+v", results[0])
	}
}

func TestQdrantHealthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	index, err := NewQdrantVectorIndex(QdrantIndexConfig{
		URL:                server.URL,
		EmbeddingDimension: 16,
		Distance:           "cosine",
		Collections:        map[string]string{"kb": "kb_chunks"},
	}, FakeEmbedder{Dimensions: 16}, server.Client())
	if err != nil {
		t.Fatalf("NewQdrantVectorIndex() error = %v", err)
	}
	err = index.Health(context.Background())
	if err == nil || !strings.Contains(err.Error(), "status=503") {
		t.Fatalf("Health() error = %v, want 503", err)
	}
}
