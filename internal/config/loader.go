package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// EnvUseKonwageBase intentionally preserves the requested external spelling.
	EnvUseKonwageBase      = "USE_KONWAGE_BASE"
	EnvKonwageBaseProvider = "KONWAGE_BASE_PROVIDER"
)

// Load loads YAML config from disk and expands environment variables.
func Load(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}

	expanded := os.ExpandEnv(string(data))
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return cfg, err
	}

	if err := applyKnowledgeBaseEnv(&cfg); err != nil {
		return cfg, err
	}
	cfg.Vector.Backend = VectorBackend(strings.ToLower(string(cfg.Vector.Backend)))
	if cfg.Vector.Backend == "" {
		cfg.Vector.Backend = Default().Vector.Backend
	}
	cfg.KB.Provider = strings.ToLower(strings.TrimSpace(cfg.KB.Provider))
	cfg.Vector.Distance = strings.ToLower(strings.TrimSpace(cfg.Vector.Distance))
	if cfg.Vector.Distance == "" {
		cfg.Vector.Distance = Default().Vector.Distance
	}
	if cfg.Vector.EmbeddingDimension <= 0 {
		cfg.Vector.EmbeddingDimension = Default().Vector.EmbeddingDimension
	}
	if cfg.Qdrant.TimeoutSeconds <= 0 {
		cfg.Qdrant.TimeoutSeconds = Default().Qdrant.TimeoutSeconds
	}
	if cfg.Qdrant.Collections == nil {
		cfg.Qdrant.Collections = Default().Qdrant.Collections
	}

	return cfg, nil
}

func applyKnowledgeBaseEnv(cfg *Config) error {
	if raw, ok := os.LookupEnv(EnvUseKonwageBase); ok {
		enabled, err := parseBool(raw)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", EnvUseKonwageBase, err)
		}
		cfg.KB.Enabled = enabled
	}
	if raw, ok := os.LookupEnv(EnvKonwageBaseProvider); ok {
		cfg.KB.Provider = raw
	}
	return nil
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("must be a boolean")
	}
}
