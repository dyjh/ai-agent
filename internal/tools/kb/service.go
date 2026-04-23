package kb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/ids"
)

// Service manages knowledge base metadata and vector indexing.
type Service struct {
	index      VectorIndex
	embedder   Embedder
	collection string
	mu         sync.RWMutex
	bases      map[string]core.KnowledgeBase
	documents  map[string][]core.KBChunk
}

// NewService creates a KB service.
func NewService(index VectorIndex, embedder Embedder, collection string) *Service {
	return &Service{
		index:      index,
		embedder:   embedder,
		collection: collection,
		bases:      map[string]core.KnowledgeBase{},
		documents:  map[string][]core.KBChunk{},
	}
}

// CreateKB registers a new knowledge base.
func (s *Service) CreateKB(name, description string) core.KnowledgeBase {
	base := core.KnowledgeBase{
		ID:          ids.New("kb"),
		Name:        name,
		Description: description,
		CreatedAt:   time.Now().UTC(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bases[base.ID] = base
	return base
}

// ListKBs returns known KBs.
func (s *Service) ListKBs() []core.KnowledgeBase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]core.KnowledgeBase, 0, len(s.bases))
	for _, base := range s.bases {
		items = append(items, base)
	}
	return items
}

// UploadDocument chunks and indexes a document into the KB.
func (s *Service) UploadDocument(ctx context.Context, kbID, filename, content string) ([]core.KBChunk, error) {
	if s.collection == "" {
		return nil, fmt.Errorf("knowledge collection is not configured")
	}
	s.mu.RLock()
	_, ok := s.bases[kbID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("knowledge base not found: %s", kbID)
	}

	chunks := SplitMarkdownChunks(content)
	items := make([]core.KBChunk, 0, len(chunks))
	records := make([]VectorChunk, 0, len(chunks))
	sourceFile := buildKBSourceFile(kbID, filename)
	documentID := buildDocumentID(kbID, filename)
	if err := s.index.DeleteBySourceFile(ctx, s.collection, sourceFile); err != nil {
		return nil, err
	}
	for idx, chunk := range chunks {
		chunkID := ids.New("kbch")
		vector, err := s.embedder.Embed(ctx, chunk)
		if err != nil {
			return nil, err
		}
		payload := map[string]any{
			"chunk_id":     chunkID,
			"kb_id":        kbID,
			"document_id":  documentID,
			"source_file":  sourceFile,
			"filename":     filename,
			"chunk_index":  fmt.Sprintf("%d", idx),
			"content_hash": hashContent(chunk),
			"updated_at":   time.Now().UTC().Format(time.RFC3339),
			"text":         chunk,
		}
		record := VectorChunk{
			ID:         chunkID,
			Text:       chunk,
			Vector:     vector,
			Payload:    payload,
			SourceFile: sourceFile,
		}
		records = append(records, record)
		items = append(items, core.KBChunk{
			ID:       chunkID,
			KBID:     kbID,
			Document: filename,
			Content:  chunk,
			Metadata: stringifyPayload(payload),
		})
	}

	if err := s.index.UpsertChunks(ctx, s.collection, records); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.documents[kbID] = append(s.documents[kbID], items...)
	return items, nil
}

// Search runs a semantic search over KB chunks.
func (s *Service) Search(ctx context.Context, kbID, query string, limit int, filters map[string]any) ([]core.KBChunk, error) {
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if s.collection == "" {
		return nil, fmt.Errorf("knowledge collection is not configured")
	}
	if kbID != "" {
		s.mu.RLock()
		_, ok := s.bases[kbID]
		s.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("knowledge base not found: %s", kbID)
		}
	}

	filters = cloneAnyMap(filters)
	if kbID != "" {
		filters["kb_id"] = kbID
	}
	results, err := s.index.Search(ctx, s.collection, query, filters, limit)
	if err != nil {
		return nil, err
	}
	items := make([]core.KBChunk, 0, len(results))
	for _, result := range results {
		metadata := stringifyPayload(result.Payload)
		document := metadata["filename"]
		if document == "" {
			document = metadata["source_file"]
		}
		items = append(items, core.KBChunk{
			ID:       result.ID,
			KBID:     metadata["kb_id"],
			Document: document,
			Content:  result.Text,
			Metadata: metadata,
			Score:    float64(result.Score),
		})
	}
	return items, nil
}

// Health returns the effective vector backend state for KB operations.
func (s *Service) Health(ctx context.Context) VectorRuntimeStatus {
	status := StatusFromIndex(s.index)
	if status.Collections == nil {
		status.Collections = map[string]string{}
	}
	if err := s.index.Health(ctx); err != nil {
		status.Error = err.Error()
		if status.VectorBackend == "qdrant" {
			status.Qdrant = "error"
			for name := range status.Collections {
				status.Collections[name] = "error"
			}
		}
		return status
	}
	if status.VectorBackend == "qdrant" {
		status.Qdrant = "ok"
		for name := range status.Collections {
			status.Collections[name] = "ok"
		}
		return status
	}
	if status.FallbackReason != "" {
		status.Qdrant = "fallback"
	}
	for name := range status.Collections {
		status.Collections[name] = "ok"
	}
	return status
}

func buildKBSourceFile(kbID, filename string) string {
	return kbID + "/" + filename
}

func buildDocumentID(kbID, filename string) string {
	return kbID + ":" + filename
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func stringifyPayload(payload map[string]any) map[string]string {
	if len(payload) == 0 {
		return nil
	}
	out := make(map[string]string, len(payload))
	for key, value := range payload {
		out[key] = fmt.Sprint(value)
	}
	return out
}

func cloneAnyMap(input map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		out[key] = value
	}
	return out
}
