package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"local-agent/internal/config"
)

// ConfigCheckReport is the non-secret startup check summary.
type ConfigCheckReport struct {
	Status             string `json:"status"`
	Knowledge          string `json:"knowledge_base"`
	Provider           string `json:"provider,omitempty"`
	LLMProvider        string `json:"llm_provider,omitempty"`
	LLMModel           string `json:"llm_model,omitempty"`
	EmbeddingProvider  string `json:"embedding_provider,omitempty"`
	EmbeddingModel     string `json:"embedding_model,omitempty"`
	EmbeddingDimension int    `json:"embedding_dimension,omitempty"`
	DocsRoute          string `json:"docs_route"`
	OpenAPIRoute       string `json:"openapi_route"`
}

// CheckConfig validates config values and creates local directories that are
// safe for the single-user runtime to own.
func CheckConfig(_ context.Context, cfg config.Config, logger *slog.Logger) (ConfigCheckReport, error) {
	report := ConfigCheckReport{
		Status:             "ok",
		Knowledge:          "disabled",
		Provider:           cfg.KB.Provider,
		LLMProvider:        cfg.LLM.Provider,
		LLMModel:           cfg.LLM.Model,
		EmbeddingProvider:  cfg.Embeddings.Provider,
		EmbeddingModel:     cfg.Embeddings.Model,
		EmbeddingDimension: cfg.Vector.EmbeddingDimension,
		DocsRoute:          "/swagger/index.html",
		OpenAPIRoute:       "/swagger/doc.json",
	}
	if cfg.KB.Enabled {
		report.Knowledge = "enabled"
	}

	if err := config.ValidateRuntime(cfg); err != nil {
		report.Status = "error"
		return report, err
	}
	for _, dir := range []string{
		cfg.Memory.RootDir,
		cfg.Events.JSONLRoot,
		cfg.Events.AuditRoot,
		"skills",
		filepath.Dir(cfg.KB.RegistryPath),
	} {
		if dir == "" || dir == "." {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			report.Status = "error"
			return report, fmt.Errorf("create runtime directory: %w", err)
		}
	}

	for _, path := range []string{
		"config/policy.yaml",
		"config/policy.profiles.yaml",
		"config/skills.registry.yaml",
		"config/mcp.servers.yaml",
		"config/mcp.tool-policies.yaml",
	} {
		if _, err := os.Stat(resolveConfigPath(path)); err != nil {
			report.Status = "error"
			return report, fmt.Errorf("required config file %s: %w", path, err)
		}
	}

	if logger != nil {
		logger.Info("config check result",
			"status", report.Status,
			"knowledge_base", report.Knowledge,
			"provider", report.Provider,
			"llm_provider", report.LLMProvider,
			"embedding_provider", report.EmbeddingProvider,
			"embedding_model", report.EmbeddingModel,
			"docs_route", report.DocsRoute,
		)
	}
	return report, nil
}
