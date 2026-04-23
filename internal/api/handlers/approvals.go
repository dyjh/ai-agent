package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"local-agent/internal/agent"
)

// ApprovalsHandler serves approval APIs.
type ApprovalsHandler struct {
	Base
}

// NewApprovalsHandler creates an approvals handler.
func NewApprovalsHandler(deps Dependencies) *ApprovalsHandler {
	return &ApprovalsHandler{Base{Deps: deps}}
}

// Pending handles GET /v1/approvals/pending.
func (h *ApprovalsHandler) Pending(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Runtime != nil {
		if runs, err := h.Deps.Runtime.ListRuns(r.Context(), []agent.RunStatus{agent.RunStatusPausedForApproval}, 100); err == nil {
			for _, run := range runs {
				if run.ApprovalID == "" {
					continue
				}
				if _, err := h.Deps.Approvals.Get(run.ApprovalID); err == nil {
					continue
				}
				if steps, err := h.Deps.Runtime.ListRunSteps(r.Context(), run.RunID); err == nil {
					for idx := len(steps) - 1; idx >= 0; idx-- {
						if steps[idx].Approval == nil || steps[idx].Approval.ID != run.ApprovalID {
							continue
						}
						_ = h.Deps.Approvals.Hydrate(*steps[idx].Approval)
						break
					}
				}
			}
		}
	}
	items, err := h.Deps.Approvals.Pending()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// Approve handles POST /v1/approvals/{approval_id}/approve.
func (h *ApprovalsHandler) Approve(w http.ResponseWriter, r *http.Request) {
	approvalID := chi.URLParam(r, "approval_id")
	existing, _ := h.Deps.Approvals.Get(approvalID)
	if h.Deps.Runtime != nil && (existing == nil || existing.RunID != "") {
		response, err := h.Deps.Runtime.ResumeApproval(r.Context(), approvalID, true, nil)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"approval": response.Approval,
				"result":   response.ToolResult,
				"run":      response,
			})
			return
		}
		if existing == nil || existing.RunID != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}

	approval, err := h.Deps.Approvals.Approve(approvalID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	result, err := h.Deps.Router.ExecuteApproved(r.Context(), approvalID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approval": approval, "result": result})
}

// Reject handles POST /v1/approvals/{approval_id}/reject.
func (h *ApprovalsHandler) Reject(w http.ResponseWriter, r *http.Request) {
	approvalID := chi.URLParam(r, "approval_id")
	var body struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSON(r, &body)
	existing, _ := h.Deps.Approvals.Get(approvalID)
	if h.Deps.Runtime != nil && (existing == nil || existing.RunID != "") {
		response, err := h.Deps.Runtime.ResumeApproval(r.Context(), approvalID, false, nil)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"approval": response.Approval,
				"run":      response,
			})
			return
		}
		if existing == nil || existing.RunID != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}

	item, err := h.Deps.Approvals.Reject(approvalID, body.Reason)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}
