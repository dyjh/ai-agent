package memory

import (
	"context"
	"encoding/json"

	"local-agent/internal/core"
	"local-agent/internal/tools"
)

// ExtractCandidatesExecutor implements memory.extract_candidates.
type ExtractCandidatesExecutor struct {
	Store *Store
}

// DetectConflictsExecutor implements memory.detect_conflicts.
type DetectConflictsExecutor struct {
	Store *Store
}

// MergeCandidatesExecutor implements memory.merge_candidates.
type MergeCandidatesExecutor struct {
	Store *Store
}

// ItemCreateExecutor implements memory.item_create.
type ItemCreateExecutor struct {
	Store *Store
}

// ItemUpdateExecutor implements memory.item_update.
type ItemUpdateExecutor struct {
	Store *Store
}

// ItemArchiveExecutor implements memory.item_archive.
type ItemArchiveExecutor struct {
	Store *Store
}

// ItemRestoreExecutor implements memory.item_restore.
type ItemRestoreExecutor struct {
	Store *Store
}

// ItemDeleteExecutor implements memory.item_delete.
type ItemDeleteExecutor struct {
	Store *Store
}

func (e *ExtractCandidatesExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	var payload MemoryExtractInput
	if err := decodeMap(input, &payload); err != nil {
		return nil, err
	}
	candidates := ExtractCandidates(payload)
	output := map[string]any{
		"candidates": candidates,
	}
	queue, _ := input["queue"].(bool)
	if queue && e.Store != nil {
		reviews := make([]MemoryReviewItem, 0, len(candidates))
		for _, candidate := range candidates {
			review, err := e.Store.CreateReview(candidate)
			if err != nil {
				return nil, err
			}
			reviews = append(reviews, review)
		}
		output["review_items"] = reviews
	}
	return &core.ToolResult{Output: output}, nil
}

func (e *DetectConflictsExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	var candidate MemoryCandidate
	if raw := tools.GetMap(input, "candidate"); raw != nil {
		if err := decodeMap(raw, &candidate); err != nil {
			return nil, err
		}
	} else if err := decodeMap(input, &candidate); err != nil {
		return nil, err
	}
	conflicts, err := e.Store.DetectConflicts(candidate)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{Output: map[string]any{"conflicts": conflicts}}, nil
}

func (e *MergeCandidatesExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	var candidate MemoryCandidate
	if raw := tools.GetMap(input, "candidate"); raw != nil {
		if err := decodeMap(raw, &candidate); err != nil {
			return nil, err
		}
	} else if err := decodeMap(input, &candidate); err != nil {
		return nil, err
	}
	suggestion, err := e.Store.MergeCandidates(candidate)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{Output: suggestion}, nil
}

func (e *ItemCreateExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	var payload MemoryItemCreateInput
	if err := decodeMap(input, &payload); err != nil {
		return nil, err
	}
	item, err := e.Store.CreateItem(ctx, payload)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{Output: map[string]any{"item": item}}, nil
}

func (e *ItemUpdateExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	id, err := tools.GetString(input, "id")
	if err != nil {
		return nil, err
	}
	var payload MemoryItemUpdateInput
	if fields := tools.GetMap(input, "fields"); fields != nil {
		if err := decodeMap(fields, &payload); err != nil {
			return nil, err
		}
		if _, ok := fields["tags"]; ok {
			payload.TagsSet = true
		}
		if _, ok := fields["metadata"]; ok {
			payload.MetadataSet = true
		}
	} else {
		if err := decodeMap(input, &payload); err != nil {
			return nil, err
		}
		if _, ok := input["tags"]; ok {
			payload.TagsSet = true
		}
		if _, ok := input["metadata"]; ok {
			payload.MetadataSet = true
		}
	}
	item, err := e.Store.UpdateItem(ctx, id, payload)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{Output: map[string]any{"item": item}}, nil
}

func (e *ItemArchiveExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	id, err := tools.GetString(input, "id")
	if err != nil {
		return nil, err
	}
	item, err := e.Store.ArchiveItem(ctx, id)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{Output: map[string]any{"item": item}}, nil
}

func (e *ItemRestoreExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	id, err := tools.GetString(input, "id")
	if err != nil {
		return nil, err
	}
	item, err := e.Store.RestoreItem(ctx, id)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{Output: map[string]any{"item": item}}, nil
}

func (e *ItemDeleteExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	id, err := tools.GetString(input, "id")
	if err != nil {
		return nil, err
	}
	force, _ := input["force"].(bool)
	item, err := e.Store.DeleteItem(ctx, id, force)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{Output: map[string]any{"item": item, "force": force}}, nil
}

func decodeMap(input map[string]any, target any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}
