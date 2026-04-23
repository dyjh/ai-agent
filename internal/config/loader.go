package config

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
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

	cfg.Vector.Backend = VectorBackend(strings.ToLower(string(cfg.Vector.Backend)))
	if cfg.Vector.Backend == "" {
		cfg.Vector.Backend = Default().Vector.Backend
	}
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
