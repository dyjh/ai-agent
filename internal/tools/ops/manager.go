package ops

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/ids"
	"local-agent/internal/security"
	toolscore "local-agent/internal/tools"
)

// Manager owns operations host profiles and runbook routing helpers.
type Manager struct {
	mu         sync.RWMutex
	hosts      map[string]HostProfile
	runbookDir string
	router     toolscore.Router
	now        func() time.Time
}

// NewManager creates an in-memory operations manager with a default local host.
func NewManager(runbookDir string) *Manager {
	m := &Manager{
		hosts:      map[string]HostProfile{},
		runbookDir: runbookDir,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
	now := m.now()
	m.hosts[HostTypeLocal] = HostProfile{
		HostID:           HostTypeLocal,
		Name:             "Localhost",
		Type:             HostTypeLocal,
		DefaultShell:     "bash",
		WorkingDirectory: ".",
		PolicyProfile:    "local-read-mostly",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	return m
}

// SetRouter lets runbook executors route each step through the normal tool chain.
func (m *Manager) SetRouter(router toolscore.Router) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.router = router
}

// ListHosts returns redacted host profiles sorted by ID.
func (m *Manager) ListHosts() []HostProfile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]HostProfile, 0, len(m.hosts))
	for _, item := range m.hosts {
		items = append(items, redactHost(item))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].HostID < items[j].HostID
	})
	return items
}

// CreateHost validates and stores a host profile.
func (m *Manager) CreateHost(input HostProfileInput) (HostProfile, error) {
	if err := validateHostInput(input, false); err != nil {
		return HostProfile{}, err
	}
	now := m.now()
	item := HostProfile{
		HostID:           ids.New("host"),
		Name:             strings.TrimSpace(input.Name),
		Type:             normalizeHostType(input.Type),
		DefaultShell:     strings.TrimSpace(input.DefaultShell),
		WorkingDirectory: strings.TrimSpace(input.WorkingDirectory),
		PolicyProfile:    strings.TrimSpace(input.PolicyProfile),
		Metadata:         cloneStringMap(input.Metadata),
		SSH:              cloneSSHConfig(input.SSH),
		K8s:              cloneK8sConfig(input.K8s),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if item.DefaultShell == "" && item.Type == HostTypeLocal {
		item.DefaultShell = "bash"
	}
	if item.WorkingDirectory == "" {
		item.WorkingDirectory = "."
	}
	if item.PolicyProfile == "" {
		item.PolicyProfile = item.Type + "-default"
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.hosts[item.HostID] = item
	return redactHost(item), nil
}

// GetHost returns one redacted host profile.
func (m *Manager) GetHost(id string) (HostProfile, error) {
	item, err := m.getHostRaw(id)
	if err != nil {
		return HostProfile{}, err
	}
	return redactHost(item), nil
}

// UpdateHost replaces mutable host fields.
func (m *Manager) UpdateHost(id string, input HostProfileInput) (HostProfile, error) {
	if err := validateHostInput(input, true); err != nil {
		return HostProfile{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.hosts[id]
	if !ok {
		return HostProfile{}, fmt.Errorf("host profile not found: %s", id)
	}
	if input.Name != "" {
		existing.Name = strings.TrimSpace(input.Name)
	}
	if input.Type != "" {
		existing.Type = normalizeHostType(input.Type)
	}
	if input.DefaultShell != "" {
		existing.DefaultShell = strings.TrimSpace(input.DefaultShell)
	}
	if input.WorkingDirectory != "" {
		existing.WorkingDirectory = strings.TrimSpace(input.WorkingDirectory)
	}
	if input.PolicyProfile != "" {
		existing.PolicyProfile = strings.TrimSpace(input.PolicyProfile)
	}
	if input.Metadata != nil {
		existing.Metadata = cloneStringMap(input.Metadata)
	}
	if input.SSH != nil {
		existing.SSH = cloneSSHConfig(input.SSH)
	}
	if input.K8s != nil {
		existing.K8s = cloneK8sConfig(input.K8s)
	}
	existing.UpdatedAt = m.now()
	if err := validateStoredHost(existing); err != nil {
		return HostProfile{}, err
	}
	m.hosts[id] = existing
	return redactHost(existing), nil
}

// DeleteHost removes a profile.
func (m *Manager) DeleteHost(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.hosts[id]; !ok {
		return fmt.Errorf("host profile not found: %s", id)
	}
	delete(m.hosts, id)
	return nil
}

// TestHost performs a safe connectivity/configuration check.
func (m *Manager) TestHost(ctx context.Context, id string) (HostTestResult, error) {
	host, err := m.getHostRaw(id)
	if err != nil {
		return HostTestResult{}, err
	}
	result := HostTestResult{
		HostID: host.HostID,
		Type:   host.Type,
		Status: "ok",
	}
	switch host.Type {
	case HostTypeLocal:
		return result, nil
	case HostTypeSSH:
		if host.SSH == nil {
			return HostTestResult{}, errors.New("ssh host config is required")
		}
		if host.SSH.Host == "" || host.SSH.User == "" {
			return HostTestResult{}, errors.New("ssh host and user are required")
		}
		_ = ctx
		return result, nil
	case HostTypeDocker, HostTypeK8s:
		return result, nil
	default:
		return HostTestResult{}, fmt.Errorf("unsupported host type: %s", host.Type)
	}
}

func (m *Manager) getHostRaw(id string) (HostProfile, error) {
	if strings.TrimSpace(id) == "" {
		id = HostTypeLocal
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.hosts[id]
	if !ok {
		return HostProfile{}, fmt.Errorf("host profile not found: %s", id)
	}
	return cloneHost(item), nil
}

func validateHostInput(input HostProfileInput, partial bool) error {
	hostType := normalizeHostType(input.Type)
	if !partial || hostType != "" {
		if !isAllowedHostType(hostType) {
			return fmt.Errorf("unsupported host type: %s", input.Type)
		}
	}
	if !partial && strings.TrimSpace(input.Name) == "" {
		return errors.New("name is required")
	}
	if hostType == HostTypeSSH && input.SSH == nil {
		return errors.New("ssh config is required for ssh host")
	}
	if input.SSH != nil {
		if strings.TrimSpace(input.SSH.Host) == "" || strings.TrimSpace(input.SSH.User) == "" {
			return errors.New("ssh host and user are required")
		}
		if input.SSH.Port < 0 || input.SSH.Port > 65535 {
			return errors.New("ssh port is invalid")
		}
		switch strings.TrimSpace(input.SSH.AuthType) {
		case "", "key", "agent", "password-ref":
		default:
			return fmt.Errorf("unsupported ssh auth_type: %s", input.SSH.AuthType)
		}
	}
	return nil
}

func validateStoredHost(host HostProfile) error {
	return validateHostInput(HostProfileInput{
		Name:             host.Name,
		Type:             host.Type,
		DefaultShell:     host.DefaultShell,
		WorkingDirectory: host.WorkingDirectory,
		PolicyProfile:    host.PolicyProfile,
		Metadata:         host.Metadata,
		SSH:              host.SSH,
		K8s:              host.K8s,
	}, false)
}

func normalizeHostType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isAllowedHostType(value string) bool {
	switch value {
	case HostTypeLocal, HostTypeSSH, HostTypeDocker, HostTypeK8s:
		return true
	default:
		return false
	}
}

func redactHost(host HostProfile) HostProfile {
	cp := cloneHost(host)
	if cp.Metadata != nil {
		raw := make(map[string]any, len(cp.Metadata))
		for key, value := range cp.Metadata {
			raw[key] = value
		}
		redacted := security.RedactMap(raw)
		cp.Metadata = map[string]string{}
		for key, value := range redacted {
			cp.Metadata[key] = fmt.Sprint(value)
		}
	}
	if cp.SSH != nil {
		if cp.SSH.KeyPath != "" {
			cp.SSH.KeyPath = "[REDACTED_PATH]"
		}
		if cp.SSH.PasswordRef != "" {
			cp.SSH.PasswordRef = "[REDACTED]"
		}
	}
	if cp.K8s != nil && cp.K8s.KubeconfigPath != "" {
		cp.K8s.KubeconfigPath = "[REDACTED_PATH]"
	}
	return cp
}

func cloneHost(host HostProfile) HostProfile {
	cp := host
	cp.Metadata = cloneStringMap(host.Metadata)
	cp.SSH = cloneSSHConfig(host.SSH)
	cp.K8s = cloneK8sConfig(host.K8s)
	return cp
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneSSHConfig(input *SSHHostConfig) *SSHHostConfig {
	if input == nil {
		return nil
	}
	cp := *input
	if cp.Port == 0 {
		cp.Port = 22
	}
	cp.Host = strings.TrimSpace(cp.Host)
	cp.User = strings.TrimSpace(cp.User)
	cp.AuthType = strings.TrimSpace(cp.AuthType)
	if cp.AuthType == "" {
		cp.AuthType = "agent"
	}
	return &cp
}

func cloneK8sConfig(input *K8sHostConfig) *K8sHostConfig {
	if input == nil {
		return nil
	}
	cp := *input
	cp.KubeconfigPath = strings.TrimSpace(cp.KubeconfigPath)
	cp.Context = strings.TrimSpace(cp.Context)
	cp.Namespace = strings.TrimSpace(cp.Namespace)
	return &cp
}

func newProposal(tool string, input map[string]any, purpose string, effects []string) core.ToolProposal {
	return core.ToolProposal{
		ID:              ids.New("tool"),
		Tool:            tool,
		Input:           core.CloneMap(input),
		Purpose:         purpose,
		ExpectedEffects: append([]string(nil), effects...),
		CreatedAt:       time.Now().UTC(),
	}
}
