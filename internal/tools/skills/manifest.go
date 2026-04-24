package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

	SandboxProfileRestricted     = "restricted"
	SandboxProfileTrustedLocal   = "trusted_local"
	SandboxProfileBestEffort     = "best_effort_local"
	defaultProcessMaxOutputBytes = int64(1 << 20)
)

var hostPattern = regexp.MustCompile(`^(\*\.)?([a-zA-Z0-9-]+\.)*[a-zA-Z0-9-]+(:[0-9]{1,5})?$`)

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
	Sandbox      SandboxConfig     `json:"sandbox,omitempty" yaml:"sandbox,omitempty"`
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
	Shell      FeaturePermission     `json:"shell,omitempty" yaml:"shell,omitempty"` // legacy compatibility
	Network    NetworkPermissions    `json:"network,omitempty" yaml:"network,omitempty"`
	Env        EnvPermissions        `json:"env,omitempty" yaml:"env,omitempty"`
	Tools      ToolPermissions       `json:"tools,omitempty" yaml:"tools,omitempty"`
	Process    ProcessPermissions    `json:"process,omitempty" yaml:"process,omitempty"`
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

// NetworkPermissions records outbound network access preferences.
type NetworkPermissions struct {
	Enabled    bool     `json:"enabled" yaml:"enabled"`
	AllowHosts []string `json:"allow_hosts,omitempty" yaml:"allow_hosts,omitempty"`
}

// EnvPermissions records the environment variables that may be inherited.
type EnvPermissions struct {
	Allow []string `json:"allow,omitempty" yaml:"allow,omitempty"`
}

// ToolPermissions records local bridge permissions.
type ToolPermissions struct {
	AllowShell bool `json:"allow_shell" yaml:"allow_shell"`
	AllowMCP   bool `json:"allow_mcp" yaml:"allow_mcp"`
}

// ProcessPermissions records runtime process limits.
type ProcessPermissions struct {
	MaxRuntimeSeconds int   `json:"max_runtime_seconds,omitempty" yaml:"max_runtime_seconds,omitempty"`
	MaxOutputBytes    int64 `json:"max_output_bytes,omitempty" yaml:"max_output_bytes,omitempty"`
}

// SandboxConfig records the requested execution isolation profile.
type SandboxConfig struct {
	Profile string `json:"profile,omitempty" yaml:"profile,omitempty"`
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
	for idx := range m.Runtime.Args {
		m.Runtime.Args[idx] = strings.TrimSpace(m.Runtime.Args[idx])
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
	seenEffects := map[string]struct{}{}
	for _, effect := range m.Effects {
		effect = strings.TrimSpace(effect)
		if effect == "" {
			continue
		}
		if _, ok := seenEffects[effect]; ok {
			continue
		}
		seenEffects[effect] = struct{}{}
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

	if m.Permissions.Shell.Enabled {
		m.Permissions.Tools.AllowShell = true
	}
	m.Permissions.Filesystem.Read = normalizePathList(m.Permissions.Filesystem.Read)
	if len(m.Permissions.Filesystem.Read) == 0 {
		m.Permissions.Filesystem.Read = []string{"."}
	}
	m.Permissions.Filesystem.Write = normalizePathList(m.Permissions.Filesystem.Write)
	m.Permissions.Network.AllowHosts = normalizeStringList(m.Permissions.Network.AllowHosts, false)
	m.Permissions.Env.Allow = normalizeStringList(m.Permissions.Env.Allow, true)
	if m.Permissions.Process.MaxRuntimeSeconds <= 0 {
		m.Permissions.Process.MaxRuntimeSeconds = m.Runtime.TimeoutSeconds
	}
	if m.Permissions.Process.MaxOutputBytes <= 0 {
		m.Permissions.Process.MaxOutputBytes = defaultProcessMaxOutputBytes
	}

	m.Sandbox.Profile = strings.ToLower(strings.TrimSpace(m.Sandbox.Profile))
	if m.Sandbox.Profile == "" {
		m.Sandbox.Profile = SandboxProfileRestricted
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
	switch m.Sandbox.Profile {
	case SandboxProfileRestricted, SandboxProfileTrustedLocal, SandboxProfileBestEffort:
	default:
		return fmt.Errorf("unsupported skill sandbox.profile: %s", m.Sandbox.Profile)
	}
	if m.Runtime.TimeoutSeconds <= 0 {
		return fmt.Errorf("skill runtime.timeout_seconds must be > 0")
	}
	if m.Permissions.Process.MaxRuntimeSeconds <= 0 {
		return fmt.Errorf("skill permissions.process.max_runtime_seconds must be > 0")
	}
	if m.Permissions.Process.MaxOutputBytes <= 0 {
		return fmt.Errorf("skill permissions.process.max_output_bytes must be > 0")
	}
	if err := validatePathList("permissions.filesystem.read", m.Permissions.Filesystem.Read); err != nil {
		return err
	}
	if err := validatePathList("permissions.filesystem.write", m.Permissions.Filesystem.Write); err != nil {
		return err
	}
	if !m.Permissions.Network.Enabled && len(m.Permissions.Network.AllowHosts) > 0 {
		return fmt.Errorf("permissions.network.allow_hosts requires permissions.network.enabled=true")
	}
	for _, host := range m.Permissions.Network.AllowHosts {
		if !isValidHostPattern(host) {
			return fmt.Errorf("permissions.network.allow_hosts contains invalid host %q", host)
		}
	}
	for _, envName := range m.Permissions.Env.Allow {
		if !isValidEnvName(envName) {
			return fmt.Errorf("permissions.env.allow contains invalid env name %q", envName)
		}
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

	if m.Permissions.Network.Enabled && !hasEffectPrefix(m.Effects, "network.") {
		return fmt.Errorf("permissions.network.enabled requires a network.* effect declaration")
	}
	if len(m.Permissions.Filesystem.Write) > 0 && !hasAnyEffect(m.Effects, "fs.write", "code.modify", "config.modify", "memory.modify") {
		return fmt.Errorf("permissions.filesystem.write requires a write-related effect declaration")
	}
	if m.Permissions.Tools.AllowShell && !hasEffectPrefix(m.Effects, "shell.") && !hasEffectPrefix(m.Effects, "process.") {
		return fmt.Errorf("permissions.tools.allow_shell requires a shell/process effect declaration")
	}
	if m.Permissions.Tools.AllowMCP && !hasEffectPrefix(m.Effects, "mcp.") {
		return fmt.Errorf("permissions.tools.allow_mcp requires an mcp.* effect declaration")
	}

	return nil
}

// DefaultEnvAllowList returns the minimum environment inherited by local skill execution.
func DefaultEnvAllowList() []string {
	return []string{"PATH", "LANG", "LC_ALL", "HOME", "TMPDIR", "TEMP", "TMP"}
}

func normalizePathList(items []string) []string {
	return normalizeStringList(items, false)
}

func normalizeStringList(items []string, upper bool) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if upper {
			item = strings.ToUpper(item)
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func validatePathList(field string, items []string) error {
	for _, item := range items {
		if item == "" {
			return fmt.Errorf("%s contains an empty path", field)
		}
		if strings.ContainsRune(item, 0) {
			return fmt.Errorf("%s contains an invalid path", field)
		}
		cleaned := filepath.Clean(item)
		if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s path %q escapes the skill root", field, item)
		}
	}
	return nil
}

func isValidHostPattern(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || strings.Contains(host, "://") || strings.Contains(host, "/") || strings.Contains(host, " ") {
		return false
	}
	return host == "localhost" || hostPattern.MatchString(host)
}

func isValidEnvName(name string) bool {
	if name == "" {
		return false
	}
	for idx, r := range name {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9' && idx > 0) {
			continue
		}
		return false
	}
	return true
}

func hasEffectPrefix(effects []string, prefix string) bool {
	for _, effect := range effects {
		if strings.HasPrefix(effect, prefix) {
			return true
		}
	}
	return false
}

func hasAnyEffect(effects []string, allowed ...string) bool {
	set := map[string]struct{}{}
	for _, item := range allowed {
		set[item] = struct{}{}
	}
	for _, effect := range effects {
		if _, ok := set[effect]; ok {
			return true
		}
	}
	return false
}
