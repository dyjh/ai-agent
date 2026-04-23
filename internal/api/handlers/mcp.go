package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"local-agent/internal/core"
	"local-agent/internal/ids"
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

// GetServer handles GET /v1/mcp/servers/{id}.
func (h *MCPHandler) GetServer(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.MCP.GetServer(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	state, _ := h.Deps.MCP.RuntimeState(chi.URLParam(r, "id"))
	writeJSON(w, http.StatusOK, map[string]any{
		"server": item,
		"state":  state,
	})
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

// RefreshServer handles POST /v1/mcp/servers/{id}/refresh.
func (h *MCPHandler) RefreshServer(w http.ResponseWriter, r *http.Request) {
	tools, err := h.Deps.MCP.RefreshTools(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	state, _ := h.Deps.MCP.RuntimeState(chi.URLParam(r, "id"))
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"tools":  tools,
		"state":  state,
	})
}

// TestServer handles POST /v1/mcp/servers/{id}/test.
func (h *MCPHandler) TestServer(w http.ResponseWriter, r *http.Request) {
	if err := h.Deps.MCP.Health(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// ListPolicies handles GET /v1/mcp/tools.
func (h *MCPHandler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": h.Deps.MCP.ListToolPolicies()})
}

// UpdatePolicy handles PATCH /v1/mcp/tools/{id}/policy.
func (h *MCPHandler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	var body mcp.ToolPolicyInput
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	item, err := h.Deps.MCP.UpdateToolPolicyInput(chi.URLParam(r, "id"), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// CallTool handles POST /v1/mcp/servers/{id}/tools/{tool_name}/call.
func (h *MCPHandler) CallTool(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Arguments map[string]any `json:"arguments"`
		Purpose   string         `json:"purpose"`
	}
	_ = decodeJSON(r, &body)
	if body.Arguments == nil {
		body.Arguments = map[string]any{}
	}
	purpose := body.Purpose
	if purpose == "" {
		purpose = "通过 MCP API 调用外部工具"
	}

	proposal := core.ToolProposal{
		ID:   ids.New("tool"),
		Tool: "mcp.call_tool",
		Input: map[string]any{
			"server_id": chi.URLParam(r, "id"),
			"tool_name": chi.URLParam(r, "tool_name"),
			"arguments": body.Arguments,
		},
		Purpose:   purpose,
		CreatedAt: time.Now().UTC(),
	}

	outcome, err := h.Deps.Router.Propose(r.Context(), "", "", proposal)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if outcome.Approval != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"approval":  outcome.Approval,
			"decision":  outcome.Decision,
			"inference": outcome.Inference,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"decision":  outcome.Decision,
		"inference": outcome.Inference,
		"result":    outcome.Result,
	})
}
