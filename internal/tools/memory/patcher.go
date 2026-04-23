package memory

import (
	"context"

	"local-agent/internal/core"
	"local-agent/internal/tools"
)

// SearchExecutor implements memory.search.
type SearchExecutor struct {
	Store *Store
}

// PatchExecutor implements memory.patch.
type PatchExecutor struct {
	Store *Store
}

// Execute searches markdown memory files.
func (e *SearchExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	query, err := tools.GetString(input, "query")
	if err != nil {
		return nil, err
	}
	limit := tools.GetInt(input, "limit", 5)
	files, err := e.Store.Search(query, limit)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{
		Output: map[string]any{
			"query": query,
			"hits":  files,
		},
	}, nil
}

// Execute creates a pending memory patch proposal.
func (e *PatchExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	path, err := tools.GetString(input, "path")
	if err != nil {
		return nil, err
	}
	body, err := tools.GetString(input, "body")
	if err != nil {
		return nil, err
	}
	summary, _ := input["summary"].(string)
	frontmatter := map[string]string{}
	if raw, ok := input["frontmatter"].(map[string]string); ok {
		frontmatter = raw
	} else if raw, ok := input["frontmatter"].(map[string]any); ok {
		for key, value := range raw {
			if text, ok := value.(string); ok {
				frontmatter[key] = text
			}
		}
	}
	patch, err := e.Store.CreatePatch(core.MemoryPatch{
		Path:        path,
		Body:        body,
		Summary:     summary,
		Frontmatter: frontmatter,
	})
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{
		Output: map[string]any{
			"patch": patch,
		},
	}, nil
}
