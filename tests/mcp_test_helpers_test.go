package tests

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
	toolscore "local-agent/internal/tools"
	"local-agent/internal/tools/mcp"
)

type mockMCPFactory struct {
	mu         sync.Mutex
	transports map[string]*mockMCPTransport
}

func newMockMCPFactory() *mockMCPFactory {
	return &mockMCPFactory{transports: map[string]*mockMCPTransport{}}
}

func (f *mockMCPFactory) NewTransport(cfg mcp.TransportConfig) (mcp.MCPTransport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	transport := f.transports[cfg.ServerID]
	if transport == nil {
		transport = &mockMCPTransport{}
		f.transports[cfg.ServerID] = transport
	}
	return transport, nil
}

func (f *mockMCPFactory) transport(serverID string) *mockMCPTransport {
	f.mu.Lock()
	defer f.mu.Unlock()
	transport := f.transports[serverID]
	if transport == nil {
		transport = &mockMCPTransport{}
		f.transports[serverID] = transport
	}
	return transport
}

type mockMCPTransport struct {
	mu       sync.Mutex
	started  bool
	closed   bool
	tools    []mcp.MCPToolSchema
	calls    []mockMCPCall
	results  map[string]*mcp.MCPToolResult
	callErrs map[string]error
}

type mockMCPCall struct {
	Name  string
	Input map[string]any
}

func (m *mockMCPTransport) Start(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	return nil
}

func (m *mockMCPTransport) ListTools(_ context.Context) ([]mcp.MCPToolSchema, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mcp.MCPToolSchema(nil), m.tools...), nil
}

func (m *mockMCPTransport) CallTool(_ context.Context, name string, input map[string]any) (*mcp.MCPToolResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockMCPCall{Name: name, Input: input})
	if err := m.callErrs[name]; err != nil {
		return nil, err
	}
	if result := m.results[name]; result != nil {
		return result, nil
	}
	return &mcp.MCPToolResult{
		Structured: map[string]any{"tool": name, "ok": true},
	}, nil
}

func (m *mockMCPTransport) Close(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockMCPTransport) setTools(tools []mcp.MCPToolSchema) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tools = append([]mcp.MCPToolSchema(nil), tools...)
}

func (m *mockMCPTransport) setResult(tool string, result *mcp.MCPToolResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.results == nil {
		m.results = map[string]*mcp.MCPToolResult{}
	}
	m.results[tool] = result
}

func (m *mockMCPTransport) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockMCPTransport) lastCall() (mockMCPCall, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return mockMCPCall{}, fmt.Errorf("no calls")
	}
	return m.calls[len(m.calls)-1], nil
}

func newMCPRouterFixture(t *testing.T, manager *mcp.Manager) (*toolscore.LocalRouter, *agent.ApprovalCenter) {
	t.Helper()
	registry := toolscore.NewRegistry()
	registry.Register(core.ToolSpec{
		ID:             "mcp.call_tool",
		Provider:       "mcp",
		Name:           "mcp.call_tool",
		Description:    "Call a configured MCP tool",
		DefaultEffects: []string{"unknown.effect"},
	}, &mcp.CallToolExecutor{Client: manager})
	approvals := agent.NewApprovalCenter()
	router := toolscore.NewRouter(
		registry,
		agent.NewEffectInferrer(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}, manager),
		agent.NewPolicyEngine(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}),
		approvals,
		nil,
	)
	return router, approvals
}

func createMCPServer(t *testing.T, manager *mcp.Manager, id string, enabled bool) {
	t.Helper()
	if _, err := manager.CreateServer(mcp.ServerInput{
		ID:        id,
		Name:      id,
		Enabled:   &enabled,
		Transport: mcp.TransportHTTP,
		URL:       "http://" + id + ".local/mcp",
	}); err != nil {
		t.Fatalf("CreateServer() error = %v", err)
	}
}
