package kb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"local-agent/internal/config"
)

func TestHTTPEmbedderOpenAICompatible(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path = %s, want /v1/embeddings", r.URL.Path)
		}
		if got := r.Header.Get("authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float32{0.1, 0.2}}},
		})
	}))
	defer server.Close()

	embedder, err := NewEmbedder(config.EmbeddingConfig{
		Provider: config.ProviderOpenAICompatible,
		BaseURL:  server.URL + "/v1",
		APIKey:   "test-key",
		Model:    "text-embedding-test",
	}, 2)
	if err != nil {
		t.Fatalf("NewEmbedder() error = %v", err)
	}
	vector, err := embedder.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(vector) != 2 {
		t.Fatalf("len(vector) = %d, want 2", len(vector))
	}
}

func TestHTTPEmbedderOllamaNative(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Fatalf("path = %s, want /api/embed", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.1, 0.2, 0.3}},
		})
	}))
	defer server.Close()

	embedder, err := NewEmbedder(config.EmbeddingConfig{
		Provider: config.ProviderOllama,
		BaseURL:  server.URL,
		Model:    "nomic-embed-text",
	}, 3)
	if err != nil {
		t.Fatalf("NewEmbedder() error = %v", err)
	}
	vector, err := embedder.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(vector) != 3 {
		t.Fatalf("len(vector) = %d, want 3", len(vector))
	}
}

func TestHTTPEmbedderDimensionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float32{0.1}}},
		})
	}))
	defer server.Close()

	embedder, err := NewEmbedder(config.EmbeddingConfig{
		Provider: config.ProviderOpenAICompatible,
		BaseURL:  server.URL,
		Model:    "text-embedding-test",
	}, 2)
	if err != nil {
		t.Fatalf("NewEmbedder() error = %v", err)
	}
	if _, err := embedder.Embed(context.Background(), "hello"); err == nil {
		t.Fatalf("Embed() error = nil, want dimension mismatch")
	}
}
