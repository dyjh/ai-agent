package tests

import (
	"os"
	"path/filepath"
	"testing"

	"local-agent/internal/tools/mcp"
)

func TestMCPConfigParsing(t *testing.T) {
	t.Setenv("MCP_LOCAL_TOKEN", "secret-token")
	root := t.TempDir()
	serversPath := filepath.Join(root, "mcp.servers.yaml")
	policiesPath := filepath.Join(root, "mcp.tool-policies.yaml")

	writeFile(t, serversPath, `
servers:
  - id: filesystem
    name: filesystem
    enabled: true
    transport: stdio
    command: node
    args:
      - ./index.js
    cwd: ./mcp/filesystem
    env:
      ROOT_DIR: ./workspace
  - id: local-http-tools
    name: local-http-tools
    enabled: true
    transport: http
    url: http://localhost:3001/mcp
    dialect: line_delimited_jsonrpc
    compatibility:
      accept_extra_metadata: true
      accept_text_only_result: true
      strict_id_matching: false
      max_payload_bytes: 4096
    headers:
      Authorization: ${MCP_LOCAL_TOKEN}
  - id: disabled
    name: disabled
    enabled: false
    transport: http
    url: http://localhost:3002/mcp
  - id: codegen-sse
    name: codegen-sse
    enabled: true
    transport: sse
    url: http://localhost:8085/sse
    message_url: http://localhost:8085/message
`)
	writeFile(t, policiesPath, `
tools:
  mcp.filesystem.read_file:
    effects:
      - fs.read
    approval: auto
`)

	manager := mcp.NewManager()
	if err := manager.LoadConfig(serversPath, policiesPath); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	servers := manager.ListServers()
	if len(servers) != 4 {
		t.Fatalf("server count = %d, want 4", len(servers))
	}
	httpServer, err := manager.GetServer("local-http-tools")
	if err != nil {
		t.Fatalf("GetServer() error = %v", err)
	}
	if httpServer.Headers["Authorization"] != "[REDACTED]" {
		t.Fatalf("authorization header was not redacted: %v", httpServer.Headers)
	}
	if httpServer.Dialect != mcp.DialectLineDelimitedJSONRPC {
		t.Fatalf("dialect = %s, want %s", httpServer.Dialect, mcp.DialectLineDelimitedJSONRPC)
	}
	if !httpServer.Compatibility.AcceptExtraMetadata || !httpServer.Compatibility.AcceptTextOnlyResult {
		t.Fatalf("compatibility flags not parsed: %+v", httpServer.Compatibility)
	}
	if httpServer.Compatibility.StrictIDMatching {
		t.Fatalf("strict_id_matching should be configurable: %+v", httpServer.Compatibility)
	}
	if httpServer.Compatibility.MaxPayloadBytes != 4096 {
		t.Fatalf("max_payload_bytes = %d, want 4096", httpServer.Compatibility.MaxPayloadBytes)
	}
	stdioServer, err := manager.GetServer("filesystem")
	if err != nil {
		t.Fatalf("GetServer(filesystem) error = %v", err)
	}
	if stdioServer.Command != "node" || stdioServer.Args[0] != "./index.js" {
		t.Fatalf("stdio server not parsed: %+v", stdioServer)
	}
	sseServer, err := manager.GetServer("codegen-sse")
	if err != nil {
		t.Fatalf("GetServer(codegen-sse) error = %v", err)
	}
	if sseServer.Transport != mcp.TransportSSE || sseServer.MessageURL != "http://localhost:8085/message" {
		t.Fatalf("sse server not parsed: %+v", sseServer)
	}

	if _, err := manager.PolicyProfile("disabled", "anything"); err == nil {
		t.Fatalf("disabled server should not be callable")
	}
	profile, err := manager.PolicyProfile("filesystem", "read_file")
	if err != nil {
		t.Fatalf("PolicyProfile() error = %v", err)
	}
	if !containsString(profile.Effects, "fs.read") || profile.RequiresApproval {
		t.Fatalf("policy profile = %+v, want fs.read auto", profile)
	}
}

func TestMCPConfigMissingRequiredFields(t *testing.T) {
	root := t.TempDir()
	policiesPath := filepath.Join(root, "mcp.tool-policies.yaml")
	writeFile(t, policiesPath, "tools: {}\n")

	testCases := []struct {
		name string
		body string
	}{
		{
			name: "stdio missing command",
			body: `
servers:
  - id: bad-stdio
    name: bad-stdio
    transport: stdio
`,
		},
		{
			name: "http missing url",
			body: `
servers:
  - id: bad-http
    name: bad-http
    transport: http
`,
		},
		{
			name: "invalid dialect",
			body: `
servers:
  - id: bad-dialect
    name: bad-dialect
    transport: http
    url: http://localhost:3001/mcp
    dialect: loose-json
`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			serversPath := filepath.Join(root, tc.name+".yaml")
			writeFile(t, serversPath, tc.body)
			manager := mcp.NewManager()
			if err := manager.LoadConfig(serversPath, policiesPath); err == nil {
				t.Fatalf("expected config error")
			}
		})
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
