package config

import "strings"

// Config is the root runtime configuration.
type Config struct {
	Server     ServerConfig    `yaml:"server"`
	Database   DatabaseConfig  `yaml:"database"`
	KB         KBConfig        `yaml:"kb"`
	Vector     VectorConfig    `yaml:"vector"`
	Qdrant     QdrantConfig    `yaml:"qdrant"`
	Pinecone   PineconeConfig  `yaml:"pinecone"`
	OpenAIKB   OpenAIKBConfig  `yaml:"openai_kb"`
	Owner      OwnerConfig     `yaml:"owner"`
	LLM        LLMConfig       `yaml:"llm"`
	Embeddings EmbeddingConfig `yaml:"embeddings"`
	Memory     MemoryConfig    `yaml:"memory"`
	Events     EventsConfig    `yaml:"events"`
	Shell      ShellConfig     `yaml:"shell"`
	Policy     PolicyConfig    `yaml:"policy"`
	Planner    PlannerConfig   `yaml:"planner"`
}

// ServerConfig stores HTTP listener settings.
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// DatabaseConfig stores PostgreSQL connectivity.
type DatabaseConfig struct {
	URL string `yaml:"url"`
}

// VectorBackend selects the vector index implementation.
type VectorBackend string

const (
	VectorBackendMemory   VectorBackend = "memory"
	VectorBackendQdrant   VectorBackend = "qdrant"
	VectorBackendPinecone VectorBackend = "pinecone"
	VectorBackendOpenAI   VectorBackend = "openai"
)

const (
	ProviderOpenAICompatible = "openai_compatible"
	ProviderOllama           = "ollama"
	ProviderMock             = "mock"
	ProviderFake             = "fake"
)

// KBConfig stores KB metadata configuration.
type KBConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Provider     string `yaml:"provider"`
	RegistryPath string `yaml:"registry_path"`
}

// VectorConfig stores vector backend selection and defaults.
type VectorConfig struct {
	Backend            VectorBackend `yaml:"backend"`
	FallbackToMemory   bool          `yaml:"fallback_to_memory"`
	EmbeddingDimension int           `yaml:"embedding_dimension"`
	Distance           string        `yaml:"distance"`
}

// QdrantConfig stores vector index configuration.
type QdrantConfig struct {
	URL                         string            `yaml:"url"`
	APIKey                      string            `yaml:"api_key"`
	TimeoutSeconds              int               `yaml:"timeout_seconds"`
	RecreateOnDimensionMismatch bool              `yaml:"recreate_on_dimension_mismatch"`
	Collections                 map[string]string `yaml:"collections"`
}

// PineconeConfig stores Pinecone vector index settings.
type PineconeConfig struct {
	IndexHost      string            `yaml:"index_host"`
	APIKey         string            `yaml:"api_key"`
	TimeoutSeconds int               `yaml:"timeout_seconds"`
	Namespaces     map[string]string `yaml:"namespaces"`
}

// OpenAIKBConfig stores OpenAI Vector Store settings.
type OpenAIKBConfig struct {
	BaseURL        string            `yaml:"base_url"`
	APIKey         string            `yaml:"api_key"`
	TimeoutSeconds int               `yaml:"timeout_seconds"`
	VectorStores   map[string]string `yaml:"vector_stores"`
}

// OwnerConfig stores local operator preferences.
type OwnerConfig struct {
	PreferredLanguage string `yaml:"preferred_language"`
	DefaultShell      string `yaml:"default_shell"`
	DefaultWorkspace  string `yaml:"default_workspace"`
}

// LLMConfig stores the chat model provider settings.
type LLMConfig struct {
	Provider string `yaml:"provider"`
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
}

// EmbeddingConfig stores embedding provider settings.
type EmbeddingConfig struct {
	Provider       string `yaml:"provider"`
	BaseURL        string `yaml:"base_url"`
	APIKey         string `yaml:"api_key"`
	Model          string `yaml:"model"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

// MemoryConfig stores markdown memory settings.
type MemoryConfig struct {
	RootDir                    string `yaml:"root_dir"`
	AutoExtract                bool   `yaml:"auto_extract"`
	AutoApplyLowRiskPreference bool   `yaml:"auto_apply_low_risk_preferences"`
	WriteAsMarkdown            bool   `yaml:"write_as_markdown"`
}

// EventsConfig stores event log locations.
type EventsConfig struct {
	JSONLRoot string `yaml:"jsonl_root"`
	AuditRoot string `yaml:"audit_root"`
}

// ShellConfig stores shell executor limits.
type ShellConfig struct {
	Enabled               bool `yaml:"enabled"`
	DefaultTimeoutSeconds int  `yaml:"default_timeout_seconds"`
	MaxOutputChars        int  `yaml:"max_output_chars"`
}

// PlannerConfig controls the hybrid natural-language planner.
type PlannerConfig struct {
	Mode                    string `yaml:"mode"`
	SemanticEnabled         bool   `yaml:"semantic_enabled"`
	SemanticShadowMode      bool   `yaml:"semantic_shadow_mode"`
	MaxRetries              int    `yaml:"max_retries"`
	RequireSchemaValidation bool   `yaml:"require_schema_validation"`
}

// PolicyProfile stores one named policy mode.
type PolicyProfile struct {
	Name                        string   `json:"name" yaml:"-"`
	Description                 string   `json:"description" yaml:"description"`
	AutoExecuteReadonly         bool     `json:"auto_execute_readonly" yaml:"auto_execute_readonly"`
	RequireApprovalFor          []string `json:"require_approval_for" yaml:"require_approval_for"`
	DenyEffects                 []string `json:"deny_effects,omitempty" yaml:"deny_effects,omitempty"`
	MinConfidenceForAutoExecute float64  `json:"min_confidence_for_auto_execute" yaml:"min_confidence_for_auto_execute"`
}

// NetworkPolicy stores outbound network safety constraints.
type NetworkPolicy struct {
	DenyPrivateIP             bool     `json:"deny_private_ip" yaml:"deny_private_ip"`
	DenyMetadataIP            bool     `json:"deny_metadata_ip" yaml:"deny_metadata_ip"`
	AllowedDomains            []string `json:"allowed_domains,omitempty" yaml:"allowed_domains,omitempty"`
	DeniedDomains             []string `json:"denied_domains,omitempty" yaml:"denied_domains,omitempty"`
	RequireApprovalForMethods []string `json:"require_approval_for_methods,omitempty" yaml:"require_approval_for_methods,omitempty"`
	MaxDownloadBytes          int64    `json:"max_download_bytes" yaml:"max_download_bytes"`
}

// PolicyConfig stores policy thresholds and sensitive paths.
type PolicyConfig struct {
	MinConfidenceForAutoExecute float64                  `yaml:"min_confidence_for_auto_execute"`
	SensitivePaths              []string                 `yaml:"sensitive_paths"`
	ActiveProfile               string                   `yaml:"active_profile"`
	Profiles                    map[string]PolicyProfile `yaml:"profiles"`
	Network                     NetworkPolicy            `yaml:"network_policy"`
}

// Default returns a production-leaning default config.
func Default() Config {
	return Config{
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 8765,
		},
		KB: KBConfig{
			Enabled:      true,
			Provider:     string(VectorBackendQdrant),
			RegistryPath: "./knowledge/registry.yaml",
		},
		Vector: VectorConfig{
			Backend:            VectorBackendMemory,
			FallbackToMemory:   true,
			EmbeddingDimension: 1536,
			Distance:           "cosine",
		},
		Qdrant: QdrantConfig{
			URL:            "http://localhost:6333",
			TimeoutSeconds: 10,
			Collections: map[string]string{
				"kb":     "kb_chunks",
				"memory": "memory_chunks",
				"code":   "code_chunks",
			},
		},
		Pinecone: PineconeConfig{
			TimeoutSeconds: 10,
			Namespaces: map[string]string{
				"kb":     "kb_chunks",
				"memory": "memory_chunks",
				"code":   "code_chunks",
			},
		},
		OpenAIKB: OpenAIKBConfig{
			BaseURL:        "https://api.openai.com/v1",
			TimeoutSeconds: 30,
			VectorStores:   map[string]string{},
		},
		Owner: OwnerConfig{
			PreferredLanguage: "zh-CN",
			DefaultShell:      "bash",
			DefaultWorkspace:  ".",
		},
		LLM: LLMConfig{
			Provider: ProviderMock,
		},
		Embeddings: EmbeddingConfig{
			Provider:       ProviderFake,
			TimeoutSeconds: 30,
		},
		Memory: MemoryConfig{
			RootDir:         "./memory",
			AutoExtract:     true,
			WriteAsMarkdown: true,
		},
		Events: EventsConfig{
			JSONLRoot: "./runs",
			AuditRoot: "./audit",
		},
		Shell: ShellConfig{
			Enabled:               true,
			DefaultTimeoutSeconds: 60,
			MaxOutputChars:        20000,
		},
		Planner: PlannerConfig{
			Mode:                    "hybrid",
			SemanticEnabled:         false,
			SemanticShadowMode:      false,
			MaxRetries:              2,
			RequireSchemaValidation: true,
		},
		Policy: PolicyConfig{
			MinConfidenceForAutoExecute: 0.85,
			SensitivePaths: []string{
				".env",
				".env.local",
				"id_rsa",
				"id_ed25519",
				"credentials",
				"cookies",
				"session",
			},
			ActiveProfile: "default",
			Profiles:      DefaultPolicyProfiles(),
			Network: NetworkPolicy{
				DenyPrivateIP:             true,
				DenyMetadataIP:            true,
				DeniedDomains:             []string{"*.internal"},
				RequireApprovalForMethods: []string{"POST", "PUT", "PATCH", "DELETE"},
				MaxDownloadBytes:          10 << 20,
			},
		},
	}
}

// DefaultPolicyProfiles returns the built-in profile set.
func DefaultPolicyProfiles() map[string]PolicyProfile {
	return map[string]PolicyProfile{
		"default": {
			Name:                        "default",
			Description:                 "Default local development policy",
			AutoExecuteReadonly:         true,
			RequireApprovalFor:          []string{"sensitive_read", "fs.write", "code.modify", "git.write", "service.restart", "process.kill", "network.post", "network.put", "network.patch", "network.delete", "unknown.effect"},
			MinConfidenceForAutoExecute: 0.75,
		},
		"strict": {
			Name:                        "strict",
			Description:                 "Strict mode, all tool calls require approval",
			AutoExecuteReadonly:         false,
			RequireApprovalFor:          []string{"*"},
			MinConfidenceForAutoExecute: 0.95,
		},
		"developer": {
			Name:                        "developer",
			Description:                 "Developer mode for local code tasks",
			AutoExecuteReadonly:         true,
			RequireApprovalFor:          []string{"sensitive_read", "fs.write", "code.modify", "git.write", "package.install", "unknown.effect"},
			MinConfidenceForAutoExecute: 0.75,
		},
		"ops": {
			Name:                        "ops",
			Description:                 "Operations mode for local and remote diagnostics",
			AutoExecuteReadonly:         true,
			RequireApprovalFor:          []string{"service.restart", "service.stop", "process.kill", "deployment.apply", "k8s.apply", "k8s.delete", "docker.restart", "fs.write", "network.write", "unknown.effect"},
			MinConfidenceForAutoExecute: 0.8,
		},
		"offline": {
			Name:                        "offline",
			Description:                 "Offline mode forbids network writes and remote calls",
			AutoExecuteReadonly:         true,
			RequireApprovalFor:          []string{"sensitive_read", "fs.write", "code.modify", "git.write", "unknown.effect"},
			DenyEffects:                 []string{"network.post", "network.put", "network.patch", "network.delete", "webhook.call", "email.send", "mcp.remote.call"},
			MinConfidenceForAutoExecute: 0.8,
		},
	}
}

// NormalizePolicy fills profile defaults and returns a self-contained copy.
func NormalizePolicy(policy PolicyConfig) PolicyConfig {
	if policy.MinConfidenceForAutoExecute <= 0 {
		policy.MinConfidenceForAutoExecute = Default().Policy.MinConfidenceForAutoExecute
	}
	if policy.ActiveProfile == "" {
		policy.ActiveProfile = "default"
	}
	if len(policy.Profiles) == 0 {
		policy.Profiles = DefaultPolicyProfiles()
	}
	for name, profile := range policy.Profiles {
		if profile.Name == "" {
			profile.Name = name
		}
		if profile.MinConfidenceForAutoExecute <= 0 {
			profile.MinConfidenceForAutoExecute = policy.MinConfidenceForAutoExecute
		}
		policy.Profiles[name] = profile
	}
	if policy.Network.MaxDownloadBytes <= 0 {
		policy.Network = Default().Policy.Network
	}
	if len(policy.Network.RequireApprovalForMethods) == 0 {
		policy.Network.RequireApprovalForMethods = []string{"POST", "PUT", "PATCH", "DELETE"}
	}
	return policy
}

// CollectionName returns the configured collection name for a logical vector scope.
func (c Config) CollectionName(scope string) string {
	switch c.vectorCollectionProvider() {
	case VectorBackendPinecone:
		if name := configuredName(c.Pinecone.Namespaces, scope); name != "" {
			return name
		}
		return defaultCollectionName(scope)
	case VectorBackendOpenAI:
		return configuredName(c.OpenAIKB.VectorStores, scope)
	default:
		if name := configuredName(c.Qdrant.Collections, scope); name != "" {
			return name
		}
		return defaultCollectionName(scope)
	}
}

func defaultCollectionName(scope string) string {
	switch scope {
	case "kb":
		return "kb_chunks"
	case "memory":
		return "memory_chunks"
	case "code":
		return "code_chunks"
	default:
		return scope + "_chunks"
	}
}

func configuredName(values map[string]string, scope string) string {
	if values == nil {
		return ""
	}
	return strings.TrimSpace(values[scope])
}

func (c Config) vectorCollectionProvider() VectorBackend {
	backend := VectorBackend(strings.ToLower(strings.TrimSpace(string(c.Vector.Backend))))
	if backend != "" && backend != VectorBackendMemory {
		return backend
	}
	provider := VectorBackend(strings.ToLower(strings.TrimSpace(c.KB.Provider)))
	if provider != "" {
		return provider
	}
	return VectorBackendQdrant
}
