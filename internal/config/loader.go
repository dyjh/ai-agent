package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// EnvUseKonwageBase intentionally preserves the requested external spelling.
	EnvUseKonwageBase      = "USE_KONWAGE_BASE"
	EnvKonwageBaseProvider = "KONWAGE_BASE_PROVIDER"
	EnvLLMProvider         = "LLM_PROVIDER"
	EnvOllamaBaseURL       = "OLLAMA_BASE_URL"
	EnvOllamaModel         = "OLLAMA_MODEL"
	EnvEmbeddingProvider   = "EMBEDDING_PROVIDER"
	EnvEmbeddingModel      = "EMBEDDING_MODEL"
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
	if err := loadPolicySidecars(path, &cfg); err != nil {
		return cfg, err
	}

	if err := applyKnowledgeBaseEnv(&cfg); err != nil {
		return cfg, err
	}
	normalizeModelProviders(&cfg)
	cfg.Policy = NormalizePolicy(cfg.Policy)
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
	if cfg.Embeddings.TimeoutSeconds <= 0 {
		cfg.Embeddings.TimeoutSeconds = Default().Embeddings.TimeoutSeconds
	}

	return cfg, nil
}

func loadPolicySidecars(configPath string, cfg *Config) error {
	if cfg == nil {
		return nil
	}
	dir := filepath.Dir(configPath)
	for _, name := range []string{"policy.yaml", "policy.profiles.yaml"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		expanded := os.ExpandEnv(string(data))
		var file struct {
			Policy        *PolicyConfig            `yaml:"policy"`
			Profiles      map[string]PolicyProfile `yaml:"profiles"`
			NetworkPolicy *NetworkPolicy           `yaml:"network_policy"`
			ActiveProfile string                   `yaml:"active_profile"`
		}
		if err := yaml.Unmarshal([]byte(expanded), &file); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if file.Policy != nil {
			mergePolicyConfig(&cfg.Policy, *file.Policy)
		}
		if len(file.Profiles) > 0 {
			if cfg.Policy.Profiles == nil {
				cfg.Policy.Profiles = map[string]PolicyProfile{}
			}
			for profileName, profile := range file.Profiles {
				cfg.Policy.Profiles[profileName] = profile
			}
		}
		if file.NetworkPolicy != nil {
			cfg.Policy.Network = *file.NetworkPolicy
		}
		if file.ActiveProfile != "" {
			cfg.Policy.ActiveProfile = file.ActiveProfile
		}
	}
	return nil
}

func mergePolicyConfig(target *PolicyConfig, source PolicyConfig) {
	if source.MinConfidenceForAutoExecute > 0 {
		target.MinConfidenceForAutoExecute = source.MinConfidenceForAutoExecute
	}
	if len(source.SensitivePaths) > 0 {
		target.SensitivePaths = append([]string(nil), source.SensitivePaths...)
	}
	if source.ActiveProfile != "" {
		target.ActiveProfile = source.ActiveProfile
	}
	if len(source.Profiles) > 0 {
		if target.Profiles == nil {
			target.Profiles = map[string]PolicyProfile{}
		}
		for name, profile := range source.Profiles {
			target.Profiles[name] = profile
		}
	}
	if source.Network.MaxDownloadBytes > 0 || len(source.Network.AllowedDomains) > 0 || len(source.Network.DeniedDomains) > 0 {
		target.Network = source.Network
	}
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
	if raw, ok := os.LookupEnv(EnvLLMProvider); ok {
		cfg.LLM.Provider = raw
	}
	if raw, ok := os.LookupEnv(EnvEmbeddingProvider); ok {
		cfg.Embeddings.Provider = raw
	}
	if raw, ok := os.LookupEnv(EnvEmbeddingModel); ok {
		cfg.Embeddings.Model = raw
	}
	if strings.EqualFold(strings.TrimSpace(cfg.LLM.Provider), ProviderOllama) {
		if raw, ok := os.LookupEnv(EnvOllamaBaseURL); ok {
			cfg.LLM.BaseURL = raw
		}
		if raw, ok := os.LookupEnv(EnvOllamaModel); ok {
			cfg.LLM.Model = raw
		}
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Embeddings.Provider), ProviderOllama) && strings.TrimSpace(cfg.Embeddings.BaseURL) == "" {
		if raw, ok := os.LookupEnv(EnvOllamaBaseURL); ok {
			cfg.Embeddings.BaseURL = raw
		}
	}
	return nil
}

func normalizeModelProviders(cfg *Config) {
	cfg.LLM.Provider = strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
	cfg.Embeddings.Provider = strings.ToLower(strings.TrimSpace(cfg.Embeddings.Provider))
	cfg.LLM.BaseURL = strings.TrimSpace(cfg.LLM.BaseURL)
	cfg.LLM.APIKey = strings.TrimSpace(cfg.LLM.APIKey)
	cfg.LLM.Model = strings.TrimSpace(cfg.LLM.Model)
	cfg.Embeddings.BaseURL = strings.TrimSpace(cfg.Embeddings.BaseURL)
	cfg.Embeddings.APIKey = strings.TrimSpace(cfg.Embeddings.APIKey)
	cfg.Embeddings.Model = strings.TrimSpace(cfg.Embeddings.Model)

	if cfg.LLM.Provider == "" {
		if cfg.LLM.Model != "" || cfg.LLM.BaseURL != "" || cfg.LLM.APIKey != "" {
			cfg.LLM.Provider = ProviderOpenAICompatible
		} else {
			cfg.LLM.Provider = ProviderMock
		}
	}
	if cfg.LLM.Provider == ProviderOllama {
		if cfg.LLM.BaseURL == "" {
			cfg.LLM.BaseURL = "http://127.0.0.1:11434"
		}
	} else if cfg.LLM.Provider == ProviderOpenAICompatible && cfg.LLM.BaseURL == "" {
		cfg.LLM.BaseURL = "https://api.openai.com/v1"
	}

	if cfg.Embeddings.Provider == "" {
		if cfg.Embeddings.Model != "" || cfg.Embeddings.BaseURL != "" || cfg.Embeddings.APIKey != "" {
			cfg.Embeddings.Provider = ProviderOpenAICompatible
		} else {
			cfg.Embeddings.Provider = ProviderFake
		}
	}
	if cfg.Embeddings.Provider == ProviderOllama {
		if cfg.Embeddings.BaseURL == "" {
			if cfg.LLM.Provider == ProviderOllama && cfg.LLM.BaseURL != "" {
				cfg.Embeddings.BaseURL = cfg.LLM.BaseURL
			} else {
				cfg.Embeddings.BaseURL = "http://127.0.0.1:11434"
			}
		}
	} else if cfg.Embeddings.Provider == ProviderOpenAICompatible {
		if cfg.Embeddings.BaseURL == "" {
			cfg.Embeddings.BaseURL = cfg.LLM.BaseURL
		}
		if cfg.Embeddings.APIKey == "" {
			cfg.Embeddings.APIKey = cfg.LLM.APIKey
		}
	}
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
