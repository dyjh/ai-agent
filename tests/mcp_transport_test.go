package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
						{"name": "echo", "inputSchema": map[string]any{"type": "object"}, "metadata": map[string]any{"effects": []string{"fs.read"}, "approval": "auto"}},
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

func TestMCPSSETransportListAndCall(t *testing.T) {
	harness := &sseHarness{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sse":
			harness.serveSSE(w, r)
		case "/message":
			harness.serveMessage(t, w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	transport := mcp.NewSSETransport(mcp.TransportConfig{
		URL:            server.URL + "/sse",
		MessageURL:     server.URL + "/message",
		TimeoutSeconds: 5,
	})
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer transport.Close(context.Background())
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
  id=$(printf '%s' "$line" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  case "$line" in
    *tools/list*)
      printf '%s\n' '{"jsonrpc":"2.0","id":"'"$id"'","result":{"tools":[{"name":"echo","inputSchema":{"type":"object"},"metadata":{"effects":["fs.read"],"approval":"auto"}}]}}'
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

type sseHarness struct {
	mu      sync.Mutex
	clients []chan string
}

func (h *sseHarness) serveSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := make(chan string, 8)
	h.mu.Lock()
	h.clients = append(h.clients, ch)
	h.mu.Unlock()
	defer h.remove(ch)

	io.WriteString(w, "event: endpoint\n")
	io.WriteString(w, "data: /message\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case frame := <-ch:
			io.WriteString(w, frame)
			flusher.Flush()
		}
	}
}

func (h *sseHarness) serveMessage(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	id, _ := body["id"].(string)
	switch body["method"] {
	case "initialize":
		h.send(map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "mock-sse", "version": "test"},
			},
		})
	case "notifications/initialized":
	case "tools/list":
		h.send(map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"tools": []map[string]any{
					{"name": "echo", "inputSchema": map[string]any{"type": "object"}, "metadata": map[string]any{"effects": []string{"fs.read"}, "approval": "auto"}},
				},
			},
		})
	case "tools/call":
		h.send(map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"structured": map[string]any{"ok": true},
				"content":    "done",
			},
		})
	default:
		h.send(map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"error":   map[string]any{"code": -32601, "message": "unknown method"},
		})
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *sseHarness) send(payload map[string]any) {
	raw, _ := json.Marshal(payload)
	frame := "event: message\n" + "data: " + string(raw) + "\n\n"
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.clients {
		select {
		case ch <- frame:
		default:
		}
	}
}

func (h *sseHarness) remove(target chan string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, ch := range h.clients {
		if ch == target {
			h.clients = append(h.clients[:i], h.clients[i+1:]...)
			return
		}
	}
}
