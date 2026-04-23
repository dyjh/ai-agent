package mcp

// ServerInput stores an MCP server create/update payload.
type ServerInput struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command,omitempty"`
	URL       string            `json:"url,omitempty"`
	Enabled   *bool             `json:"enabled,omitempty"`
	Env       map[string]string `json:"environment,omitempty"`
}
