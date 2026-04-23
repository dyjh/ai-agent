package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"local-agent/internal/tools/mcp"
)

// MCPHandler serves MCP config APIs.
type MCPHandler struct {
	Base
}

// NewMCPHandler creates an MCP handler.
func NewMCPHandler(deps Dependencies) *MCPHandler {
	return &MCPHandler{Base{Deps: deps}}
}

// ListServers handles GET /v1/mcp/servers.
func (h *MCPHandler) ListServers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": h.Deps.MCP.ListServers()})
}

// CreateServer handles POST /v1/mcp/servers.
func (h *MCPHandler) CreateServer(w http.ResponseWriter, r *http.Request) {
	var body mcp.ServerInput
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	item, err := h.Deps.MCP.CreateServer(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// UpdateServer handles PATCH /v1/mcp/servers/{id}.
func (h *MCPHandler) UpdateServer(w http.ResponseWriter, r *http.Request) {
	var body mcp.ServerInput
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	item, err := h.Deps.MCP.UpdateServer(chi.URLParam(r, "id"), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// ListPolicies handles GET /v1/mcp/tools.
func (h *MCPHandler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": h.Deps.MCP.ListToolPolicies()})
}

// UpdatePolicy handles PATCH /v1/mcp/tools/{id}/policy.
func (h *MCPHandler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RequiresApproval bool   `json:"requires_approval"`
		RiskLevel        string `json:"risk_level"`
		Reason           string `json:"reason"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	item := h.Deps.MCP.UpdateToolPolicy(chi.URLParam(r, "id"), body.RequiresApproval, body.RiskLevel, body.Reason)
	writeJSON(w, http.StatusOK, item)
}
