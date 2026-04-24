package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/security"
	"local-agent/internal/tools"
)

// CallToolExecutor executes approved mcp.call_tool snapshots through an MCPClient.
type CallToolExecutor struct {
	Client MCPClient
}

// Execute calls the configured MCP server tool.
func (e *CallToolExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	startedAt := time.Now().UTC()
	serverID, err := tools.GetString(input, "server_id")
	if err != nil {
		return failedMCPResult(startedAt, "", "", err), err
	}
	toolName, err := tools.GetString(input, "tool_name")
	if err != nil {
		return failedMCPResult(startedAt, serverID, "", err), err
	}
	args := tools.GetMap(input, "arguments")
	if args == nil {
		args = map[string]any{}
	}
	if e.Client == nil {
		err := fmt.Errorf("mcp client is not configured")
		return failedMCPResult(startedAt, serverID, toolName, err), err
	}

	result, err := e.Client.CallTool(ctx, serverID, toolName, args)
	finishedAt := time.Now().UTC()
	output := map[string]any{
		"server_id": serverID,
		"tool_name": toolName,
		"status":    "ok",
	}
	if result != nil {
		output["content"] = sanitizeAny(result.Content)
		output["structured"] = security.RedactMap(result.Structured)
		output["raw"] = security.RedactMap(result.Raw)
	}
	if err != nil {
		output["status"] = "error"
		output["error"] = security.RedactString(err.Error())
		var mcpErr *MCPError
		if errors.As(err, &mcpErr) {
			output["error_code"] = string(mcpErr.Code)
		}
	}
	toolResult := &core.ToolResult{
		Output:     output,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}
	if err != nil {
		toolResult.Error = err.Error()
	}
	return toolResult, err
}

func failedMCPResult(startedAt time.Time, serverID, toolName string, err error) *core.ToolResult {
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	finishedAt := time.Now().UTC()
	output := map[string]any{
		"server_id": serverID,
		"tool_name": toolName,
		"status":    "error",
		"error":     security.RedactString(err.Error()),
	}
	var mcpErr *MCPError
	if errors.As(err, &mcpErr) {
		output["error_code"] = string(mcpErr.Code)
	}
	return &core.ToolResult{
		Output:     output,
		Error:      err.Error(),
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}
}

func sanitizeAny(value any) any {
	switch item := value.(type) {
	case nil:
		return nil
	case string:
		return security.RedactString(item)
	case map[string]any:
		return security.RedactMap(item)
	default:
		raw, err := json.Marshal(item)
		if err != nil {
			return item
		}
		redacted := security.RedactString(string(raw))
		var decoded any
		if err := json.Unmarshal([]byte(redacted), &decoded); err != nil {
			return item
		}
		return decoded
	}
}
