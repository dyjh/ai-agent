package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"local-agent/internal/core"
)

// RegisteredSkill is the normalized registry record stored by the manager.
type RegisteredSkill struct {
	Registration core.SkillRegistration `json:"registration"`
	Manifest     Manifest               `json:"manifest"`
	Root         string                 `json:"root"`
}

// Manager tracks locally registered skills.
type Manager struct {
	root   string
	mu     sync.RWMutex
	skills map[string]RegisteredSkill
}

// NewManager creates a skill manager.
func NewManager(root string) *Manager {
	return &Manager{
		root:   root,
		skills: map[string]RegisteredSkill{},
	}
}

// Upload registers a local skill directory or manifest path.
func (m *Manager) Upload(path, name, description string) (core.SkillRegistration, error) {
	if path == "" {
		return core.SkillRegistration{}, fmt.Errorf("skill path is required")
	}
	root, err := m.resolveRoot(path)
	if err != nil {
		return core.SkillRegistration{}, err
	}
	if _, err := os.Stat(root); err != nil {
		return core.SkillRegistration{}, err
	}
	manifest, err := LoadManifest(root)
	if err != nil {
		return core.SkillRegistration{}, err
	}
	if name == "" {
		name = manifest.Name
	}
	if name == "" {
		name = manifest.ID
	}
	if description == "" {
		description = manifest.Description
	}

	item := core.SkillRegistration{
		ID:              manifest.ID,
		Name:            name,
		Version:         manifest.Version,
		Description:     description,
		ArchivePath:     root,
		RuntimeType:     manifest.Runtime.Type,
		Effects:         manifest.EffectiveEffects(),
		ApprovalDefault: manifest.Approval.Default,
		Enabled:         true,
		CreatedAt:       time.Now().UTC(),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.skills[item.ID]; ok {
		item.Enabled = existing.Registration.Enabled
		item.CreatedAt = existing.Registration.CreatedAt
	}
	m.skills[item.ID] = RegisteredSkill{
		Registration: item,
		Manifest:     manifest,
		Root:         root,
	}
	return item, nil
}

// List returns all registered skills.
func (m *Manager) List() []core.SkillRegistration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]core.SkillRegistration, 0, len(m.skills))
	for _, skill := range m.skills {
		items = append(items, skill.Registration)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

// SetEnabled updates the enabled state.
func (m *Manager) SetEnabled(id string, enabled bool) (core.SkillRegistration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.skills[id]
	if !ok {
		return core.SkillRegistration{}, fmt.Errorf("skill not found: %s", id)
	}
	item.Registration.Enabled = enabled
	m.skills[id] = item
	return item.Registration, nil
}

// Get returns a registered skill by ID.
func (m *Manager) Get(id string) (core.SkillRegistration, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.skills[id]
	if !ok {
		return core.SkillRegistration{}, fmt.Errorf("skill not found: %s", id)
	}
	return item.Registration, nil
}

// Resolve returns the full skill entry used by the runner.
func (m *Manager) Resolve(id string) (RegisteredSkill, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.skills[id]
	if !ok {
		return RegisteredSkill{}, fmt.Errorf("skill not found: %s", id)
	}
	item.Registration.Effects = append([]string(nil), item.Registration.Effects...)
	item.Manifest.Effects = append([]string(nil), item.Manifest.Effects...)
	return item, nil
}

// PolicyProfile returns the policy metadata used by effect inference.
func (m *Manager) PolicyProfile(id string) (core.SkillPolicyProfile, error) {
	item, err := m.Resolve(id)
	if err != nil {
		return core.SkillPolicyProfile{}, err
	}
	return core.SkillPolicyProfile{
		ID:              item.Registration.ID,
		Effects:         append([]string(nil), item.Manifest.EffectiveEffects()...),
		ApprovalDefault: item.Manifest.Approval.Default,
		Enabled:         item.Registration.Enabled,
	}, nil
}

func (m *Manager) resolveRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return abs, nil
	}
	if filepath.Base(abs) == "skill.yaml" {
		return filepath.Dir(abs), nil
	}
	return "", fmt.Errorf("skill path must be a directory or skill.yaml")
}
