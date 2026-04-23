package kb

import (
	"context"

	"local-agent/internal/core"
	"local-agent/internal/tools"
)

// SearchExecutor implements kb.search.
type SearchExecutor struct {
	Service *Service
}

// Execute searches KB chunks via the service and returns structured results.
func (e *SearchExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	query, err := tools.GetString(input, "query")
	if err != nil {
		return nil, err
	}
	kbID, _ := input["kb_id"].(string)
	limit := tools.GetInt(input, "limit", 5)
	results, err := e.Service.Search(ctx, kbID, query, limit, tools.GetMap(input, "filters"))
	if err != nil {
		return nil, err
	}

	items := make([]map[string]any, 0, len(results))
	for _, result := range results {
		items = append(items, map[string]any{
			"id":          result.ID,
			"kb_id":       result.KBID,
			"document":    result.Document,
			"source_file": result.Metadata["source_file"],
			"score":       result.Score,
			"snippet":     snippet(result.Content, 240),
			"payload":     result.Metadata,
		})
	}

	return &core.ToolResult{
		Output: map[string]any{
			"kb_id":   kbID,
			"query":   query,
			"results": items,
		},
	}, nil
}

func snippet(content string, maxChars int) string {
	if maxChars <= 0 || len(content) <= maxChars {
		return content
	}
	return content[:maxChars]
}
