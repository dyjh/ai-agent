package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/ids"
)

// Manager tracks locally registered skills.
type Manager struct {
	root   string
	mu     sync.RWMutex
	skills map[string]core.SkillRegistration
}

// NewManager creates a skill manager.
func NewManager(root string) *Manager {
	return &Manager{
		root:   root,
		skills: map[string]core.SkillRegistration{},
	}
}

// Upload registers a local skill archive or directory.
func (m *Manager) Upload(path, name, description string) (core.SkillRegistration, error) {
	if path == "" {
		return core.SkillRegistration{}, fmt.Errorf("skill path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return core.SkillRegistration{}, err
	}
	if name == "" {
		name = filepath.Base(path)
	}
	item := core.SkillRegistration{
		ID:          ids.New("skill"),
		Name:        name,
		Description: description,
		ArchivePath: path,
		Enabled:     true,
		CreatedAt:   time.Now().UTC(),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skills[item.ID] = item
	return item, nil
}

// List returns all registered skills.
func (m *Manager) List() []core.SkillRegistration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]core.SkillRegistration, 0, len(m.skills))
	for _, skill := range m.skills {
		items = append(items, skill)
	}
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
	item.Enabled = enabled
	m.skills[id] = item
	return item, nil
}
