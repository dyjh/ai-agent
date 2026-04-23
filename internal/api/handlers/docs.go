package handlers

import (
	"encoding/json"
	"net/http"

	"local-agent/internal/openapi"
)

// DocsHandler serves OpenAPI and Swagger UI assets.
type DocsHandler struct{}

// NewDocsHandler creates a docs handler.
func NewDocsHandler() *DocsHandler {
	return &DocsHandler{}
}

// Redirect sends /swagger to the browser UI.
func (h *DocsHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/swagger/index.html", http.StatusFound)
}

// DocJSON serves the generated OpenAPI document.
func (h *DocsHandler) DocJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(openapi.Spec())
}

// Index serves a small Swagger UI shell backed by /swagger/doc.json.
func (h *DocsHandler) Index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(swaggerHTML))
}

const swaggerHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Local Agent API Docs</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    body { margin: 0; background: #ffffff; }
    #swagger-ui { min-height: 100vh; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function() {
      SwaggerUIBundle({ url: "/swagger/doc.json", dom_id: "#swagger-ui" });
    };
  </script>
</body>
</html>`
