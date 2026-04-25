package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/security"
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
	records := []kb.VectorChunk{}
	now := time.Now().UTC()
	for _, file := range files {
		doc := ParseMemoryDocument(file)
		for _, item := range doc.Items {
			if !itemActiveForContext(item, now) {
				continue
			}
			text := strings.TrimSpace(item.Text)
			if text == "" || ScanSensitiveMemory(text).Sensitive {
				continue
			}
			text = security.RedactString(text)
			vector, err := i.Embedder.Embed(ctx, text)
			if err != nil {
				return err
			}
			payload := map[string]any{
				"source_file": file.Path,
				"item_id":     item.ID,
				"text":        text,
				"memory_type": string(item.Type),
				"scope":       string(item.Scope),
				"status":      string(item.Status),
			}
			if item.ProjectKey != "" {
				payload["project_key"] = item.ProjectKey
			}
			if len(item.Tags) > 0 {
				payload["tags"] = strings.Join(item.Tags, ",")
			}
			records = append(records, kb.VectorChunk{
				ID:         fmt.Sprintf("%s#%s", file.Path, item.ID),
				Text:       text,
				Vector:     vector,
				Payload:    payload,
				SourceFile: file.Path,
			})
		}
	}
	return i.Index.UpsertChunks(ctx, i.Collection, records)
}
