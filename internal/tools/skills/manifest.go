package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	RuntimeTypeExecutable = "executable"
	RuntimeTypeScript     = "script"
	RuntimeTypeGo         = "go"

	InputModeJSONStdin = "json_stdin"
	InputModeArgs      = "args"
	InputModeEnv       = "env"

	OutputModeJSONStdout = "json_stdout"
	OutputModeText       = "text"

	ApprovalDefaultAuto    = "auto"
	ApprovalDefaultRequire = "require"
)

// Manifest is the local skill descriptor used for validation and execution.
type Manifest struct {
	ID           string            `json:"id" yaml:"id"`
	Name         string            `json:"name" yaml:"name"`
	Version      string            `json:"version" yaml:"version"`
	Description  string            `json:"description,omitempty" yaml:"description,omitempty"`
	Runtime      Runtime           `json:"runtime" yaml:"runtime"`
	InputSchema  map[string]any    `json:"input_schema,omitempty" yaml:"input_schema,omitempty"`
	OutputSchema map[string]any    `json:"output_schema,omitempty" yaml:"output_schema,omitempty"`
	Effects      []string          `json:"effects,omitempty" yaml:"effects,omitempty"`
	Approval     ApprovalConfig    `json:"approval,omitempty" yaml:"approval,omitempty"`
	Permissions  PermissionsConfig `json:"permissions,omitempty" yaml:"permissions,omitempty"`
}

// Runtime defines how the local skill executable should be invoked.
type Runtime struct {
	Type           string   `json:"type" yaml:"type"`
	Command        string   `json:"command,omitempty" yaml:"command,omitempty"`
	Entrypoint     string   `json:"entrypoint,omitempty" yaml:"entrypoint,omitempty"`
	Args           []string `json:"args,omitempty" yaml:"args,omitempty"`
	Cwd            string   `json:"cwd,omitempty" yaml:"cwd,omitempty"`
	InputMode      string   `json:"input_mode,omitempty" yaml:"input_mode,omitempty"`
	OutputMode     string   `json:"output_mode,omitempty" yaml:"output_mode,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
}

// ApprovalConfig controls whether a skill should auto-run or request approval.
type ApprovalConfig struct {
	Default string `json:"default,omitempty" yaml:"default,omitempty"`
}

// PermissionsConfig records the local capability intent declared by the skill author.
type PermissionsConfig struct {
	Filesystem FilesystemPermissions `json:"filesystem,omitempty" yaml:"filesystem,omitempty"`
	Shell      FeaturePermission     `json:"shell,omitempty" yaml:"shell,omitempty"`
	Network    FeaturePermission     `json:"network,omitempty" yaml:"network,omitempty"`
}

// FilesystemPermissions records declared read/write paths.
type FilesystemPermissions struct {
	Read  []string `json:"read,omitempty" yaml:"read,omitempty"`
	Write []string `json:"write,omitempty" yaml:"write,omitempty"`
}

// FeaturePermission toggles a broad capability family.
type FeaturePermission struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// LoadManifest loads and validates a skill manifest from the given skill root.
func LoadManifest(root string) (Manifest, error) {
	path := ManifestPath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}

	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}

	if err := manifest.Normalize(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// ManifestPath returns the expected manifest path inside a skill directory.
func ManifestPath(root string) string {
	return filepath.Join(root, "skill.yaml")
}

// Normalize fills defaults and validates a manifest.
func (m *Manifest) Normalize() error {
	m.ID = strings.TrimSpace(m.ID)
	m.Name = strings.TrimSpace(m.Name)
	m.Version = strings.TrimSpace(m.Version)
	m.Description = strings.TrimSpace(m.Description)

	m.Runtime.Type = strings.ToLower(strings.TrimSpace(m.Runtime.Type))
	m.Runtime.Command = strings.TrimSpace(m.Runtime.Command)
	m.Runtime.Entrypoint = strings.TrimSpace(m.Runtime.Entrypoint)
	if m.Runtime.Command == "" {
		m.Runtime.Command = m.Runtime.Entrypoint
	}
	m.Runtime.Cwd = strings.TrimSpace(m.Runtime.Cwd)
	if m.Runtime.Cwd == "" {
		m.Runtime.Cwd = "."
	}
	m.Runtime.InputMode = strings.ToLower(strings.TrimSpace(m.Runtime.InputMode))
	if m.Runtime.InputMode == "" {
		m.Runtime.InputMode = InputModeJSONStdin
	}
	m.Runtime.OutputMode = strings.ToLower(strings.TrimSpace(m.Runtime.OutputMode))
	if m.Runtime.OutputMode == "" {
		m.Runtime.OutputMode = OutputModeJSONStdout
	}
	if m.Runtime.TimeoutSeconds <= 0 {
		m.Runtime.TimeoutSeconds = 30
	}

	effects := make([]string, 0, len(m.Effects))
	seen := map[string]struct{}{}
	for _, effect := range m.Effects {
		effect = strings.TrimSpace(effect)
		if effect == "" {
			continue
		}
		if _, ok := seen[effect]; ok {
			continue
		}
		seen[effect] = struct{}{}
		effects = append(effects, effect)
	}
	if len(effects) == 0 {
		effects = []string{"unknown.effect"}
	}
	m.Effects = effects

	m.Approval.Default = strings.ToLower(strings.TrimSpace(m.Approval.Default))
	if m.Approval.Default == "" {
		m.Approval.Default = ApprovalDefaultAuto
	}

	return ValidateManifest(*m)
}

// EffectiveEffects returns the normalized effect list for policy inference.
func (m Manifest) EffectiveEffects() []string {
	if len(m.Effects) == 0 {
		return []string{"unknown.effect"}
	}
	return append([]string(nil), m.Effects...)
}

// ValidateManifest validates the executable contract of a manifest.
func ValidateManifest(m Manifest) error {
	if m.ID == "" {
		return fmt.Errorf("skill id is required")
	}
	if m.Version == "" {
		return fmt.Errorf("skill version is required")
	}
	if m.Runtime.Type == "" {
		return fmt.Errorf("skill runtime.type is required")
	}
	if m.Runtime.Command == "" {
		return fmt.Errorf("skill runtime.command is required")
	}
	switch m.Runtime.Type {
	case RuntimeTypeExecutable, RuntimeTypeScript, RuntimeTypeGo:
	default:
		return fmt.Errorf("unsupported skill runtime.type: %s", m.Runtime.Type)
	}
	switch m.Runtime.InputMode {
	case InputModeJSONStdin, InputModeArgs, InputModeEnv:
	default:
		return fmt.Errorf("unsupported skill runtime.input_mode: %s", m.Runtime.InputMode)
	}
	switch m.Runtime.OutputMode {
	case OutputModeJSONStdout, OutputModeText:
	default:
		return fmt.Errorf("unsupported skill runtime.output_mode: %s", m.Runtime.OutputMode)
	}
	switch m.Approval.Default {
	case ApprovalDefaultAuto, ApprovalDefaultRequire:
	default:
		return fmt.Errorf("unsupported skill approval.default: %s", m.Approval.Default)
	}
	if m.InputSchema != nil {
		if err := ValidateSchemaDocument(m.InputSchema); err != nil {
			return fmt.Errorf("invalid input_schema: %w", err)
		}
	}
	if m.OutputSchema != nil {
		if err := ValidateSchemaDocument(m.OutputSchema); err != nil {
			return fmt.Errorf("invalid output_schema: %w", err)
		}
	}
	return nil
}
