package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local-agent/internal/config"
)

func TestConfigValidationRequiresDatabaseURL(t *testing.T) {
	cfg := validRuntimeConfig(t)
	cfg.Database.URL = ""
	if err := config.ValidateRuntime(cfg); err == nil || !strings.Contains(err.Error(), "database.url") {
		t.Fatalf("ValidateRuntime() error = %v, want database.url error", err)
	}
}

func TestConfigLoadExpandsEnvAndDisablesKnowledgeBase(t *testing.T) {
	t.Setenv(config.EnvUseKonwageBase, "false")
	t.Setenv(config.EnvKonwageBaseProvider, "")
	t.Setenv("DATABASE_URL", "postgresql://agent:agent@localhost:5432/local_agent")
	t.Setenv("QDRANT_URL", "")

	cfg, err := config.Load(writeConfigFixture(t))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.KB.Enabled {
		t.Fatalf("KB enabled = true, want false")
	}
	if cfg.Database.URL == "" {
		t.Fatalf("expected DATABASE_URL expansion")
	}
	if err := config.ValidateRuntime(cfg); err != nil {
		t.Fatalf("ValidateRuntime() with disabled KB error = %v", err)
	}
}

func TestConfigLoadEnablesQdrantKnowledgeBase(t *testing.T) {
	t.Setenv(config.EnvUseKonwageBase, "true")
	t.Setenv(config.EnvKonwageBaseProvider, "qdrant")
	t.Setenv("DATABASE_URL", "postgresql://agent:agent@localhost:5432/local_agent")
	t.Setenv("QDRANT_URL", "http://localhost:6333")

	cfg, err := config.Load(writeConfigFixture(t))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.KB.Enabled || cfg.KB.Provider != "qdrant" {
		t.Fatalf("KB config = enabled:%v provider:%q, want true/qdrant", cfg.KB.Enabled, cfg.KB.Provider)
	}
	if err := config.ValidateRuntime(cfg); err != nil {
		t.Fatalf("ValidateRuntime() error = %v", err)
	}
}

func TestConfigLoadSupportsOllamaAndEmbeddingModelEnv(t *testing.T) {
	t.Setenv(config.EnvUseKonwageBase, "false")
	t.Setenv(config.EnvKonwageBaseProvider, "")
	t.Setenv(config.EnvLLMProvider, config.ProviderOllama)
	t.Setenv(config.EnvOllamaBaseURL, "http://localhost:11434")
	t.Setenv(config.EnvOllamaModel, "qwen2.5-coder:7b")
	t.Setenv(config.EnvEmbeddingProvider, config.ProviderOllama)
	t.Setenv(config.EnvEmbeddingModel, "nomic-embed-text")
	t.Setenv("DATABASE_URL", "postgresql://agent:agent@localhost:5432/local_agent")

	cfg, err := config.Load(writeConfigFixture(t))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLM.Provider != config.ProviderOllama || cfg.LLM.BaseURL != "http://localhost:11434" || cfg.LLM.Model != "qwen2.5-coder:7b" {
		t.Fatalf("LLM config = provider:%q base:%q model:%q", cfg.LLM.Provider, cfg.LLM.BaseURL, cfg.LLM.Model)
	}
	if cfg.Embeddings.Provider != config.ProviderOllama || cfg.Embeddings.Model != "nomic-embed-text" {
		t.Fatalf("embedding config = provider:%q model:%q", cfg.Embeddings.Provider, cfg.Embeddings.Model)
	}
	if cfg.Embeddings.BaseURL != "http://localhost:11434" {
		t.Fatalf("embedding base_url = %q, want ollama base url", cfg.Embeddings.BaseURL)
	}
	if err := config.ValidateRuntime(cfg); err != nil {
		t.Fatalf("ValidateRuntime() error = %v", err)
	}
}

func TestConfigValidationRejectsInvalidKnowledgeProvider(t *testing.T) {
	cfg := validRuntimeConfig(t)
	cfg.KB.Enabled = true
	cfg.KB.Provider = "memory"
	if err := config.ValidateRuntime(cfg); err == nil || !strings.Contains(err.Error(), "unsupported knowledge base provider") {
		t.Fatalf("ValidateRuntime() error = %v, want unsupported provider", err)
	}
}

func TestConfigValidationRequiresProviderWhenKnowledgeEnabled(t *testing.T) {
	cfg := validRuntimeConfig(t)
	cfg.KB.Enabled = true
	cfg.KB.Provider = ""
	if err := config.ValidateRuntime(cfg); err == nil || !strings.Contains(err.Error(), config.EnvKonwageBaseProvider) {
		t.Fatalf("ValidateRuntime() error = %v, want provider required", err)
	}
}

func TestConfigValidationRejectsInvalidModelProviders(t *testing.T) {
	cfg := validRuntimeConfig(t)
	cfg.LLM.Provider = "other"
	cfg.Embeddings.Provider = "other"
	if err := config.ValidateRuntime(cfg); err == nil || !strings.Contains(err.Error(), "unsupported llm provider") || !strings.Contains(err.Error(), "unsupported embeddings provider") {
		t.Fatalf("ValidateRuntime() error = %v, want unsupported model provider errors", err)
	}
}

func TestConfigValidationRequiresEmbeddingModelForConfiguredProvider(t *testing.T) {
	cfg := validRuntimeConfig(t)
	cfg.Embeddings.Provider = config.ProviderOpenAICompatible
	cfg.Embeddings.BaseURL = "http://localhost:9999/v1"
	cfg.Embeddings.Model = ""
	if err := config.ValidateRuntime(cfg); err == nil || !strings.Contains(err.Error(), config.EnvEmbeddingModel) {
		t.Fatalf("ValidateRuntime() error = %v, want embedding model error", err)
	}
}

func validRuntimeConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Database.URL = "postgresql://agent:agent@localhost:5432/local_agent"
	cfg.Memory.RootDir = filepath.Join(t.TempDir(), "memory")
	cfg.Events.JSONLRoot = filepath.Join(t.TempDir(), "runs")
	cfg.Events.AuditRoot = filepath.Join(t.TempDir(), "audit")
	cfg.KB.Enabled = false
	cfg.KB.Provider = ""
	cfg.Vector.Backend = config.VectorBackendMemory
	cfg.Vector.EmbeddingDimension = 16
	return cfg
}

func writeConfigFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.yaml")
	data := []byte(`server:
  host: 127.0.0.1
  port: 8765
database:
  url: ${DATABASE_URL}
kb:
  enabled: ${USE_KONWAGE_BASE}
  provider: ${KONWAGE_BASE_PROVIDER}
  registry_path: ./knowledge/registry.yaml
vector:
  backend: memory
  fallback_to_memory: true
  embedding_dimension: 16
  distance: cosine
qdrant:
  url: ${QDRANT_URL}
  timeout_seconds: 10
  collections:
    kb: kb_chunks
    memory: memory_chunks
memory:
  root_dir: ./memory
events:
  jsonl_root: ./runs
  audit_root: ./audit
shell:
  enabled: true
  default_timeout_seconds: 60
  max_output_chars: 20000
llm:
  provider: ${LLM_PROVIDER}
  base_url: ${OPENAI_BASE_URL}
  api_key: ${OPENAI_API_KEY}
  model: ${OPENAI_MODEL}
embeddings:
  provider: ${EMBEDDING_PROVIDER}
  base_url: ${EMBEDDING_BASE_URL}
  api_key: ${EMBEDDING_API_KEY}
  model: ${EMBEDDING_MODEL}
  timeout_seconds: 30
policy:
  min_confidence_for_auto_execute: 0.85
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
