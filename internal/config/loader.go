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
	EnvUseKonwageBase                    = "USE_KONWAGE_BASE"
	EnvKonwageBaseProvider               = "KONWAGE_BASE_PROVIDER"
	EnvLLMProvider                       = "LLM_PROVIDER"
	EnvOllamaBaseURL                     = "OLLAMA_BASE_URL"
	EnvOllamaModel                       = "OLLAMA_MODEL"
	EnvEmbeddingProvider                 = "EMBEDDING_PROVIDER"
	EnvEmbeddingModel                    = "EMBEDDING_MODEL"
	EnvQdrantCollectionKB                = "QDRANT_COLLECTION_KB"
	EnvQdrantCollectionMemory            = "QDRANT_COLLECTION_MEMORY"
	EnvQdrantCollectionCode              = "QDRANT_COLLECTION_CODE"
	EnvQdrantRecreateOnDimensionMismatch = "QDRANT_RECREATE_ON_DIMENSION_MISMATCH"
	EnvPineconeIndexHost                 = "PINECONE_INDEX_HOST"
	EnvPineconeAPIKey                    = "PINECONE_API_KEY"
	EnvPineconeNamespaceKB               = "PINECONE_NAMESPACE_KB"
	EnvPineconeNamespaceMemory           = "PINECONE_NAMESPACE_MEMORY"
	EnvPineconeNamespaceCode             = "PINECONE_NAMESPACE_CODE"
	EnvOpenAIKBBaseURL                   = "OPENAI_KB_BASE_URL"
	EnvOpenAIKBAPIKey                    = "OPENAI_KB_API_KEY"
	EnvOpenAIVectorStoreKB               = "OPENAI_VECTOR_STORE_KB"
	EnvOpenAIVectorStoreMemory           = "OPENAI_VECTOR_STORE_MEMORY"
	EnvOpenAIVectorStoreCode             = "OPENAI_VECTOR_STORE_CODE"
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
	if err := applyQdrantEnv(&cfg); err != nil {
		return cfg, err
	}
	applyPineconeEnv(&cfg)
	applyOpenAIKBEnv(&cfg)
	normalizeModelProviders(&cfg)
	normalizeKnowledgeProviders(&cfg)
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
	if cfg.Pinecone.TimeoutSeconds <= 0 {
		cfg.Pinecone.TimeoutSeconds = Default().Pinecone.TimeoutSeconds
	}
	if cfg.Pinecone.Namespaces == nil {
		cfg.Pinecone.Namespaces = Default().Pinecone.Namespaces
	}
	if cfg.OpenAIKB.TimeoutSeconds <= 0 {
		cfg.OpenAIKB.TimeoutSeconds = Default().OpenAIKB.TimeoutSeconds
	}
	if strings.TrimSpace(cfg.OpenAIKB.BaseURL) == "" {
		cfg.OpenAIKB.BaseURL = Default().OpenAIKB.BaseURL
	}
	if cfg.OpenAIKB.VectorStores == nil {
		cfg.OpenAIKB.VectorStores = map[string]string{}
	}
	if cfg.Embeddings.TimeoutSeconds <= 0 {
		cfg.Embeddings.TimeoutSeconds = Default().Embeddings.TimeoutSeconds
	}
	cfg.Planner.Mode = strings.ToLower(strings.TrimSpace(cfg.Planner.Mode))
	if cfg.Planner.Mode == "" {
		cfg.Planner.Mode = Default().Planner.Mode
	}
	if cfg.Planner.MaxRetries <= 0 {
		cfg.Planner.MaxRetries = Default().Planner.MaxRetries
	}
	if strings.TrimSpace(cfg.Planner.ChatGate.Mode) == "" {
		cfg.Planner.ChatGate.Mode = Default().Planner.ChatGate.Mode
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

func applyQdrantEnv(cfg *Config) error {
	setCollection := func(scope, envName string) {
		raw, ok := os.LookupEnv(envName)
		if !ok {
			return
		}
		name := strings.TrimSpace(raw)
		if name == "" {
			return
		}
		if cfg.Qdrant.Collections == nil {
			cfg.Qdrant.Collections = map[string]string{}
		}
		cfg.Qdrant.Collections[scope] = name
	}
	setCollection("kb", EnvQdrantCollectionKB)
	setCollection("memory", EnvQdrantCollectionMemory)
	setCollection("code", EnvQdrantCollectionCode)
	if raw, ok := os.LookupEnv(EnvQdrantRecreateOnDimensionMismatch); ok {
		enabled, err := parseBool(raw)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", EnvQdrantRecreateOnDimensionMismatch, err)
		}
		cfg.Qdrant.RecreateOnDimensionMismatch = enabled
	}
	return nil
}

func applyPineconeEnv(cfg *Config) {
	if raw, ok := os.LookupEnv(EnvPineconeIndexHost); ok {
		cfg.Pinecone.IndexHost = raw
	}
	if raw, ok := os.LookupEnv(EnvPineconeAPIKey); ok {
		cfg.Pinecone.APIKey = raw
	}
	setNamespace := func(scope, envName string) {
		raw, ok := os.LookupEnv(envName)
		if !ok {
			return
		}
		name := strings.TrimSpace(raw)
		if name == "" {
			return
		}
		if cfg.Pinecone.Namespaces == nil {
			cfg.Pinecone.Namespaces = map[string]string{}
		}
		cfg.Pinecone.Namespaces[scope] = name
	}
	setNamespace("kb", EnvPineconeNamespaceKB)
	setNamespace("memory", EnvPineconeNamespaceMemory)
	setNamespace("code", EnvPineconeNamespaceCode)
}

func applyOpenAIKBEnv(cfg *Config) {
	if raw, ok := os.LookupEnv(EnvOpenAIKBBaseURL); ok {
		cfg.OpenAIKB.BaseURL = raw
	}
	if raw, ok := os.LookupEnv(EnvOpenAIKBAPIKey); ok {
		cfg.OpenAIKB.APIKey = raw
	}
	setStore := func(scope, envName string) {
		raw, ok := os.LookupEnv(envName)
		if !ok {
			return
		}
		id := strings.TrimSpace(raw)
		if id == "" {
			return
		}
		if cfg.OpenAIKB.VectorStores == nil {
			cfg.OpenAIKB.VectorStores = map[string]string{}
		}
		cfg.OpenAIKB.VectorStores[scope] = id
	}
	setStore("kb", EnvOpenAIVectorStoreKB)
	setStore("memory", EnvOpenAIVectorStoreMemory)
	setStore("code", EnvOpenAIVectorStoreCode)
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

func normalizeKnowledgeProviders(cfg *Config) {
	cfg.KB.Provider = strings.ToLower(strings.TrimSpace(cfg.KB.Provider))
	cfg.Pinecone.IndexHost = strings.TrimSpace(cfg.Pinecone.IndexHost)
	cfg.Pinecone.APIKey = strings.TrimSpace(cfg.Pinecone.APIKey)
	cfg.OpenAIKB.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.OpenAIKB.BaseURL), "/")
	cfg.OpenAIKB.APIKey = strings.TrimSpace(cfg.OpenAIKB.APIKey)
	if cfg.OpenAIKB.BaseURL == "" {
		cfg.OpenAIKB.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.OpenAIKB.APIKey == "" && strings.Contains(cfg.OpenAIKB.BaseURL, "openai.com") {
		cfg.OpenAIKB.APIKey = cfg.LLM.APIKey
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
