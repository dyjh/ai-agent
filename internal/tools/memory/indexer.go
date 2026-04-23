package memory

import (
	"context"
	"strings"

	"local-agent/internal/core"
	"local-agent/internal/tools/kb"
)

// Indexer syncs markdown memory into the vector index.
type Indexer struct {
	Collection string
	Index      kb.VectorIndex
	Embedder   kb.Embedder
}

// Reindex upserts memory files into the vector index.
func (i *Indexer) Reindex(ctx context.Context, files []core.MemoryFile) error {
	if i == nil || i.Index == nil || i.Embedder == nil {
		return nil
	}
	records := make([]kb.VectorChunk, 0, len(files))
	for _, file := range files {
		vector, err := i.Embedder.Embed(ctx, file.Body)
		if err != nil {
			return err
		}
		payload := map[string]any{
			"source_file": file.Path,
			"text":        strings.TrimSpace(file.Body),
		}
		if value := file.Frontmatter["kind"]; value != "" {
			payload["memory_type"] = value
		}
		if value := file.Frontmatter["scope"]; value != "" {
			payload["scope"] = value
		}
		if value := file.Frontmatter["project_key"]; value != "" {
			payload["project_key"] = value
		}
		records = append(records, kb.VectorChunk{
			ID:         file.Path,
			Text:       strings.TrimSpace(file.Body),
			Vector:     vector,
			Payload:    payload,
			SourceFile: file.Path,
		})
	}
	return i.Index.UpsertChunks(ctx, i.Collection, records)
}
