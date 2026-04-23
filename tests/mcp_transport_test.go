package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local-agent/internal/tools/mcp"
)

func TestMCPHTTPTransportListAndCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTransportJSON(t, w, map[string]any{"status": "ok"})
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		switch body["method"] {
		case "tools/list":
			writeTransportJSON(t, w, map[string]any{
				"jsonrpc": "2.0",
				"id":      body["id"],
				"result": map[string]any{
					"tools": []map[string]any{
						{"name": "echo", "metadata": map[string]any{"effects": []string{"fs.read"}, "approval": "auto"}},
					},
				},
			})
		case "tools/call":
			writeTransportJSON(t, w, map[string]any{
				"jsonrpc": "2.0",
				"id":      body["id"],
				"result": map[string]any{
					"structured": map[string]any{"ok": true},
					"content":    "done",
				},
			})
		default:
			t.Fatalf("unexpected method: %v", body["method"])
		}
	}))
	defer server.Close()

	transport := mcp.NewHTTPTransport(mcp.TransportConfig{URL: server.URL, TimeoutSeconds: 5})
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := transport.Health(context.Background()); err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	tools, err := transport.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", tools)
	}
	result, err := transport.CallTool(context.Background(), "echo", map[string]any{"message": "hi"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.Structured["ok"] != true || result.Content != "done" {
		t.Fatalf("result = %+v", result)
	}
}

func TestMCPStdioTransportListAndCall(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "mock-mcp.sh")
	body := `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *tools/list*)
      printf '%s\n' '{"jsonrpc":"2.0","id":"1","result":{"tools":[{"name":"echo","metadata":{"effects":["fs.read"],"approval":"auto"}}]}}'
      ;;
    *tools/call*)
      printf '%s\n' '{"jsonrpc":"2.0","id":"2","result":{"structured":{"ok":true},"content":"done"}}'
      ;;
    *)
      printf '%s\n' '{"jsonrpc":"2.0","id":"0","error":{"code":-32601,"message":"unknown method"}}'
      ;;
  esac
done
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	transport := mcp.NewStdioTransport(mcp.TransportConfig{Command: script, TimeoutSeconds: 5})
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer transport.Close(context.Background())

	tools, err := transport.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", tools)
	}
	result, err := transport.CallTool(context.Background(), "echo", map[string]any{"message": "hi"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.Structured["ok"] != true || !strings.Contains(result.Content.(string), "done") {
		t.Fatalf("result = %+v", result)
	}
}

func writeTransportJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
}
