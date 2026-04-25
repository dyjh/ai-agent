package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ValidateRuntime checks config values that must be coherent before starting
// the local server. It does not print secret-bearing values.
func ValidateRuntime(cfg Config) error {
	var errs []error
	if strings.TrimSpace(cfg.Database.URL) == "" {
		errs = append(errs, errors.New("database.url is required"))
	}
	if strings.TrimSpace(cfg.Memory.RootDir) == "" {
		errs = append(errs, errors.New("memory.root_dir is required"))
	}
	if strings.TrimSpace(cfg.Events.JSONLRoot) == "" {
		errs = append(errs, errors.New("events.jsonl_root is required"))
	}
	if strings.TrimSpace(cfg.Events.AuditRoot) == "" {
		errs = append(errs, errors.New("events.audit_root is required"))
	}
	if cfg.Policy.MinConfidenceForAutoExecute <= 0 {
		errs = append(errs, errors.New("policy.min_confidence_for_auto_execute must be positive"))
	}
	if err := ValidatePolicyConfig(cfg.Policy); err != nil {
		errs = append(errs, err)
	}
	if err := ValidateKnowledgeBase(cfg); err != nil {
		errs = append(errs, err)
	}
	if err := ValidateModelProviders(cfg); err != nil {
		errs = append(errs, err)
	}
	if err := validateVector(cfg); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// ValidateKnowledgeBase enforces the runtime KB feature gate. The in-memory
// vector index remains available to package-level tests, but the configured
// runtime provider is intentionally qdrant-only.
func ValidateKnowledgeBase(cfg Config) error {
	if !cfg.KB.Enabled {
		return nil
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.KB.Provider))
	if provider == "" {
		return fmt.Errorf("%s is required when %s=true", EnvKonwageBaseProvider, EnvUseKonwageBase)
	}
	if provider != string(VectorBackendQdrant) {
		return fmt.Errorf("unsupported knowledge base provider: %s", provider)
	}
	if strings.TrimSpace(cfg.Qdrant.URL) == "" {
		return errors.New("qdrant.url is required when knowledge base provider is qdrant")
	}
	if _, err := url.ParseRequestURI(cfg.Qdrant.URL); err != nil {
		return errors.New("qdrant.url is invalid")
	}
	if strings.TrimSpace(cfg.KB.RegistryPath) == "" {
		return errors.New("kb.registry_path is required when knowledge base is enabled")
	}
	return nil
}

// ValidateModelProviders checks non-secret model provider configuration.
func ValidateModelProviders(cfg Config) error {
	var errs []error
	switch strings.ToLower(strings.TrimSpace(cfg.LLM.Provider)) {
	case "", ProviderMock:
	case ProviderOpenAICompatible:
		if strings.TrimSpace(cfg.LLM.Model) == "" && (strings.TrimSpace(cfg.LLM.BaseURL) != "" || strings.TrimSpace(cfg.LLM.APIKey) != "") {
			errs = append(errs, errors.New("llm.model is required when llm.provider=openai_compatible"))
		}
	case ProviderOllama:
		if strings.TrimSpace(cfg.LLM.Model) == "" {
			errs = append(errs, errors.New("llm.model is required when llm.provider=ollama"))
		}
		if strings.TrimSpace(cfg.LLM.BaseURL) == "" {
			errs = append(errs, errors.New("llm.base_url is required when llm.provider=ollama"))
		}
	default:
		errs = append(errs, fmt.Errorf("unsupported llm provider: %s", cfg.LLM.Provider))
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Embeddings.Provider)) {
	case "", ProviderFake:
	case ProviderOpenAICompatible:
		if strings.TrimSpace(cfg.Embeddings.Model) == "" {
			errs = append(errs, fmt.Errorf("%s is required when embeddings.provider=openai_compatible", EnvEmbeddingModel))
		}
		if strings.TrimSpace(cfg.Embeddings.BaseURL) == "" {
			errs = append(errs, errors.New("embeddings.base_url is required when embeddings.provider=openai_compatible"))
		}
	case ProviderOllama:
		if strings.TrimSpace(cfg.Embeddings.Model) == "" {
			errs = append(errs, fmt.Errorf("%s is required when embeddings.provider=ollama", EnvEmbeddingModel))
		}
		if strings.TrimSpace(cfg.Embeddings.BaseURL) == "" {
			errs = append(errs, errors.New("embeddings.base_url is required when embeddings.provider=ollama"))
		}
	default:
		errs = append(errs, fmt.Errorf("unsupported embeddings provider: %s", cfg.Embeddings.Provider))
	}
	if cfg.Embeddings.TimeoutSeconds < 0 {
		errs = append(errs, errors.New("embeddings.timeout_seconds must be non-negative"))
	}
	return errors.Join(errs...)
}

// ValidatePolicyConfig checks policy profiles and network policy coherence.
func ValidatePolicyConfig(policy PolicyConfig) error {
	var errs []error
	normalized := NormalizePolicy(policy)
	if strings.TrimSpace(normalized.ActiveProfile) == "" {
		errs = append(errs, errors.New("policy.active_profile is required"))
	} else if _, ok := normalized.Profiles[normalized.ActiveProfile]; !ok {
		errs = append(errs, fmt.Errorf("policy active profile %q is not defined", normalized.ActiveProfile))
	}
	for name, profile := range normalized.Profiles {
		if strings.TrimSpace(name) == "" {
			errs = append(errs, errors.New("policy profile name is required"))
		}
		if profile.MinConfidenceForAutoExecute <= 0 {
			errs = append(errs, fmt.Errorf("policy profile %s min_confidence_for_auto_execute must be positive", name))
		}
	}
	if normalized.Network.MaxDownloadBytes <= 0 {
		errs = append(errs, errors.New("policy.network_policy.max_download_bytes must be positive"))
	}
	return errors.Join(errs...)
}

func validateVector(cfg Config) error {
	switch cfg.Vector.Backend {
	case "", VectorBackendMemory, VectorBackendQdrant:
	default:
		return fmt.Errorf("unsupported vector backend: %s", cfg.Vector.Backend)
	}
	if cfg.Vector.EmbeddingDimension <= 0 {
		return errors.New("vector.embedding_dimension must be positive")
	}
	if cfg.Qdrant.TimeoutSeconds <= 0 {
		return errors.New("qdrant.timeout_seconds must be positive")
	}
	return nil
}
