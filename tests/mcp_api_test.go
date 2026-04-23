package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"local-agent/internal/api"
	"local-agent/internal/tools/mcp"
)

func TestMCPAPIRefreshTestAndCall(t *testing.T) {
	factory := newMockMCPFactory()
	manager := mcp.NewManager(mcp.WithTransportFactory(factory))
	createMCPServer(t, manager, "filesystem", true)
	createMCPServer(t, manager, "disabled", false)
	if _, err := manager.UpdateToolPolicyInput("mcp.filesystem.read_file", mcp.ToolPolicyInput{
		Effects:  []string{"fs.read"},
		Approval: mcp.ApprovalAuto,
	}); err != nil {
		t.Fatalf("UpdateToolPolicyInput() error = %v", err)
	}
	factory.transport("filesystem").setTools([]mcp.MCPToolSchema{
		{Name: "read_file", Metadata: map[string]any{"effects": []any{"fs.read"}, "approval": "auto"}},
	})
	factory.transport("filesystem").setResult("read_file", &mcp.MCPToolResult{
		Structured: map[string]any{"content": "hello"},
	})

	router, approvals := newMCPRouterFixture(t, manager)
	server := httptest.NewServer(api.NewRouter(api.Dependencies{
		Approvals: approvals,
		Router:    router,
		MCP:       manager,
	}))
	defer server.Close()

	var refresh map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/mcp/servers/filesystem/refresh", map[string]any{}, &refresh)
	if refresh["status"] != "ok" {
		t.Fatalf("refresh status = %v", refresh["status"])
	}

	var health map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/mcp/servers/filesystem/test", map[string]any{}, &health)
	if health["status"] != "ok" {
		t.Fatalf("health status = %v", health["status"])
	}

	var call map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/mcp/servers/filesystem/tools/read_file/call", map[string]any{
		"arguments": map[string]any{"path": "README.md"},
	}, &call)
	if _, ok := call["result"].(map[string]any); !ok {
		t.Fatalf("expected result payload, got %+v", call)
	}
	if calls := factory.transport("filesystem").callCount(); calls != 1 {
		t.Fatalf("call count = %d, want 1", calls)
	}

	assertStatusAtLeast(t, http.MethodPost, server.URL+"/v1/mcp/servers/missing/refresh", map[string]any{}, 400)
	assertStatusAtLeast(t, http.MethodPost, server.URL+"/v1/mcp/servers/disabled/test", map[string]any{}, 400)
	assertStatusAtLeast(t, http.MethodPost, server.URL+"/v1/mcp/servers/disabled/tools/read_file/call", map[string]any{
		"arguments": map[string]any{},
	}, 400)
}

func assertStatusAtLeast(t *testing.T, method, url string, body any, want int) {
	t.Helper()
	status, payload := doJSON(t, method, url, body)
	if status < want {
		t.Fatalf("status = %d body = %s, want >= %d", status, payload, want)
	}
}

func doJSON(t *testing.T, method, url string, body any) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}
