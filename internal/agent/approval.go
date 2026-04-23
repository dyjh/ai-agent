package agent

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/ids"
)

// ApprovalCenter stores exact input snapshots and their resolution state.
type ApprovalCenter struct {
	mu        sync.RWMutex
	approvals map[string]core.ApprovalRecord
}

// NewApprovalCenter constructs an in-memory approval store.
func NewApprovalCenter() *ApprovalCenter {
	return &ApprovalCenter{
		approvals: map[string]core.ApprovalRecord{},
	}
}

// Create stores a new approval request with an immutable input snapshot.
func (a *ApprovalCenter) Create(runID, conversationID string, proposal core.ToolProposal, inference core.EffectInferenceResult, decision core.PolicyDecision) (*core.ApprovalRecord, error) {
	record := core.ApprovalRecord{
		ID:             ids.New("apr"),
		RunID:          runID,
		ConversationID: conversationID,
		Proposal:       proposal,
		Inference:      inference,
		Decision:       decision,
		InputSnapshot:  core.CloneMap(proposal.Input),
		Summary:        approvalSummary(proposal, inference),
		Status:         core.ApprovalPending,
		CreatedAt:      time.Now().UTC(),
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.approvals[record.ID] = record

	cp := record
	return &cp, nil
}

// Get returns an approval record by ID.
func (a *ApprovalCenter) Get(id string) (*core.ApprovalRecord, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	record, ok := a.approvals[id]
	if !ok {
		return nil, errors.New("approval not found")
	}
	cp := record
	return &cp, nil
}

// Pending returns all unresolved approvals.
func (a *ApprovalCenter) Pending() ([]core.ApprovalRecord, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	items := make([]core.ApprovalRecord, 0, len(a.approvals))
	for _, record := range a.approvals {
		if record.Status == core.ApprovalPending {
			items = append(items, record)
		}
	}
	return items, nil
}

// Approve marks an approval as approved.
func (a *ApprovalCenter) Approve(id string) (*core.ApprovalRecord, error) {
	return a.resolve(id, true, "")
}

// Reject marks an approval as rejected.
func (a *ApprovalCenter) Reject(id, reason string) (*core.ApprovalRecord, error) {
	return a.resolve(id, false, reason)
}

// SnapshotMatches reports whether a proposal matches the exact approved snapshot.
func (a *ApprovalCenter) SnapshotMatches(id string, proposal core.ToolProposal) (bool, error) {
	record, err := a.Get(id)
	if err != nil {
		return false, err
	}
	if record.Proposal.Tool != proposal.Tool {
		return false, nil
	}
	return core.MapsEqual(record.InputSnapshot, proposal.Input), nil
}

func (a *ApprovalCenter) resolve(id string, approved bool, reason string) (*core.ApprovalRecord, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.approvals[id]
	if !ok {
		return nil, errors.New("approval not found")
	}
	if record.Status != core.ApprovalPending {
		return nil, fmt.Errorf("approval %s already resolved", id)
	}

	now := time.Now().UTC()
	record.ResolvedAt = &now
	record.Reason = reason
	if approved {
		record.Status = core.ApprovalApproved
	} else {
		record.Status = core.ApprovalRejected
	}
	a.approvals[id] = record

	cp := record
	return &cp, nil
}

func approvalSummary(proposal core.ToolProposal, inference core.EffectInferenceResult) string {
	switch proposal.Tool {
	case "shell.exec":
		if command, ok := proposal.Input["command"].(string); ok {
			return fmt.Sprintf("准备执行 shell 命令 `%s`，风险等级为 %s。", command, inference.RiskLevel)
		}
	case "code.apply_patch":
		if path, ok := proposal.Input["path"].(string); ok {
			return fmt.Sprintf("准备修改工作区文件 `%s`。", path)
		}
	case "memory.patch":
		if path, ok := proposal.Input["path"].(string); ok {
			return fmt.Sprintf("准备修改 Markdown memory `%s`。", path)
		}
	}
	return fmt.Sprintf("准备执行 `%s`，风险等级为 %s。", proposal.Tool, inference.RiskLevel)
}
