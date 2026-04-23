package tests

import (
	"context"
	"testing"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
	"local-agent/internal/tools/mcp"
)

func TestMCPPolicyOverrideEffectInference(t *testing.T) {
	manager := mcp.NewManager()
	createMCPServer(t, manager, "filesystem", true)
	if _, err := manager.UpdateToolPolicyInput("mcp.filesystem.read_file", mcp.ToolPolicyInput{
		Effects:  []string{"fs.read"},
		Approval: mcp.ApprovalAuto,
	}); err != nil {
		t.Fatalf("UpdateToolPolicyInput(read) error = %v", err)
	}
	if _, err := manager.UpdateToolPolicyInput("mcp.filesystem.write_file", mcp.ToolPolicyInput{
		Effects:  []string{"fs.write"},
		Approval: mcp.ApprovalRequire,
	}); err != nil {
		t.Fatalf("UpdateToolPolicyInput(write) error = %v", err)
	}
	if _, err := manager.UpdateToolPolicyInput("mcp.filesystem.create_issue", mcp.ToolPolicyInput{
		Effects:  []string{"network.post"},
		Approval: mcp.ApprovalRequire,
	}); err != nil {
		t.Fatalf("UpdateToolPolicyInput(network) error = %v", err)
	}

	inferrer := agent.NewEffectInferrer(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}, manager)
	policy := agent.NewPolicyEngine(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85})

	testCases := []struct {
		name         string
		toolName     string
		wantEffect   string
		wantApproval bool
	}{
		{name: "read override auto", toolName: "read_file", wantEffect: "fs.read", wantApproval: false},
		{name: "write override requires approval", toolName: "write_file", wantEffect: "fs.write", wantApproval: true},
		{name: "network post requires approval", toolName: "create_issue", wantEffect: "network.post", wantApproval: true},
		{name: "unknown tool requires approval", toolName: "missing", wantEffect: "unknown.effect", wantApproval: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			proposal := core.ToolProposal{
				Tool: "mcp.call_tool",
				Input: map[string]any{
					"server_id": "filesystem",
					"tool_name": tc.toolName,
				},
			}
			inference, err := inferrer.Infer(context.Background(), proposal)
			if err != nil {
				t.Fatalf("Infer() error = %v", err)
			}
			if !containsString(inference.Effects, tc.wantEffect) {
				t.Fatalf("effects = %v, want %s", inference.Effects, tc.wantEffect)
			}
			decision, err := policy.Decide(context.Background(), proposal, inference)
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if decision.RequiresApproval != tc.wantApproval {
				t.Fatalf("RequiresApproval = %v, want %v", decision.RequiresApproval, tc.wantApproval)
			}
		})
	}
}

func TestReadOnlyMCPToolAutoExecutes(t *testing.T) {
	factory := newMockMCPFactory()
	manager := mcp.NewManager(mcp.WithTransportFactory(factory))
	createMCPServer(t, manager, "filesystem", true)
	if _, err := manager.UpdateToolPolicyInput("mcp.filesystem.read_file", mcp.ToolPolicyInput{
		Effects:  []string{"fs.read"},
		Approval: mcp.ApprovalAuto,
	}); err != nil {
		t.Fatalf("UpdateToolPolicyInput() error = %v", err)
	}
	factory.transport("filesystem").setResult("read_file", &mcp.MCPToolResult{
		Structured: map[string]any{"content": "hello"},
	})

	router, _ := newMCPRouterFixture(t, manager)
	outcome, err := router.Propose(context.Background(), "run_mcp_read", "conv_mcp_read", core.ToolProposal{
		ID:   "tool_mcp_read",
		Tool: "mcp.call_tool",
		Input: map[string]any{
			"server_id": "filesystem",
			"tool_name": "read_file",
			"arguments": map[string]any{
				"path": "README.md",
			},
		},
	})
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if outcome.Decision.RequiresApproval {
		t.Fatalf("read-only MCP tool should auto execute")
	}
	if outcome.Result == nil || outcome.Result.Output["status"] != "ok" {
		t.Fatalf("unexpected result: %+v", outcome.Result)
	}
	if calls := factory.transport("filesystem").callCount(); calls != 1 {
		t.Fatalf("call count = %d, want 1", calls)
	}
}
