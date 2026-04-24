package agent

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/ids"
)

var errApprovalNotFound = errors.New("approval not found")

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
	snapshot := normalizedApprovalInput(proposal)
	hash, err := core.HashMap(snapshot)
	if err != nil {
		return nil, err
	}
	storedProposal := proposal
	storedProposal.Input = core.CloneMap(snapshot)
	record := core.ApprovalRecord{
		ID:             ids.New("apr"),
		RunID:          runID,
		ConversationID: conversationID,
		Proposal:       storedProposal,
		Inference:      inference,
		Decision:       decision,
		InputSnapshot:  snapshot,
		SnapshotHash:   hash,
		Summary:        approvalSummary(proposal, inference),
		Status:         core.ApprovalPending,
		CreatedAt:      time.Now().UTC(),
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.approvals[record.ID] = record

	return cloneApproval(record), nil
}

func normalizedApprovalInput(proposal core.ToolProposal) map[string]any {
	snapshot := core.CloneMap(proposal.Input)
	if !strings.HasPrefix(proposal.Tool, "git.") {
		return snapshot
	}
	operation := strings.TrimPrefix(proposal.Tool, "git.")
	args := gitApprovalArgs(operation, snapshot)
	if len(args) > 0 {
		anyArgs := make([]any, 0, len(args))
		for _, arg := range args {
			anyArgs = append(anyArgs, arg)
		}
		snapshot["args"] = anyArgs
		snapshot["command"] = "git " + strings.Join(args, " ")
	}
	if _, ok := snapshot["workspace"]; !ok {
		snapshot["workspace"] = "."
	}
	return snapshot
}

func gitApprovalArgs(operation string, input map[string]any) []string {
	paths := approvalStringSlice(input["paths"])
	switch operation {
	case "status":
		return appendApprovalPathspec([]string{"status", "--short", "--branch"}, paths)
	case "diff":
		return appendApprovalPathspec([]string{"diff"}, paths)
	case "log":
		return []string{"log", "--oneline", "-n", "20"}
	case "branch":
		return []string{"branch", "--show-current"}
	case "add":
		return appendApprovalPathspec([]string{"add"}, paths)
	case "commit":
		message, _ := input["message"].(string)
		return []string{"commit", "-m", message}
	case "restore":
		return appendApprovalPathspec([]string{"restore"}, paths)
	case "clean":
		return appendApprovalPathspec([]string{"clean", "-fd"}, paths)
	default:
		return nil
	}
}

func appendApprovalPathspec(args []string, paths []string) []string {
	if len(paths) == 0 {
		return args
	}
	out := append([]string(nil), args...)
	out = append(out, "--")
	out = append(out, paths...)
	return out
}

func approvalStringSlice(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(string); ok {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}

// Get returns an approval record by ID.
func (a *ApprovalCenter) Get(id string) (*core.ApprovalRecord, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	record, ok := a.approvals[id]
	if !ok {
		return nil, errApprovalNotFound
	}
	return cloneApproval(record), nil
}

// Pending returns all unresolved approvals.
func (a *ApprovalCenter) Pending() ([]core.ApprovalRecord, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	items := make([]core.ApprovalRecord, 0, len(a.approvals))
	for _, record := range a.approvals {
		if record.Status == core.ApprovalPending {
			items = append(items, *cloneApproval(record))
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

// VerifySnapshotHash checks that the stored snapshot still matches its hash.
func (a *ApprovalCenter) VerifySnapshotHash(id string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	record, ok := a.approvals[id]
	if !ok {
		return errors.New("approval not found")
	}
	hash, err := core.HashMap(record.InputSnapshot)
	if err != nil {
		return err
	}
	if hash != record.SnapshotHash {
		return fmt.Errorf("approval %s input snapshot hash mismatch", id)
	}
	return nil
}

// Hydrate restores an approval record from durable workflow state.
func (a *ApprovalCenter) Hydrate(record core.ApprovalRecord) error {
	if record.ID == "" {
		return errors.New("approval id is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.approvals[record.ID] = *cloneApproval(record)
	return nil
}

func (a *ApprovalCenter) resolve(id string, approved bool, reason string) (*core.ApprovalRecord, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.approvals[id]
	if !ok {
		return nil, errApprovalNotFound
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

	return cloneApproval(record), nil
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
		if files, ok := proposal.Input["files"].([]any); ok && len(files) > 0 {
			return fmt.Sprintf("准备修改 %d 个工作区文件。", len(files))
		}
	case "code.run_tests":
		if command, ok := proposal.Input["command"].(string); ok && command != "" {
			return fmt.Sprintf("准备运行测试命令 `%s`，风险等级为 %s。", command, inference.RiskLevel)
		}
		return fmt.Sprintf("准备运行检测到的测试命令，风险等级为 %s。", inference.RiskLevel)
	case "git.add", "git.restore", "git.clean":
		if paths, ok := proposal.Input["paths"].([]any); ok && len(paths) > 0 {
			return fmt.Sprintf("准备执行 `%s`，影响 %d 个路径，风险等级为 %s。", proposal.Tool, len(paths), inference.RiskLevel)
		}
		return fmt.Sprintf("准备执行 `%s`，风险等级为 %s。", proposal.Tool, inference.RiskLevel)
	case "git.commit":
		if message, ok := proposal.Input["message"].(string); ok && message != "" {
			return fmt.Sprintf("准备创建本地 commit：`%s`。", message)
		}
		return fmt.Sprintf("准备创建本地 commit，风险等级为 %s。", inference.RiskLevel)
	case "memory.patch":
		if path, ok := proposal.Input["path"].(string); ok {
			return fmt.Sprintf("准备修改 Markdown memory `%s`。", path)
		}
	case "skill.run":
		if skillID, ok := proposal.Input["skill_id"].(string); ok {
			return fmt.Sprintf("准备执行 skill `%s`，风险等级为 %s。", skillID, inference.RiskLevel)
		}
	case "mcp.call_tool":
		serverID, _ := proposal.Input["server_id"].(string)
		toolName, _ := proposal.Input["tool_name"].(string)
		if serverID != "" && toolName != "" {
			return fmt.Sprintf("准备调用 MCP 工具 `%s/%s`，风险等级为 %s。", serverID, toolName, inference.RiskLevel)
		}
	default:
		if strings.HasPrefix(proposal.Tool, "ops.") {
			target := opsApprovalTarget(proposal.Input)
			if target != "" {
				return fmt.Sprintf("准备执行运维操作 `%s`，目标 `%s`，风险等级为 %s。", proposal.Tool, target, inference.RiskLevel)
			}
			return fmt.Sprintf("准备执行运维操作 `%s`，风险等级为 %s。", proposal.Tool, inference.RiskLevel)
		}
	}
	return fmt.Sprintf("准备执行 `%s`，风险等级为 %s。", proposal.Tool, inference.RiskLevel)
}

func opsApprovalTarget(input map[string]any) string {
	for _, key := range []string{"service", "service_name", "container", "container_id", "target", "name", "resource"} {
		if value, ok := input[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func cloneApproval(record core.ApprovalRecord) *core.ApprovalRecord {
	cp := record
	cp.Proposal.Input = core.CloneMap(record.Proposal.Input)
	cp.InputSnapshot = core.CloneMap(record.InputSnapshot)
	cp.Inference.Effects = append([]string(nil), record.Inference.Effects...)
	if record.Decision.ApprovalPayload != nil {
		cp.Decision.ApprovalPayload = core.CloneMap(record.Decision.ApprovalPayload)
	}
	return &cp
}
