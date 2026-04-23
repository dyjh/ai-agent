package tests

import (
	"context"
	"testing"

	"local-agent/internal/core"
	toolscore "local-agent/internal/tools"
	"local-agent/internal/tools/mcp"
)

func TestMCPCallToolIsRegisteredExecutor(t *testing.T) {
	bootstrap := newSkillBootstrap(t)
	executor, err := bootstrap.Registry.Executor("mcp.call_tool")
	if err != nil {
		t.Fatalf("Executor() error = %v", err)
	}
	if _, ok := executor.(toolscore.NotImplementedExecutor); ok {
		t.Fatalf("mcp.call_tool should not use NotImplementedExecutor")
	}
}

func TestMCPMockTransportListAndCall(t *testing.T) {
	factory := newMockMCPFactory()
	manager := mcp.NewManager(mcp.WithTransportFactory(factory))
	createMCPServer(t, manager, "mock", true)
	factory.transport("mock").setTools([]mcp.MCPToolSchema{
		{
			Name:        "echo",
			Description: "echo input",
			Metadata: map[string]any{
				"effects":  []any{"fs.read"},
				"approval": "auto",
			},
		},
	})
	factory.transport("mock").setResult("echo", &mcp.MCPToolResult{
		Structured: map[string]any{"message": "ok"},
	})

	tools, err := manager.RefreshTools(context.Background(), "mock")
	if err != nil {
		t.Fatalf("RefreshTools() error = %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v, want echo", tools)
	}
	result, err := manager.CallTool(context.Background(), "mock", "echo", map[string]any{"message": "hi"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.Structured["message"] != "ok" {
		t.Fatalf("result = %+v, want message ok", result)
	}
	call, err := factory.transport("mock").lastCall()
	if err != nil {
		t.Fatalf("lastCall() error = %v", err)
	}
	if call.Name != "echo" || call.Input["message"] != "hi" {
		t.Fatalf("call = %+v", call)
	}
}

func TestMCPCallToolApprovalSnapshot(t *testing.T) {
	factory := newMockMCPFactory()
	manager := mcp.NewManager(mcp.WithTransportFactory(factory))
	createMCPServer(t, manager, "filesystem", true)
	if _, err := manager.UpdateToolPolicyInput("mcp.filesystem.write_file", mcp.ToolPolicyInput{
		Effects:  []string{"fs.write"},
		Approval: mcp.ApprovalRequire,
	}); err != nil {
		t.Fatalf("UpdateToolPolicyInput() error = %v", err)
	}
	factory.transport("filesystem").setResult("write_file", &mcp.MCPToolResult{
		Structured: map[string]any{"written": true},
	})

	router, approvals := newMCPRouterFixture(t, manager)
	proposal := core.ToolProposal{
		ID:   "tool_mcp_write",
		Tool: "mcp.call_tool",
		Input: map[string]any{
			"server_id": "filesystem",
			"tool_name": "write_file",
			"arguments": map[string]any{
				"path":    "alpha.txt",
				"content": "alpha",
			},
		},
	}
	outcome, err := router.Propose(context.Background(), "run_mcp_write", "conv_mcp_write", proposal)
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if outcome.Approval == nil || !outcome.Decision.RequiresApproval {
		t.Fatalf("write MCP tool should require approval")
	}
	if calls := factory.transport("filesystem").callCount(); calls != 0 {
		t.Fatalf("tool executed before approval, call count = %d", calls)
	}

	changed := proposal
	changed.Input = map[string]any{
		"server_id": "filesystem",
		"tool_name": "write_file",
		"arguments": map[string]any{
			"path":    "beta.txt",
			"content": "beta",
		},
	}
	matches, err := approvals.SnapshotMatches(outcome.Approval.ID, changed)
	if err != nil {
		t.Fatalf("SnapshotMatches() error = %v", err)
	}
	if matches {
		t.Fatalf("changed proposal should require a new approval")
	}

	if _, err := approvals.Approve(outcome.Approval.ID); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	result, err := router.ExecuteApproved(context.Background(), outcome.Approval.ID)
	if err != nil {
		t.Fatalf("ExecuteApproved() error = %v", err)
	}
	if result.Output["status"] != "ok" {
		t.Fatalf("status = %v, want ok", result.Output["status"])
	}
	call, err := factory.transport("filesystem").lastCall()
	if err != nil {
		t.Fatalf("lastCall() error = %v", err)
	}
	args, ok := call.Input["path"].(string)
	if !ok || args != "alpha.txt" {
		t.Fatalf("executed input = %+v, want approved alpha snapshot", call.Input)
	}
}
