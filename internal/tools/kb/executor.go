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
	mode := RetrievalMode("")
	if raw, _ := input["mode"].(string); raw != "" {
		mode = RetrievalMode(raw)
	}
	rerank, _ := input["rerank"].(bool)
	results, err := e.Service.Retrieve(ctx, RetrievalQuery{
		KBID:    kbID,
		Query:   query,
		Mode:    mode,
		Filters: tools.GetMap(input, "filters"),
		TopK:    limit,
		Rerank:  rerank,
	})
	if err != nil {
		return nil, err
	}

	items := make([]map[string]any, 0, len(results))
	for _, result := range results {
		items = append(items, map[string]any{
			"id":          result.ChunkID,
			"kb_id":       result.Metadata["kb_id"],
			"document":    result.Citation.Title,
			"source_file": result.Citation.SourceFile,
			"score":       result.Score,
			"snippet":     snippet(result.Text, 240),
			"payload":     stringifyPayload(result.Metadata),
			"citation":    result.Citation,
		})
	}

	return &core.ToolResult{
		Output: map[string]any{
			"kb_id":   kbID,
			"query":   query,
			"mode":    string(mode),
			"rerank":  rerank,
			"results": items,
		},
	}, nil
}

// RetrieveExecutor implements kb.retrieve.
type RetrieveExecutor struct {
	Service *Service
}

// Execute performs citation-aware retrieval.
func (e *RetrieveExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	query, err := tools.GetString(input, "query")
	if err != nil {
		return nil, err
	}
	kbID, _ := input["kb_id"].(string)
	mode := RetrievalMode("hybrid")
	if raw, _ := input["mode"].(string); raw != "" {
		mode = RetrievalMode(raw)
	}
	rerank, _ := input["rerank"].(bool)
	topK := tools.GetInt(input, "top_k", tools.GetInt(input, "limit", 5))
	results, err := e.Service.Retrieve(ctx, RetrievalQuery{
		KBID:    kbID,
		Query:   query,
		Mode:    mode,
		Filters: tools.GetMap(input, "filters"),
		TopK:    topK,
		Rerank:  rerank,
	})
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{Output: map[string]any{
		"kb_id":   kbID,
		"query":   query,
		"mode":    string(mode),
		"rerank":  rerank,
		"results": results,
	}}, nil
}

// AnswerExecutor implements kb.answer.
type AnswerExecutor struct {
	Service *Service
}

// Execute returns a grounded answer with citations.
func (e *AnswerExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	query, err := tools.GetString(input, "query")
	if err != nil {
		return nil, err
	}
	mode := AnswerMode("")
	if raw, _ := input["mode"].(string); raw != "" {
		mode = AnswerMode(raw)
	}
	requireCitations, _ := input["require_citations"].(bool)
	rerank, _ := input["rerank"].(bool)
	kbID, _ := input["kb_id"].(string)
	result, err := e.Service.Answer(ctx, AnswerInput{
		KBID:             kbID,
		Query:            query,
		Mode:             mode,
		TopK:             tools.GetInt(input, "top_k", tools.GetInt(input, "limit", 5)),
		Filters:          tools.GetMap(input, "filters"),
		RequireCitations: requireCitations,
		Rerank:           rerank,
	})
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{Output: map[string]any{
		"kb_id":  kbID,
		"query":  query,
		"answer": result,
	}}, nil
}

func snippet(content string, maxChars int) string {
	if maxChars <= 0 || len(content) <= maxChars {
		return content
	}
	return content[:maxChars]
}
