package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local-agent/internal/api"
	"local-agent/internal/openapi"
)

func TestSwaggerRoutesAndSpec(t *testing.T) {
	server := httptest.NewServer(api.NewRouter(api.Dependencies{}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/swagger/index.html")
	if err != nil {
		t.Fatalf("GET swagger UI: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("swagger UI status = %d", resp.StatusCode)
	}
	html, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(html), "SwaggerUIBundle") {
		t.Fatalf("swagger UI shell does not contain SwaggerUIBundle")
	}

	resp, err = http.Get(server.URL + "/swagger/doc.json")
	if err != nil {
		t.Fatalf("GET swagger doc: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("swagger doc status = %d", resp.StatusCode)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode swagger doc: %v", err)
	}
	paths := doc["paths"].(map[string]any)
	for _, path := range []string{
		"/v1/health",
		"/v1/conversations/{conversation_id}/messages",
		"/v1/kbs/{kb_id}/search",
		"/v1/skills/{id}/run",
		"/v1/mcp/servers/{id}/tools/{tool_name}/call",
	} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("OpenAPI path %s missing", path)
		}
	}
}

func TestOpenAPIGenerationWritesDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.json")
	if err := openapi.WriteFile(path); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(raw), `"/v1/health"`) {
		t.Fatalf("generated OpenAPI document missing /v1/health")
	}
}
