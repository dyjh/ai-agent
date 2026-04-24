package kb

import (
	"context"
	"time"
)

// KnowledgeSourceType identifies the origin kind of indexed documents.
type KnowledgeSourceType string

const (
	KnowledgeSourceLocalFolder KnowledgeSourceType = "local_folder"
	KnowledgeSourceGitDocs     KnowledgeSourceType = "git_docs"
	KnowledgeSourceURL         KnowledgeSourceType = "url"
	KnowledgeSourceUpload      KnowledgeSourceType = "upload"
	KnowledgeSourcePDF         KnowledgeSourceType = "pdf"
	KnowledgeSourceOffice      KnowledgeSourceType = "office"
	KnowledgeSourceAPIDocs     KnowledgeSourceType = "api_docs"
)

// KnowledgeSource stores a registered source for one KB.
type KnowledgeSource struct {
	SourceID     string              `json:"source_id"`
	KBID         string              `json:"kb_id"`
	Type         KnowledgeSourceType `json:"type"`
	Name         string              `json:"name"`
	URI          string              `json:"uri,omitempty"`
	RootPath     string              `json:"root_path,omitempty"`
	IncludeGlobs []string            `json:"include_globs,omitempty"`
	ExcludeGlobs []string            `json:"exclude_globs,omitempty"`
	Metadata     map[string]any      `json:"metadata,omitempty"`
	Enabled      bool                `json:"enabled"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

// CreateSourceInput is the service/API payload for creating a source.
type CreateSourceInput struct {
	Type         KnowledgeSourceType `json:"type"`
	Name         string              `json:"name"`
	URI          string              `json:"uri,omitempty"`
	RootPath     string              `json:"root_path,omitempty"`
	IncludeGlobs []string            `json:"include_globs,omitempty"`
	ExcludeGlobs []string            `json:"exclude_globs,omitempty"`
	Metadata     map[string]any      `json:"metadata,omitempty"`
	Enabled      *bool               `json:"enabled,omitempty"`
}

// UpdateSourceInput is the service/API payload for updating a source.
type UpdateSourceInput struct {
	Name         *string             `json:"name,omitempty"`
	URI          *string             `json:"uri,omitempty"`
	RootPath     *string             `json:"root_path,omitempty"`
	IncludeGlobs []string            `json:"include_globs,omitempty"`
	ExcludeGlobs []string            `json:"exclude_globs,omitempty"`
	Metadata     map[string]any      `json:"metadata,omitempty"`
	Enabled      *bool               `json:"enabled,omitempty"`
	Type         KnowledgeSourceType `json:"type,omitempty"`
}

// IndexJobStatus tracks source synchronization lifecycle.
type IndexJobStatus string

const (
	IndexJobPending   IndexJobStatus = "pending"
	IndexJobRunning   IndexJobStatus = "running"
	IndexJobCompleted IndexJobStatus = "completed"
	IndexJobFailed    IndexJobStatus = "failed"
)

// KnowledgeIndexJob stores one source sync run.
type KnowledgeIndexJob struct {
	JobID        string         `json:"job_id"`
	KBID         string         `json:"kb_id"`
	SourceID     string         `json:"source_id"`
	Status       IndexJobStatus `json:"status"`
	TotalFiles   int            `json:"total_files"`
	IndexedFiles int            `json:"indexed_files"`
	DeletedFiles int            `json:"deleted_files"`
	SkippedFiles int            `json:"skipped_files,omitempty"`
	TotalChunks  int            `json:"total_chunks"`
	Error        string         `json:"error,omitempty"`
	StartedAt    time.Time      `json:"started_at,omitempty"`
	FinishedAt   time.Time      `json:"finished_at,omitempty"`
}

// KnowledgeDocument records source document metadata without storing full text.
type KnowledgeDocument struct {
	DocumentID  string         `json:"document_id"`
	KBID        string         `json:"kb_id"`
	SourceID    string         `json:"source_id"`
	SourceURI   string         `json:"source_uri"`
	SourceFile  string         `json:"source_file,omitempty"`
	Title       string         `json:"title,omitempty"`
	ContentHash string         `json:"content_hash"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// KnowledgeChunk is the citation-aware internal chunk payload.
type KnowledgeChunk struct {
	ChunkID     string         `json:"chunk_id"`
	KBID        string         `json:"kb_id"`
	SourceID    string         `json:"source_id"`
	DocumentID  string         `json:"document_id"`
	SourceURI   string         `json:"source_uri"`
	SourceFile  string         `json:"source_file,omitempty"`
	Title       string         `json:"title,omitempty"`
	Section     string         `json:"section,omitempty"`
	ChunkIndex  int            `json:"chunk_index"`
	Text        string         `json:"text"`
	ContentHash string         `json:"content_hash"`
	UpdatedAt   time.Time      `json:"updated_at"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Citation describes the evidence location for a retrieval or answer result.
type Citation struct {
	DocumentID string    `json:"document_id"`
	SourceID   string    `json:"source_id"`
	SourceURI  string    `json:"source_uri,omitempty"`
	SourceFile string    `json:"source_file,omitempty"`
	Title      string    `json:"title,omitempty"`
	Section    string    `json:"section,omitempty"`
	ChunkID    string    `json:"chunk_id"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	Score      float64   `json:"score,omitempty"`
}

// RetrievalMode controls search strategy.
type RetrievalMode string

const (
	RetrievalModeVector  RetrievalMode = "vector"
	RetrievalModeKeyword RetrievalMode = "keyword"
	RetrievalModeHybrid  RetrievalMode = "hybrid"
)

// RetrievalQuery is the input for citation-aware retrieval.
type RetrievalQuery struct {
	KBID    string         `json:"kb_id"`
	Query   string         `json:"query"`
	Mode    RetrievalMode  `json:"mode"`
	Filters map[string]any `json:"filters,omitempty"`
	TopK    int            `json:"top_k"`
	Rerank  bool           `json:"rerank"`
}

// RetrievalResult is one citation-aware retrieval hit.
type RetrievalResult struct {
	ChunkID      string         `json:"chunk_id"`
	Text         string         `json:"text"`
	Score        float64        `json:"score"`
	VectorScore  float64        `json:"vector_score,omitempty"`
	KeywordScore float64        `json:"keyword_score,omitempty"`
	RerankScore  float64        `json:"rerank_score,omitempty"`
	Citation     Citation       `json:"citation"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// AnswerMode controls KB answer evidence behavior.
type AnswerMode string

const (
	AnswerModeNormal             AnswerMode = "normal"
	AnswerModeKBOnly             AnswerMode = "kb_only"
	AnswerModeNoCitationNoAnswer AnswerMode = "no_citation_no_answer"
)

// AnswerInput is the input for kb.answer.
type AnswerInput struct {
	KBID             string         `json:"kb_id"`
	Query            string         `json:"query"`
	Mode             AnswerMode     `json:"mode"`
	TopK             int            `json:"top_k"`
	Filters          map[string]any `json:"filters,omitempty"`
	RequireCitations bool           `json:"require_citations"`
	Rerank           bool           `json:"rerank"`
}

// AnswerResult is the grounded answer payload.
type AnswerResult struct {
	Answer      string     `json:"answer"`
	Citations   []Citation `json:"citations"`
	HasEvidence bool       `json:"has_evidence"`
	Mode        AnswerMode `json:"mode"`
}

// RAGEvalCase stores one golden RAG check.
type RAGEvalCase struct {
	ID              string   `json:"id"`
	KBID            string   `json:"kb_id"`
	Question        string   `json:"question"`
	ExpectedSources []string `json:"expected_sources"`
	ExpectedHints   []string `json:"expected_hints"`
	MustRefuse      bool     `json:"must_refuse"`
}

// RAGEvalResult stores one evaluated case result.
type RAGEvalResult struct {
	CaseID           string   `json:"case_id"`
	RetrievedSources []string `json:"retrieved_sources"`
	RecallHit        bool     `json:"recall_hit"`
	CitationCorrect  bool     `json:"citation_correct"`
	Refused          bool     `json:"refused"`
	Summary          string   `json:"summary"`
}

// RAGEvalRun stores a complete eval report.
type RAGEvalRun struct {
	RunID      string          `json:"run_id"`
	Results    []RAGEvalResult `json:"results"`
	Total      int             `json:"total"`
	RecallHits int             `json:"recall_hits"`
	CreatedAt  time.Time       `json:"created_at"`
}

// ParseInput is the parser input for source documents.
type ParseInput struct {
	Source      KnowledgeSource `json:"source"`
	Filename    string          `json:"filename"`
	SourceURI   string          `json:"source_uri"`
	ContentType string          `json:"content_type,omitempty"`
	Content     []byte          `json:"-"`
}

// ParsedDocument is normalized text extracted from a source document.
type ParsedDocument struct {
	Title    string          `json:"title,omitempty"`
	Text     string          `json:"text"`
	Sections []ParsedSection `json:"sections,omitempty"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

// ParsedSection is one logical section inside a parsed document.
type ParsedSection struct {
	Heading string `json:"heading,omitempty"`
	Text    string `json:"text"`
	Offset  int    `json:"offset,omitempty"`
}

// DocumentParser extracts plain text from source bytes.
type DocumentParser interface {
	Supports(source KnowledgeSource, filename string, contentType string) bool
	Parse(ctx context.Context, input ParseInput) (*ParsedDocument, error)
}

// SearchInput is the input snapshot for kb.search.
type SearchInput struct {
	KBID    string         `json:"kb_id"`
	Query   string         `json:"query"`
	Limit   int            `json:"limit"`
	Filters map[string]any `json:"filters,omitempty"`
	Mode    RetrievalMode  `json:"mode,omitempty"`
	Rerank  bool           `json:"rerank,omitempty"`
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
