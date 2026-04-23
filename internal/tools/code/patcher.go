package code

import (
	"context"
	"os"

	"local-agent/internal/core"
	"local-agent/internal/tools"
)

// ProposePatchExecutor previews a patch without writing it.
type ProposePatchExecutor struct{}

// ApplyPatchExecutor applies a file replacement inside the workspace.
type ApplyPatchExecutor struct {
	Workspace Workspace
}

// Execute implements code.propose_patch.
func (e *ProposePatchExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	path, err := tools.GetString(input, "path")
	if err != nil {
		return nil, err
	}
	content, err := tools.GetString(input, "content")
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{
		Output: map[string]any{
			"path":    path,
			"preview": content,
		},
	}, nil
}

// Execute implements code.apply_patch.
func (e *ApplyPatchExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	path, err := tools.GetString(input, "path")
	if err != nil {
		return nil, err
	}
	content, err := tools.GetString(input, "content")
	if err != nil {
		return nil, err
	}
	abs, err := e.Workspace.resolve(path)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return nil, err
	}
	return &core.ToolResult{
		Output: map[string]any{
			"path":   path,
			"status": "applied",
		},
	}, nil
}
