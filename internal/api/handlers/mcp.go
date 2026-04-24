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
// @Tags MCP
// @Summary List MCP servers
// @Produce application/json
// @Success 200 {object} MCPServerListResponse
// @Router /v1/mcp/servers [get]
func (h *MCPHandler) ListServers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, MCPServerListResponse{Items: h.Deps.MCP.ListServers()})
}

// CreateServer handles POST /v1/mcp/servers.
// @Tags MCP
// @Summary Create MCP server
// @Accept application/json
// @Produce application/json
// @Param body body mcp.ServerInput true "MCP server payload"
// @Success 201 {object} core.MCPServer
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/mcp/servers [post]
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
// @Tags MCP
// @Summary Get MCP server
// @Produce application/json
// @Param id path string true "MCP server ID"
// @Success 200 {object} MCPServerDetailResponse
// @Failure 404 {object} LegacyErrorResponse
// @Router /v1/mcp/servers/{id} [get]
func (h *MCPHandler) GetServer(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.MCP.GetServer(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	state, _ := h.Deps.MCP.RuntimeState(chi.URLParam(r, "id"))
	writeJSON(w, http.StatusOK, MCPServerDetailResponse{
		Server: item,
		State:  state,
	})
}

// UpdateServer handles PATCH /v1/mcp/servers/{id}.
// @Tags MCP
// @Summary Update MCP server
// @Accept application/json
// @Produce application/json
// @Param id path string true "MCP server ID"
// @Param body body mcp.ServerInput true "MCP server payload"
// @Success 200 {object} core.MCPServer
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/mcp/servers/{id} [patch]
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
// @Tags MCP
// @Summary Refresh MCP tools
// @Accept application/json
// @Produce application/json
// @Param id path string true "MCP server ID"
// @Success 200 {object} MCPRefreshResponse
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/mcp/servers/{id}/refresh [post]
func (h *MCPHandler) RefreshServer(w http.ResponseWriter, r *http.Request) {
	tools, err := h.Deps.MCP.RefreshTools(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	state, _ := h.Deps.MCP.RuntimeState(chi.URLParam(r, "id"))
	writeJSON(w, http.StatusOK, MCPRefreshResponse{
		Status: "ok",
		Tools:  tools,
		State:  state,
	})
}

// TestServer handles POST /v1/mcp/servers/{id}/test.
// @Tags MCP
// @Summary Test MCP server connectivity
// @Accept application/json
// @Produce application/json
// @Param id path string true "MCP server ID"
// @Success 200 {object} MCPTestServerResponse
// @Failure 400 {object} MCPTestServerResponse
// @Router /v1/mcp/servers/{id}/test [post]
func (h *MCPHandler) TestServer(w http.ResponseWriter, r *http.Request) {
	if err := h.Deps.MCP.Health(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeJSON(w, http.StatusBadRequest, MCPTestServerResponse{Status: "error", Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, MCPTestServerResponse{Status: "ok"})
}

// ListPolicies handles GET /v1/mcp/tools.
// @Tags MCP
// @Summary List MCP tool policies
// @Produce application/json
// @Success 200 {object} MCPToolPolicyListResponse
// @Router /v1/mcp/tools [get]
func (h *MCPHandler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, MCPToolPolicyListResponse{Items: h.Deps.MCP.ListToolPolicies()})
}

// UpdatePolicy handles PATCH /v1/mcp/tools/{id}/policy.
// @Tags MCP
// @Summary Update MCP tool policy
// @Accept application/json
// @Produce application/json
// @Param id path string true "Tool policy ID"
// @Param body body mcp.ToolPolicyInput true "Policy payload"
// @Success 200 {object} core.MCPToolPolicy
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/mcp/tools/{id}/policy [patch]
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
// @Tags MCP
// @Summary Call MCP tool through approval chain
// @Accept application/json
// @Produce application/json
// @Param id path string true "MCP server ID"
// @Param tool_name path string true "Tool name"
// @Param body body MCPCallToolRequest false "Tool call payload"
// @Success 200 {object} ToolRouteResponse
// @Success 202 {object} ToolRouteResponse
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/mcp/servers/{id}/tools/{tool_name}/call [post]
func (h *MCPHandler) CallTool(w http.ResponseWriter, r *http.Request) {
	var body MCPCallToolRequest
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
		writeJSON(w, http.StatusAccepted, ToolRouteResponse{
			Approval:  outcome.Approval,
			Decision:  &outcome.Decision,
			Inference: &outcome.Inference,
		})
		return
	}
	writeJSON(w, http.StatusOK, ToolRouteResponse{
		Decision:  &outcome.Decision,
		Inference: &outcome.Inference,
		Result:    outcome.Result,
	})
}
