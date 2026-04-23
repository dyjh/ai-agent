package kb

import "context"

// SearchInput is the input snapshot for kb.search.
type SearchInput struct {
	KBID    string         `json:"kb_id"`
	Query   string         `json:"query"`
	Limit   int            `json:"limit"`
	Filters map[string]any `json:"filters,omitempty"`
}

// UploadInput is the input snapshot for KB document ingestion.
type UploadInput struct {
	KBID     string `json:"kb_id"`
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// VectorChunk stores one indexed vector payload.
type VectorChunk struct {
	ID         string         `json:"id"`
	Text       string         `json:"text"`
	Vector     []float32      `json:"vector"`
	Payload    map[string]any `json:"payload,omitempty"`
	SourceFile string         `json:"source_file,omitempty"`
}

// VectorSearchResult stores a vector search hit.
type VectorSearchResult struct {
	ID      string         `json:"id"`
	Text    string         `json:"text"`
	Score   float32        `json:"score"`
	Payload map[string]any `json:"payload,omitempty"`
}

// VectorRuntimeStatus reports the effective backend and health state.
type VectorRuntimeStatus struct {
	VectorBackend  string            `json:"vector_backend"`
	FallbackReason string            `json:"fallback_reason,omitempty"`
	Qdrant         string            `json:"qdrant,omitempty"`
	Collections    map[string]string `json:"collections,omitempty"`
	Error          string            `json:"error,omitempty"`
}

// VectorIndex is the common vector index abstraction.
type VectorIndex interface {
	EnsureCollections(ctx context.Context) error
	UpsertChunks(ctx context.Context, collection string, chunks []VectorChunk) error
	DeleteBySourceFile(ctx context.Context, collection string, sourceFile string) error
	Search(ctx context.Context, collection string, query string, filters map[string]any, topK int) ([]VectorSearchResult, error)
	Health(ctx context.Context) error
}
