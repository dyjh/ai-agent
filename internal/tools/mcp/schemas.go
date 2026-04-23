package mcp

import (
	"context"
	"time"
)

const (
	// TransportStdio starts a local MCP server process and talks JSON-RPC over stdio.
	TransportStdio = "stdio"
	// TransportHTTP calls an MCP endpoint over HTTP.
	TransportHTTP = "http"

	ApprovalAuto    = "auto"
	ApprovalRequire = "require"
)

// ServerInput stores an MCP server create/update payload.
type ServerInput struct {
	ID             string            `json:"id,omitempty" yaml:"id,omitempty"`
	Name           string            `json:"name" yaml:"name"`
	Transport      string            `json:"transport" yaml:"transport"`
	Command        string            `json:"command,omitempty" yaml:"command,omitempty"`
	Args           []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Cwd            string            `json:"cwd,omitempty" yaml:"cwd,omitempty"`
	URL            string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers        map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Enabled        *bool             `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Env            map[string]string `json:"environment,omitempty" yaml:"env,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
}

// ToolPolicyInput stores a local MCP tool policy override payload.
type ToolPolicyInput struct {
	ID               string   `json:"id,omitempty" yaml:"id,omitempty"`
	ToolName         string   `json:"tool_name,omitempty" yaml:"tool_name,omitempty"`
	Effects          []string `json:"effects,omitempty" yaml:"effects,omitempty"`
	Approval         string   `json:"approval,omitempty" yaml:"approval,omitempty"`
	RequiresApproval *bool    `json:"requires_approval,omitempty" yaml:"requires_approval,omitempty"`
	RiskLevel        string   `json:"risk_level,omitempty" yaml:"risk_level,omitempty"`
	Reason           string   `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// ServersFile is the on-disk config/mcp.servers.yaml shape.
type ServersFile struct {
	Servers []ServerInput `yaml:"servers"`
}

// ToolPoliciesFile is the on-disk config/mcp.tool-policies.yaml shape.
type ToolPoliciesFile struct {
	Tools        map[string]ToolPolicyInput `yaml:"tools"`
	ToolPolicies []ToolPolicyInput          `yaml:"tool_policies"`
}

// MCPToolSchema is the normalized tool schema cached from an MCP server.
type MCPToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// MCPToolResult is the normalized tool call result from an MCP transport.
type MCPToolResult struct {
	Content    any            `json:"content,omitempty"`
	Structured map[string]any `json:"structured,omitempty"`
	Raw        map[string]any `json:"raw,omitempty"`
}

// MCPTransport hides protocol details behind a mockable runtime boundary.
type MCPTransport interface {
	Start(ctx context.Context) error
	ListTools(ctx context.Context) ([]MCPToolSchema, error)
	CallTool(ctx context.Context, name string, input map[string]any) (*MCPToolResult, error)
	Close(ctx context.Context) error
}

// MCPClient selects a configured server transport by server_id.
type MCPClient interface {
	ListTools(ctx context.Context, serverID string) ([]MCPToolSchema, error)
	CallTool(ctx context.Context, serverID string, toolName string, input map[string]any) (*MCPToolResult, error)
	Health(ctx context.Context, serverID string) error
}

// TransportConfig contains a private, non-redacted server snapshot for transport construction.
type TransportConfig struct {
	ServerID       string
	Transport      string
	Command        string
	Args           []string
	Cwd            string
	URL            string
	Headers        map[string]string
	Env            map[string]string
	TimeoutSeconds int
}

// TransportFactory builds transports; tests can replace it with a mock factory.
type TransportFactory interface {
	NewTransport(cfg TransportConfig) (MCPTransport, error)
}

// TransportFactoryFunc adapts a function into a TransportFactory.
type TransportFactoryFunc func(cfg TransportConfig) (MCPTransport, error)

// NewTransport creates a transport.
func (f TransportFactoryFunc) NewTransport(cfg TransportConfig) (MCPTransport, error) {
	return f(cfg)
}

// RuntimeState captures non-secret manager runtime state for API responses.
type RuntimeState struct {
	ServerID      string          `json:"server_id"`
	LastRefreshAt *time.Time      `json:"last_refresh_at,omitempty"`
	ToolCount     int             `json:"tool_count"`
	Tools         []MCPToolSchema `json:"tools,omitempty"`
}
