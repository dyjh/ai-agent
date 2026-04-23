package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"local-agent/internal/agent"
	"local-agent/internal/security"
)

// RunsHandler serves run inspection and recovery APIs.
type RunsHandler struct {
	Base
}

// NewRunsHandler creates a runs handler.
func NewRunsHandler(deps Dependencies) *RunsHandler {
	return &RunsHandler{Base{Deps: deps}}
}

// List handles GET /v1/runs.
func (h *RunsHandler) List(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "workflow_unavailable", "runtime is not configured", nil)
		return
	}
	items, err := h.Deps.Runtime.ListRuns(r.Context(), parseRunStatuses(r.URL.Query().Get("status")), parseLimit(r.URL.Query().Get("limit")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "run_list_failed", err.Error(), nil)
		return
	}
	sanitized := make([]agent.RunState, 0, len(items))
	for _, item := range items {
		sanitized = append(sanitized, sanitizeRunState(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": sanitized})
}

// Get handles GET /v1/runs/{run_id}.
func (h *RunsHandler) Get(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "workflow_unavailable", "runtime is not configured", nil)
		return
	}
	state, err := h.Deps.Runtime.GetRun(r.Context(), chi.URLParam(r, "run_id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "run_not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, sanitizeRunState(*state))
}

// Steps handles GET /v1/runs/{run_id}/steps.
func (h *RunsHandler) Steps(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "workflow_unavailable", "runtime is not configured", nil)
		return
	}
	items, err := h.Deps.Runtime.ListRunSteps(r.Context(), chi.URLParam(r, "run_id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "run_not_found", err.Error(), nil)
		return
	}
	sanitized := make([]agent.RunStep, 0, len(items))
	for _, item := range items {
		sanitized = append(sanitized, sanitizeRunStep(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": sanitized})
}

// Resume handles POST /v1/runs/{run_id}/resume.
func (h *RunsHandler) Resume(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "workflow_unavailable", "runtime is not configured", nil)
		return
	}
	var body struct {
		ApprovalID string `json:"approval_id"`
		Approved   bool   `json:"approved"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error(), nil)
		return
	}
	runID := chi.URLParam(r, "run_id")
	if body.ApprovalID == "" {
		state, err := h.Deps.Runtime.GetRun(r.Context(), runID)
		if err != nil {
			writeError(w, http.StatusNotFound, "run_not_found", err.Error(), nil)
			return
		}
		body.ApprovalID = state.ApprovalID
	}
	if body.ApprovalID == "" {
		writeError(w, http.StatusBadRequest, "approval_required", "approval_id is required for resume", nil)
		return
	}
	response, err := h.Deps.Runtime.ResumeRun(r.Context(), runID, body.ApprovalID, body.Approved, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "resume_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// Cancel handles POST /v1/runs/{run_id}/cancel.
func (h *RunsHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "workflow_unavailable", "runtime is not configured", nil)
		return
	}
	state, err := h.Deps.Runtime.CancelRun(r.Context(), chi.URLParam(r, "run_id"), nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "cancel_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, sanitizeRunState(*state))
}

func parseRunStatuses(raw string) []agent.RunStatus {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	items := make([]agent.RunStatus, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		items = append(items, agent.RunStatus(part))
	}
	return items
}

func parseLimit(raw string) int {
	if raw == "" {
		return 20
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 20
	}
	return limit
}

func sanitizeRunState(input agent.RunState) agent.RunState {
	cp := cloneJSON(input)
	cp.UserMessage = security.RedactString(cp.UserMessage)
	cp.Error = security.RedactString(cp.Error)
	for idx := range cp.Context.Messages {
		if cp.Context.Messages[idx] == nil {
			continue
		}
		cp.Context.Messages[idx].Content = security.RedactString(cp.Context.Messages[idx].Content)
	}
	if cp.Plan != nil {
		cp.Plan.Message = security.RedactString(cp.Plan.Message)
		cp.Plan.Reason = security.RedactString(cp.Plan.Reason)
		if cp.Plan.ToolProposal != nil {
			cp.Plan.ToolProposal.Input = security.RedactMap(cp.Plan.ToolProposal.Input)
		}
	}
	if cp.Proposal != nil {
		cp.Proposal.Input = security.RedactMap(cp.Proposal.Input)
	}
	if cp.Policy != nil {
		cp.Policy.Reason = security.RedactString(cp.Policy.Reason)
		cp.Policy.ApprovalPayload = security.RedactMap(cp.Policy.ApprovalPayload)
	}
	if cp.ToolResult != nil {
		cp.ToolResult.Output = security.RedactMap(cp.ToolResult.Output)
		cp.ToolResult.Error = security.RedactString(cp.ToolResult.Error)
	}
	return cp
}

func sanitizeRunStep(input agent.RunStep) agent.RunStep {
	cp := cloneJSON(input)
	cp.Summary = security.RedactString(cp.Summary)
	cp.Error = security.RedactString(cp.Error)
	if cp.Proposal != nil {
		cp.Proposal.Input = security.RedactMap(cp.Proposal.Input)
	}
	if cp.Policy != nil {
		cp.Policy.Reason = security.RedactString(cp.Policy.Reason)
		cp.Policy.ApprovalPayload = security.RedactMap(cp.Policy.ApprovalPayload)
	}
	if cp.Approval != nil {
		cp.Approval.Summary = security.RedactString(cp.Approval.Summary)
		cp.Approval.Reason = security.RedactString(cp.Approval.Reason)
		cp.Approval.InputSnapshot = security.RedactMap(cp.Approval.InputSnapshot)
		cp.Approval.Proposal.Input = security.RedactMap(cp.Approval.Proposal.Input)
	}
	if cp.ToolResult != nil {
		cp.ToolResult.Output = security.RedactMap(cp.ToolResult.Output)
		cp.ToolResult.Error = security.RedactString(cp.ToolResult.Error)
	}
	return cp
}

func cloneJSON[T any](input T) T {
	var out T
	raw, err := json.Marshal(input)
	if err != nil {
		return input
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return input
	}
	return out
}
