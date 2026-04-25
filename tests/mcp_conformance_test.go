package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"local-agent/internal/tools/mcp"
)

func TestMCPHTTPConformanceMatrix(t *testing.T) {
	testCases := []struct {
		name     string
		dialect  string
		profile  func(mcp.MCPCompatibilityProfile) mcp.MCPCompatibilityProfile
		fixture  func(t *testing.T, w http.ResponseWriter, body map[string]any)
		call     bool
		wantTool string
		wantText string
		wantCode mcp.MCPErrorCode
		timeout  bool
	}{
		{
			name:     "strict json-rpc list tools",
			dialect:  mcp.DialectStrictJSONRPC,
			wantTool: "echo",
			fixture: func(t *testing.T, w http.ResponseWriter, body map[string]any) {
				writeTransportJSON(t, w, rpcResult(body, map[string]any{
					"tools": []map[string]any{{
						"name":        "echo",
						"description": "echo input",
						"inputSchema": map[string]any{"type": "object"},
						"annotations": map[string]any{"readOnlyHint": true},
					}},
				}))
			},
		},
		{
			name:     "line-delimited json-rpc list tools",
			dialect:  mcp.DialectLineDelimitedJSONRPC,
			wantTool: "echo",
			fixture: func(t *testing.T, w http.ResponseWriter, body map[string]any) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"` + body["id"].(string) + `","result":{"tools":[{"name":"echo"}]}}` + "\n"))
			},
		},
		{
			name:     "envelope wrapped list tools",
			dialect:  mcp.DialectEnvelopeWrapped,
			wantTool: "echo",
			fixture: func(t *testing.T, w http.ResponseWriter, body map[string]any) {
				writeTransportJSON(t, w, map[string]any{
					"data": map[string]any{
						"tools": []map[string]any{{"name": "echo", "extra": "kept"}},
					},
				})
			},
		},
		{
			name:     "malformed tool entry",
			dialect:  mcp.DialectStrictJSONRPC,
			wantCode: mcp.MCPErrorInvalidResponse,
			fixture: func(t *testing.T, w http.ResponseWriter, body map[string]any) {
				writeTransportJSON(t, w, rpcResult(body, map[string]any{
					"tools": []map[string]any{{"description": "missing name"}},
				}))
			},
		},
		{
			name:     "strict structured call result",
			dialect:  mcp.DialectStrictJSONRPC,
			call:     true,
			wantText: "done",
			fixture: func(t *testing.T, w http.ResponseWriter, body map[string]any) {
				writeTransportJSON(t, w, rpcResult(body, map[string]any{
					"structured": map[string]any{"ok": true},
					"content":    "done",
				}))
			},
		},
		{
			name:    "text-only content result",
			dialect: mcp.DialectStrictJSONRPC,
			call:    true,
			profile: func(profile mcp.MCPCompatibilityProfile) mcp.MCPCompatibilityProfile {
				profile.AcceptTextOnlyResult = true
				return profile
			},
			wantText: "hello",
			fixture: func(t *testing.T, w http.ResponseWriter, body map[string]any) {
				writeTransportJSON(t, w, rpcResult(body, map[string]any{
					"content": []map[string]any{{"type": "text", "text": "hello"}},
				}))
			},
		},
		{
			name:     "json-rpc error mapping",
			dialect:  mcp.DialectStrictJSONRPC,
			call:     true,
			wantCode: mcp.MCPErrorServerError,
			fixture: func(t *testing.T, w http.ResponseWriter, body map[string]any) {
				writeTransportJSON(t, w, map[string]any{
					"jsonrpc": "2.0",
					"id":      body["id"],
					"error":   map[string]any{"code": -32602, "message": "bad input"},
				})
			},
		},
		{
			name:     "invalid json response",
			dialect:  mcp.DialectStrictJSONRPC,
			wantCode: mcp.MCPErrorInvalidResponse,
			fixture: func(t *testing.T, w http.ResponseWriter, body map[string]any) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("{not-json"))
			},
		},
		{
			name:     "network timeout",
			dialect:  mcp.DialectStrictJSONRPC,
			call:     true,
			timeout:  true,
			wantCode: mcp.MCPErrorTimeout,
			fixture: func(t *testing.T, w http.ResponseWriter, body map[string]any) {
				time.Sleep(100 * time.Millisecond)
				writeTransportJSON(t, w, rpcResult(body, map[string]any{"content": "late"}))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					writeTransportJSON(t, w, map[string]any{"status": "ok"})
					return
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("Decode() error = %v", err)
				}
				tc.fixture(t, w, body)
			}))
			defer server.Close()

			profile := testMCPProfile(tc.dialect)
			if tc.profile != nil {
				profile = tc.profile(profile)
			}
			transport := mcp.NewHTTPTransport(mcp.TransportConfig{
				URL:           server.URL,
				Compatibility: profile,
			})
			if err := transport.Start(context.Background()); err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			ctx := context.Background()
			if tc.timeout {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 10*time.Millisecond)
				defer cancel()
			}

			var err error
			if tc.call {
				var result *mcp.MCPToolResult
				result, err = transport.CallTool(ctx, "echo", map[string]any{"message": "hi"})
				if tc.wantText != "" && err == nil && !strings.Contains(result.Content.(string), tc.wantText) {
					t.Fatalf("content = %+v, want %q", result.Content, tc.wantText)
				}
			} else {
				var tools []mcp.MCPToolSchema
				tools, err = transport.ListTools(ctx)
				if tc.wantTool != "" && err == nil && (len(tools) != 1 || tools[0].Name != tc.wantTool) {
					t.Fatalf("tools = %+v, want %s", tools, tc.wantTool)
				}
			}
			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			assertMCPErrorCode(t, err, tc.wantCode)
		})
	}
}

func TestMCPStdioConformanceProcessExitEarly(t *testing.T) {
	transport := mcp.NewStdioTransport(mcp.TransportConfig{
		Command:       "/bin/sh",
		Args:          []string{"-c", "exit 1"},
		Compatibility: testMCPProfile(mcp.DialectStrictJSONRPC),
	})
	err := transport.Start(context.Background())
	if err == nil {
		defer transport.Close(context.Background())
		_, err = transport.ListTools(context.Background())
	}
	assertMCPErrorCode(t, err, mcp.MCPErrorTransportFailure)
}

func TestMCPStdioConformanceStrictListAndCall(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "mock-mcp.sh")
	body := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  case "$line" in
    *tools/list*)
      printf '%s\n' '{"jsonrpc":"2.0","id":"'"$id"'","result":{"tools":[{"name":"echo","inputSchema":{"type":"object"}}]}}'
      ;;
    *tools/call*)
      printf '%s\n' '{"jsonrpc":"2.0","id":"'"$id"'","result":{"structured":{"ok":true},"content":"done"}}'
      ;;
    *)
      printf '%s\n' '{"jsonrpc":"2.0","id":"'"$id"'","error":{"code":-32601,"message":"unknown method"}}'
      ;;
  esac
done
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	transport := mcp.NewStdioTransport(mcp.TransportConfig{
		Command:       script,
		Compatibility: testMCPProfile(mcp.DialectStrictJSONRPC),
	})
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
	if result.Structured["ok"] != true || result.Content != "done" {
		t.Fatalf("result = %+v", result)
	}
}

func testMCPProfile(dialect string) mcp.MCPCompatibilityProfile {
	return mcp.MCPCompatibilityProfile{
		Dialect:                dialect,
		AcceptMissingSchema:    true,
		AcceptExtraMetadata:    true,
		AcceptTextOnlyResult:   false,
		AcceptStructuredResult: true,
		NormalizeErrorShape:    true,
		StrictIDMatching:       true,
		MaxPayloadBytes:        mcp.DefaultMaxPayloadBytes,
		TimeoutSeconds:         1,
	}
}

func rpcResult(request map[string]any, result any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      request["id"],
		"result":  result,
	}
}

func assertMCPErrorCode(t *testing.T, err error, code mcp.MCPErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected MCP error code %s", code)
	}
	var mcpErr *mcp.MCPError
	if !errors.As(err, &mcpErr) {
		t.Fatalf("error = %T %v, want MCPError code %s", err, err, code)
	}
	if mcpErr.Code != code {
		t.Fatalf("error code = %s, want %s (err=%v)", mcpErr.Code, code, err)
	}
}
