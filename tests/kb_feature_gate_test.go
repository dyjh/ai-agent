package tests

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"local-agent/internal/api"
	"local-agent/internal/app"
	"local-agent/internal/config"
)

func TestKnowledgeBaseDisabledFeatureGate(t *testing.T) {
	cfg := bootstrapTestConfig(t)
	cfg.KB.Enabled = false
	cfg.KB.Provider = ""

	bootstrap, err := app.NewBootstrap(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	if bootstrap.Knowledge != nil {
		t.Fatalf("Knowledge service should be nil when KB is disabled")
	}
	if bootstrap.Runtime.ContextBuilder.Knowledge != nil {
		t.Fatalf("ContextBuilder should not receive a KB service when disabled")
	}
	if _, err := bootstrap.Registry.Spec("kb.search"); err == nil {
		t.Fatalf("kb.search should not be registered when KB is disabled")
	}

	server := httptest.NewServer(api.NewRouter(api.Dependencies{
		Config:    bootstrap.Config,
		Runtime:   bootstrap.Runtime,
		Knowledge: bootstrap.Knowledge,
		Store:     bootstrap.Store,
	}))
	defer server.Close()

	var health map[string]any
	mustRequestJSON(t, http.MethodGet, server.URL+"/v1/health", nil, &health)
	kbHealth := health["knowledge_base"].(map[string]any)
	if kbHealth["enabled"] != false || kbHealth["status"] != "disabled" {
		t.Fatalf("knowledge_base health = %+v, want disabled", kbHealth)
	}

	status, body := doJSON(t, http.MethodGet, server.URL+"/v1/kbs", nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("GET /v1/kbs status = %d body = %s, want 503", status, body)
	}
	var errPayload map[string]any
	if err := json.Unmarshal([]byte(body), &errPayload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if errPayload["code"] != "feature_disabled" {
		t.Fatalf("error code = %v, want feature_disabled", errPayload["code"])
	}
}

func TestKnowledgeBaseEnabledQdrantInitializes(t *testing.T) {
	qdrant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":true}`))
	}))
	defer qdrant.Close()

	cfg := bootstrapTestConfig(t)
	cfg.KB.Enabled = true
	cfg.KB.Provider = "qdrant"
	cfg.Qdrant.URL = qdrant.URL

	bootstrap, err := app.NewBootstrap(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	if bootstrap.Knowledge == nil {
		t.Fatalf("Knowledge service should be initialized")
	}
	if _, err := bootstrap.Registry.Spec("kb.search"); err != nil {
		t.Fatalf("kb.search should be registered: %v", err)
	}

	server := httptest.NewServer(api.NewRouter(api.Dependencies{
		Config:    bootstrap.Config,
		Runtime:   bootstrap.Runtime,
		Knowledge: bootstrap.Knowledge,
		Store:     bootstrap.Store,
	}))
	defer server.Close()

	var health map[string]any
	mustRequestJSON(t, http.MethodGet, server.URL+"/v1/health", nil, &health)
	kbHealth := health["knowledge_base"].(map[string]any)
	if kbHealth["enabled"] != true || kbHealth["provider"] != "qdrant" || kbHealth["status"] != "ok" {
		t.Fatalf("knowledge_base health = %+v, want qdrant ok", kbHealth)
	}
}

func bootstrapTestConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Database.URL = ""
	cfg.Memory.RootDir = t.TempDir()
	cfg.Events.JSONLRoot = t.TempDir()
	cfg.Events.AuditRoot = t.TempDir()
	cfg.Vector.Backend = config.VectorBackendMemory
	cfg.Vector.EmbeddingDimension = 16
	cfg.Vector.FallbackToMemory = true
	cfg.Qdrant.TimeoutSeconds = 1
	cfg.Qdrant.Collections = map[string]string{
		"kb":     "kb_chunks",
		"memory": "memory_chunks",
		"code":   "code_chunks",
	}
	return cfg
}
