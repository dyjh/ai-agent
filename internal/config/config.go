package config

// Config is the root runtime configuration.
type Config struct {
	Server     ServerConfig    `yaml:"server"`
	Database   DatabaseConfig  `yaml:"database"`
	KB         KBConfig        `yaml:"kb"`
	Vector     VectorConfig    `yaml:"vector"`
	Qdrant     QdrantConfig    `yaml:"qdrant"`
	Owner      OwnerConfig     `yaml:"owner"`
	LLM        LLMConfig       `yaml:"llm"`
	Embeddings EmbeddingConfig `yaml:"embeddings"`
	Memory     MemoryConfig    `yaml:"memory"`
	Events     EventsConfig    `yaml:"events"`
	Shell      ShellConfig     `yaml:"shell"`
	Policy     PolicyConfig    `yaml:"policy"`
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
	VectorBackendMemory VectorBackend = "memory"
	VectorBackendQdrant VectorBackend = "qdrant"
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
	URL            string            `yaml:"url"`
	APIKey         string            `yaml:"api_key"`
	TimeoutSeconds int               `yaml:"timeout_seconds"`
	Collections    map[string]string `yaml:"collections"`
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
	Provider string `yaml:"provider"`
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
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

// PolicyConfig stores policy thresholds and sensitive paths.
type PolicyConfig struct {
	MinConfidenceForAutoExecute float64  `yaml:"min_confidence_for_auto_execute"`
	SensitivePaths              []string `yaml:"sensitive_paths"`
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
		Owner: OwnerConfig{
			PreferredLanguage: "zh-CN",
			DefaultShell:      "bash",
			DefaultWorkspace:  ".",
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
		},
	}
}

// CollectionName returns the configured collection name for a logical vector scope.
func (c Config) CollectionName(scope string) string {
	if c.Qdrant.Collections == nil {
		return defaultCollectionName(scope)
	}
	if name := c.Qdrant.Collections[scope]; name != "" {
		return name
	}
	return defaultCollectionName(scope)
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
