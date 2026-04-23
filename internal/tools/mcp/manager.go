package mcp

import (
	"fmt"
	"sync"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/ids"
)

// Manager stores MCP server configs and local policy overrides.
type Manager struct {
	mu       sync.RWMutex
	servers  map[string]core.MCPServer
	policies map[string]core.MCPToolPolicy
}

// NewManager creates an MCP manager.
func NewManager() *Manager {
	return &Manager{
		servers:  map[string]core.MCPServer{},
		policies: map[string]core.MCPToolPolicy{},
	}
}

// ListServers returns all configured MCP servers.
func (m *Manager) ListServers() []core.MCPServer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]core.MCPServer, 0, len(m.servers))
	for _, server := range m.servers {
		items = append(items, server)
	}
	return items
}

// CreateServer registers a new MCP server.
func (m *Manager) CreateServer(input ServerInput) (core.MCPServer, error) {
	item := core.MCPServer{
		ID:          ids.New("mcp"),
		Name:        input.Name,
		Transport:   input.Transport,
		Command:     input.Command,
		URL:         input.URL,
		Enabled:     input.Enabled == nil || *input.Enabled,
		Environment: input.Env,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers[item.ID] = item
	return item, nil
}

// UpdateServer mutates a server config.
func (m *Manager) UpdateServer(id string, input ServerInput) (core.MCPServer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.servers[id]
	if !ok {
		return core.MCPServer{}, fmt.Errorf("mcp server not found: %s", id)
	}
	if input.Name != "" {
		item.Name = input.Name
	}
	if input.Transport != "" {
		item.Transport = input.Transport
	}
	if input.Command != "" {
		item.Command = input.Command
	}
	if input.URL != "" {
		item.URL = input.URL
	}
	if input.Enabled != nil {
		item.Enabled = *input.Enabled
	}
	if input.Env != nil {
		item.Environment = input.Env
	}
	item.UpdatedAt = time.Now().UTC()
	m.servers[id] = item
	return item, nil
}

// ListToolPolicies returns local policy overrides.
func (m *Manager) ListToolPolicies() []core.MCPToolPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]core.MCPToolPolicy, 0, len(m.policies))
	for _, policy := range m.policies {
		items = append(items, policy)
	}
	return items
}

// UpdateToolPolicy upserts a tool policy override.
func (m *Manager) UpdateToolPolicy(id string, requiresApproval bool, riskLevel, reason string) core.MCPToolPolicy {
	m.mu.Lock()
	defer m.mu.Unlock()
	policy := core.MCPToolPolicy{
		ID:               id,
		ToolName:         id,
		RequiresApproval: requiresApproval,
		RiskLevel:        riskLevel,
		Reason:           reason,
		UpdatedAt:        time.Now().UTC(),
	}
	m.policies[id] = policy
	return policy
}
