package kb

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"local-agent/internal/config"
)

func TestVectorFactoryMemoryBackend(t *testing.T) {
	cfg := config.Default()
	cfg.Vector.Backend = config.VectorBackendMemory
	cfg.Vector.EmbeddingDimension = 16

	index, err := NewVectorIndexFactory(slog.New(slog.NewTextHandler(io.Discard, nil))).NewVectorIndex(context.Background(), cfg, FakeEmbedder{Dimensions: 16})
	if err != nil {
		t.Fatalf("NewVectorIndex() error = %v", err)
	}
	if _, ok := index.(*InMemoryVectorIndex); !ok {
		t.Fatalf("expected in-memory index, got %T", index)
	}
}

func TestVectorFactoryQdrantBackend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case r.Method == http.MethodGet && len(r.URL.Path) > len("/collections/"):
			http.NotFound(w, r)
		case r.Method == http.MethodPut && len(r.URL.Path) > len("/collections/"):
			_, _ = w.Write([]byte(`{"status":"ok","result":true}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Vector.Backend = config.VectorBackendQdrant
	cfg.Vector.EmbeddingDimension = 16
	cfg.Qdrant.URL = server.URL

	index, err := NewVectorIndexFactory(slog.New(slog.NewTextHandler(io.Discard, nil))).NewVectorIndex(context.Background(), cfg, FakeEmbedder{Dimensions: 16})
	if err != nil {
		t.Fatalf("NewVectorIndex() error = %v", err)
	}
	if _, ok := index.(*QdrantVectorIndex); !ok {
		t.Fatalf("expected qdrant index, got %T", index)
	}
}

func TestVectorFactoryFallbackToMemory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Vector.Backend = config.VectorBackendQdrant
	cfg.Vector.FallbackToMemory = true
	cfg.Vector.EmbeddingDimension = 16
	cfg.Qdrant.URL = server.URL

	index, err := NewVectorIndexFactory(slog.New(slog.NewTextHandler(io.Discard, nil))).NewVectorIndex(context.Background(), cfg, FakeEmbedder{Dimensions: 16})
	if err != nil {
		t.Fatalf("NewVectorIndex() error = %v", err)
	}
	memoryIndex, ok := index.(*InMemoryVectorIndex)
	if !ok {
		t.Fatalf("expected fallback in-memory index, got %T", index)
	}
	if memoryIndex.Status().FallbackReason == "" {
		t.Fatalf("expected fallback reason to be recorded")
	}
}

func TestVectorFactoryQdrantNoFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Vector.Backend = config.VectorBackendQdrant
	cfg.Vector.FallbackToMemory = false
	cfg.Vector.EmbeddingDimension = 16
	cfg.Qdrant.URL = server.URL

	_, err := NewVectorIndexFactory(slog.New(slog.NewTextHandler(io.Discard, nil))).NewVectorIndex(context.Background(), cfg, FakeEmbedder{Dimensions: 16})
	if err == nil {
		t.Fatalf("expected qdrant failure without fallback")
	}
}
