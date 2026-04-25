package kb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"local-agent/internal/config"
	"local-agent/internal/core"
	"local-agent/internal/ids"
	"local-agent/internal/security"
)

const (
	defaultTopK            = 5
	defaultChunkMaxChars   = 1200
	defaultURLMaxBytes     = 2 << 20
	defaultURLTimeout      = 10 * time.Second
	implicitUploadSourceID = "upload"
)

// Service manages knowledge base metadata, sources, indexing and retrieval.
type Service struct {
	index      VectorIndex
	embedder   Embedder
	collection string
	parsers    ParserRegistry
	httpClient *http.Client
	urlMax     int64
	network    config.NetworkPolicy

	mu             sync.RWMutex
	bases          map[string]core.KnowledgeBase
	sources        map[string]map[string]KnowledgeSource
	documents      map[string]map[string]KnowledgeDocument
	chunks         map[string]map[string]KnowledgeChunk
	jobs           map[string]map[string]KnowledgeIndexJob
	evalCases      map[string]RAGEvalCase
	evalRuns       map[string]RAGEvalRun
	documentChunks map[string]map[string][]string
}

// NewService creates a KB service.
func NewService(index VectorIndex, embedder Embedder, collection string) *Service {
	return &Service{
		index:          index,
		embedder:       embedder,
		collection:     collection,
		parsers:        NewDefaultParserRegistry(),
		httpClient:     &http.Client{Timeout: defaultURLTimeout},
		urlMax:         defaultURLMaxBytes,
		network:        config.Default().Policy.Network,
		bases:          map[string]core.KnowledgeBase{},
		sources:        map[string]map[string]KnowledgeSource{},
		documents:      map[string]map[string]KnowledgeDocument{},
		chunks:         map[string]map[string]KnowledgeChunk{},
		jobs:           map[string]map[string]KnowledgeIndexJob{},
		evalCases:      map[string]RAGEvalCase{},
		evalRuns:       map[string]RAGEvalRun{},
		documentChunks: map[string]map[string][]string{},
	}
}

// SetNetworkPolicy sets URL source policy constraints.
func (s *Service) SetNetworkPolicy(policy config.NetworkPolicy) {
	if policy.MaxDownloadBytes > 0 {
		s.urlMax = policy.MaxDownloadBytes
	}
	s.network = policy
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
	s.ensureKBMapsLocked(base.ID)
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
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items
}

// CreateSource registers a source under a KB.
func (s *Service) CreateSource(kbID string, input CreateSourceInput) (KnowledgeSource, error) {
	now := time.Now().UTC()
	source, err := s.buildSource(kbID, input, now)
	if err != nil {
		return KnowledgeSource{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.bases[kbID]; !ok {
		return KnowledgeSource{}, fmt.Errorf("knowledge base not found: %s", kbID)
	}
	s.ensureKBMapsLocked(kbID)
	s.sources[kbID][source.SourceID] = source
	return cloneSource(source), nil
}

// ListSources returns all registered sources for a KB.
func (s *Service) ListSources(kbID string) ([]KnowledgeSource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.bases[kbID]; !ok {
		return nil, fmt.Errorf("knowledge base not found: %s", kbID)
	}
	items := make([]KnowledgeSource, 0, len(s.sources[kbID]))
	for _, source := range s.sources[kbID] {
		items = append(items, cloneSource(source))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items, nil
}

// GetSource returns one source by id.
func (s *Service) GetSource(kbID, sourceID string) (KnowledgeSource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	source, ok := s.sources[kbID][sourceID]
	return cloneSource(source), ok
}

// UpdateSource updates mutable source fields.
func (s *Service) UpdateSource(kbID, sourceID string, input UpdateSourceInput) (KnowledgeSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	source, ok := s.sources[kbID][sourceID]
	if !ok {
		return KnowledgeSource{}, fmt.Errorf("knowledge source not found: %s", sourceID)
	}
	if input.Type != "" && input.Type != source.Type {
		return KnowledgeSource{}, fmt.Errorf("source type cannot be changed")
	}
	if input.Name != nil {
		source.Name = strings.TrimSpace(*input.Name)
	}
	if input.URI != nil {
		source.URI = sanitizeURI(*input.URI)
	}
	if input.RootPath != nil {
		source.RootPath = filepath.Clean(*input.RootPath)
	}
	if input.IncludeGlobs != nil {
		source.IncludeGlobs = append([]string(nil), input.IncludeGlobs...)
	}
	if input.ExcludeGlobs != nil {
		source.ExcludeGlobs = append([]string(nil), input.ExcludeGlobs...)
	}
	if input.Metadata != nil {
		source.Metadata = sanitizeMetadata(input.Metadata)
	}
	if input.Enabled != nil {
		source.Enabled = *input.Enabled
	}
	source.UpdatedAt = time.Now().UTC()
	if err := validateSource(source); err != nil {
		return KnowledgeSource{}, err
	}
	if source.Type == KnowledgeSourceURL {
		decision := security.ValidateNetworkURL(s.network, source.URI, http.MethodGet, s.urlMax)
		if !decision.Allowed {
			return KnowledgeSource{}, fmt.Errorf("network policy denied URL source: %s", decision.Reason)
		}
	}
	s.sources[kbID][sourceID] = source
	return cloneSource(source), nil
}

// DeleteSource removes a source and its indexed chunks.
func (s *Service) DeleteSource(ctx context.Context, kbID, sourceID string) error {
	s.mu.RLock()
	docs := make([]KnowledgeDocument, 0)
	for _, doc := range s.documents[kbID] {
		if doc.SourceID == sourceID {
			docs = append(docs, doc)
		}
	}
	s.mu.RUnlock()

	for _, doc := range docs {
		if err := s.index.DeleteBySourceFile(ctx, s.collection, doc.SourceFile); err != nil {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sources[kbID][sourceID]; !ok {
		return fmt.Errorf("knowledge source not found: %s", sourceID)
	}
	delete(s.sources[kbID], sourceID)
	for _, doc := range docs {
		s.removeDocumentLocked(kbID, doc.DocumentID)
	}
	return nil
}

// UploadDocument chunks and indexes an uploaded document into the KB.
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

	source := KnowledgeSource{
		SourceID:  implicitUploadSourceID,
		KBID:      kbID,
		Type:      KnowledgeSourceUpload,
		Name:      "Uploads",
		Enabled:   true,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	parsed, err := s.parsers.Parse(ctx, ParseInput{
		Source:    source,
		Filename:  filename,
		SourceURI: filename,
		Content:   []byte(content),
	})
	if err != nil {
		parsed = &ParsedDocument{Title: filename, Text: strings.TrimSpace(content), Sections: []ParsedSection{{Text: strings.TrimSpace(content)}}}
	}
	doc, chunks, records, err := s.prepareDocument(ctx, source, filename, filename, parsed, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := s.index.DeleteBySourceFile(ctx, s.collection, doc.SourceFile); err != nil {
		return nil, err
	}
	if err := s.index.UpsertChunks(ctx, s.collection, records); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.ensureKBMapsLocked(kbID)
	s.documents[kbID][doc.DocumentID] = doc
	s.replaceChunksLocked(kbID, doc.DocumentID, chunks)
	s.mu.Unlock()

	items := make([]core.KBChunk, 0, len(chunks))
	for _, chunk := range chunks {
		items = append(items, chunk.toCore())
	}
	return items, nil
}

// SyncSource performs a synchronous incremental index job for a source.
func (s *Service) SyncSource(ctx context.Context, kbID, sourceID string) (KnowledgeIndexJob, error) {
	if s.collection == "" {
		return KnowledgeIndexJob{}, fmt.Errorf("knowledge collection is not configured")
	}
	now := time.Now().UTC()
	job := KnowledgeIndexJob{
		JobID:     ids.New("kbjob"),
		KBID:      kbID,
		SourceID:  sourceID,
		Status:    IndexJobRunning,
		StartedAt: now,
	}
	source, existing, err := s.startJob(job)
	if err != nil {
		return KnowledgeIndexJob{}, err
	}
	defer func() {
		s.mu.Lock()
		current := s.jobs[kbID][job.JobID]
		if current.Status == IndexJobRunning {
			current.Status = IndexJobFailed
			current.FinishedAt = time.Now().UTC()
			current.Error = "job exited before completion"
			s.jobs[kbID][job.JobID] = current
		}
		s.mu.Unlock()
	}()

	candidates, err := s.collectSourceDocuments(ctx, source)
	if err != nil {
		return s.finishJob(kbID, job.JobID, func(j *KnowledgeIndexJob) {
			j.Status = IndexJobFailed
			j.Error = err.Error()
		}), err
	}

	seen := map[string]bool{}
	for _, candidate := range candidates {
		job.TotalFiles++
		docID := stableDocumentID(kbID, sourceID, candidate.sourceURI)
		seen[docID] = true
		parsed, err := s.parsers.Parse(ctx, ParseInput{
			Source:      source,
			Filename:    candidate.filename,
			SourceURI:   candidate.sourceURI,
			ContentType: candidate.contentType,
			Content:     candidate.content,
		})
		if err != nil {
			return s.finishJob(kbID, job.JobID, func(j *KnowledgeIndexJob) {
				j.Status = IndexJobFailed
				j.Error = err.Error()
				j.TotalFiles = job.TotalFiles
				j.IndexedFiles = job.IndexedFiles
				j.SkippedFiles = job.SkippedFiles
				j.DeletedFiles = job.DeletedFiles
				j.TotalChunks = job.TotalChunks
			}), err
		}
		contentHash := hashContent(parsed.Text)
		if old, ok := existing[docID]; ok && old.ContentHash == contentHash {
			job.SkippedFiles++
			continue
		}
		doc, chunks, records, err := s.prepareDocument(ctx, source, candidate.filename, candidate.sourceURI, parsed, time.Now().UTC())
		if err != nil {
			return s.finishJob(kbID, job.JobID, func(j *KnowledgeIndexJob) {
				j.Status = IndexJobFailed
				j.Error = err.Error()
			}), err
		}
		if err := s.index.DeleteBySourceFile(ctx, s.collection, doc.SourceFile); err != nil {
			return s.finishJob(kbID, job.JobID, func(j *KnowledgeIndexJob) {
				j.Status = IndexJobFailed
				j.Error = err.Error()
			}), err
		}
		if err := s.index.UpsertChunks(ctx, s.collection, records); err != nil {
			return s.finishJob(kbID, job.JobID, func(j *KnowledgeIndexJob) {
				j.Status = IndexJobFailed
				j.Error = err.Error()
			}), err
		}
		s.mu.Lock()
		s.documents[kbID][doc.DocumentID] = doc
		s.replaceChunksLocked(kbID, doc.DocumentID, chunks)
		s.mu.Unlock()
		job.IndexedFiles++
		job.TotalChunks += len(chunks)
	}

	for docID, old := range existing {
		if seen[docID] {
			continue
		}
		if err := s.index.DeleteBySourceFile(ctx, s.collection, old.SourceFile); err != nil {
			return s.finishJob(kbID, job.JobID, func(j *KnowledgeIndexJob) {
				j.Status = IndexJobFailed
				j.Error = err.Error()
			}), err
		}
		s.mu.Lock()
		s.removeDocumentLocked(kbID, docID)
		s.mu.Unlock()
		job.DeletedFiles++
	}

	return s.finishJob(kbID, job.JobID, func(j *KnowledgeIndexJob) {
		j.Status = IndexJobCompleted
		j.TotalFiles = job.TotalFiles
		j.IndexedFiles = job.IndexedFiles
		j.SkippedFiles = job.SkippedFiles
		j.DeletedFiles = job.DeletedFiles
		j.TotalChunks = job.TotalChunks
	}), nil
}

// ListIndexJobs returns index jobs for a KB.
func (s *Service) ListIndexJobs(kbID string) ([]KnowledgeIndexJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.bases[kbID]; !ok {
		return nil, fmt.Errorf("knowledge base not found: %s", kbID)
	}
	items := make([]KnowledgeIndexJob, 0, len(s.jobs[kbID]))
	for _, job := range s.jobs[kbID] {
		items = append(items, job)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt.After(items[j].StartedAt) })
	return items, nil
}

// GetIndexJob returns one index job.
func (s *Service) GetIndexJob(kbID, jobID string) (KnowledgeIndexJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[kbID][jobID]
	return job, ok
}

// Search runs a semantic search over KB chunks and preserves the legacy shape.
func (s *Service) Search(ctx context.Context, kbID, query string, limit int, filters map[string]any) ([]core.KBChunk, error) {
	results, err := s.Retrieve(ctx, RetrievalQuery{
		KBID:    kbID,
		Query:   query,
		Mode:    RetrievalModeVector,
		Filters: filters,
		TopK:    limit,
	})
	if err != nil {
		return nil, err
	}
	items := make([]core.KBChunk, 0, len(results))
	for _, result := range results {
		items = append(items, retrievalToCore(result, kbID))
	}
	return items, nil
}

// Retrieve performs vector, keyword or hybrid retrieval with citation metadata.
func (s *Service) Retrieve(ctx context.Context, query RetrievalQuery) ([]RetrievalResult, error) {
	if strings.TrimSpace(query.Query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if s.collection == "" {
		return nil, fmt.Errorf("knowledge collection is not configured")
	}
	if query.TopK <= 0 {
		query.TopK = defaultTopK
	}
	if query.Mode == "" {
		query.Mode = RetrievalModeHybrid
	}
	if query.KBID != "" {
		s.mu.RLock()
		_, ok := s.bases[query.KBID]
		s.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("knowledge base not found: %s", query.KBID)
		}
	}

	filters := cloneAnyMap(query.Filters)
	if query.KBID != "" {
		filters["kb_id"] = query.KBID
	}

	combined := map[string]RetrievalResult{}
	if query.Mode == RetrievalModeVector || query.Mode == RetrievalModeHybrid {
		vectorResults, err := s.index.Search(ctx, s.collection, query.Query, filters, query.TopK*2)
		if err != nil {
			return nil, err
		}
		for _, item := range vectorResults {
			result := retrievalFromVector(item)
			result.VectorScore = float64(item.Score)
			result.Score = result.VectorScore
			combined[result.ChunkID] = result
		}
	}
	if query.Mode == RetrievalModeKeyword || query.Mode == RetrievalModeHybrid {
		keywordResults := s.keywordSearch(query.Query, filters, query.TopK*2)
		maxKeyword := 0.0
		for _, item := range keywordResults {
			if item.KeywordScore > maxKeyword {
				maxKeyword = item.KeywordScore
			}
		}
		for _, item := range keywordResults {
			normalizedKeyword := item.KeywordScore
			if maxKeyword > 0 {
				normalizedKeyword = normalizedKeyword / maxKeyword
			}
			if existing, ok := combined[item.ChunkID]; ok {
				existing.KeywordScore = item.KeywordScore
				existing.Score = 0.6*existing.VectorScore + 0.4*normalizedKeyword
				combined[item.ChunkID] = existing
				continue
			}
			item.Score = normalizedKeyword
			combined[item.ChunkID] = item
		}
	}

	results := make([]RetrievalResult, 0, len(combined))
	for _, item := range combined {
		results = append(results, item)
	}
	if query.Rerank {
		applyLocalRerank(query.Query, results)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ChunkID < results[j].ChunkID
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > query.TopK {
		results = results[:query.TopK]
	}
	return results, nil
}

// Answer returns a grounded, citation-only local answer.
func (s *Service) Answer(ctx context.Context, input AnswerInput) (AnswerResult, error) {
	mode := input.Mode
	if mode == "" {
		mode = AnswerModeNormal
	}
	topK := input.TopK
	if topK <= 0 {
		topK = defaultTopK
	}
	results, err := s.Retrieve(ctx, RetrievalQuery{
		KBID:    input.KBID,
		Query:   input.Query,
		Mode:    RetrievalModeHybrid,
		Filters: input.Filters,
		TopK:    topK,
		Rerank:  input.Rerank,
	})
	if err != nil {
		return AnswerResult{}, err
	}
	if input.RequireCitations || mode == AnswerModeKBOnly || mode == AnswerModeNoCitationNoAnswer {
		results = strongEvidenceResults(input.Query, results)
	}
	citations := make([]Citation, 0, len(results))
	for _, result := range results {
		citation := result.Citation
		citation.Score = result.Score
		citations = append(citations, citation)
	}
	hasEvidence := len(citations) > 0
	requireCitation := input.RequireCitations || mode == AnswerModeKBOnly || mode == AnswerModeNoCitationNoAnswer
	if !hasEvidence && requireCitation {
		return AnswerResult{
			Answer:      "没有找到足够的知识库证据，无法回答。",
			Citations:   nil,
			HasEvidence: false,
			Mode:        mode,
		}, nil
	}
	if !hasEvidence {
		return AnswerResult{
			Answer:      "知识库中没有找到相关证据。",
			HasEvidence: false,
			Mode:        mode,
		}, nil
	}
	parts := make([]string, 0, len(results))
	for idx, result := range results {
		parts = append(parts, fmt.Sprintf("[%d] %s", idx+1, safeEvidenceSnippet(result.Text, 280)))
	}
	return AnswerResult{
		Answer:      "根据知识库证据：\n" + strings.Join(parts, "\n"),
		Citations:   citations,
		HasEvidence: true,
		Mode:        mode,
	}, nil
}

func strongEvidenceResults(query string, results []RetrievalResult) []RetrievalResult {
	queryTokens := tokenize(query)
	requiredOverlap := 1
	if len(queryTokens) > 1 {
		requiredOverlap = 2
	}
	out := make([]RetrievalResult, 0, len(results))
	querySet := map[string]bool{}
	for _, token := range queryTokens {
		querySet[token] = true
	}
	for _, result := range results {
		overlap := 0
		seen := map[string]bool{}
		for _, token := range tokenize(result.Text) {
			if querySet[token] && !seen[token] {
				overlap++
				seen[token] = true
			}
		}
		if overlap >= requiredOverlap {
			out = append(out, result)
		}
	}
	return out
}

func safeEvidenceSnippet(text string, maxChars int) string {
	sentences := strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == '\n' || r == '。' || r == '!' || r == '！'
	})
	cleaned := make([]string, 0, len(sentences))
	for _, sentence := range sentences {
		trimmed := strings.TrimSpace(sentence)
		lower := strings.ToLower(trimmed)
		if trimmed == "" || strings.Contains(lower, "ignore system") || strings.Contains(lower, "system rules") || strings.Contains(lower, "follow document instructions") {
			continue
		}
		cleaned = append(cleaned, trimmed)
	}
	value := strings.Join(cleaned, ". ")
	if value == "" {
		value = text
	}
	return snippet(value, maxChars)
}

// CreateRAGEval creates or replaces an eval case.
func (s *Service) CreateRAGEval(input RAGEvalCase) (RAGEvalCase, error) {
	if strings.TrimSpace(input.Question) == "" {
		return RAGEvalCase{}, fmt.Errorf("question is required")
	}
	if input.ID == "" {
		input.ID = ids.New("rageval")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evalCases[input.ID] = input
	return input, nil
}

// ListRAGEvals returns all eval cases.
func (s *Service) ListRAGEvals() []RAGEvalCase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]RAGEvalCase, 0, len(s.evalCases))
	for _, item := range s.evalCases {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

// RunRAGEval runs selected eval cases.
func (s *Service) RunRAGEval(ctx context.Context, caseIDs []string) (RAGEvalRun, error) {
	s.mu.RLock()
	cases := make([]RAGEvalCase, 0, len(s.evalCases))
	selected := map[string]bool{}
	for _, id := range caseIDs {
		selected[id] = true
	}
	for _, item := range s.evalCases {
		if len(selected) == 0 || selected[item.ID] {
			cases = append(cases, item)
		}
	}
	s.mu.RUnlock()
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })

	run := RAGEvalRun{RunID: ids.New("ragevalrun"), CreatedAt: time.Now().UTC()}
	for _, item := range cases {
		answer, err := s.Answer(ctx, AnswerInput{
			KBID:             item.KBID,
			Query:            item.Question,
			Mode:             AnswerModeNoCitationNoAnswer,
			TopK:             defaultTopK,
			RequireCitations: true,
			Rerank:           true,
		})
		if err != nil {
			return RAGEvalRun{}, err
		}
		retrieved := citationSources(answer.Citations)
		refused := !answer.HasEvidence
		recallHit := expectedSourceHit(item.ExpectedSources, retrieved)
		if len(item.ExpectedSources) == 0 && item.MustRefuse {
			recallHit = refused
		}
		result := RAGEvalResult{
			CaseID:           item.ID,
			RetrievedSources: retrieved,
			RecallHit:        recallHit,
			CitationCorrect:  item.MustRefuse && refused || (!item.MustRefuse && recallHit && len(answer.Citations) > 0),
			Refused:          refused,
			Summary:          evalSummary(item, recallHit, refused),
		}
		if result.RecallHit {
			run.RecallHits++
		}
		run.Results = append(run.Results, result)
	}
	run.Total = len(run.Results)
	s.mu.Lock()
	s.evalRuns[run.RunID] = run
	s.mu.Unlock()
	return run, nil
}

// GetRAGEvalRun returns an eval run report.
func (s *Service) GetRAGEvalRun(runID string) (RAGEvalRun, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.evalRuns[runID]
	return run, ok
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

type sourceCandidate struct {
	filename    string
	sourceURI   string
	contentType string
	content     []byte
}

func (s *Service) buildSource(kbID string, input CreateSourceInput, now time.Time) (KnowledgeSource, error) {
	if input.Type == "" {
		return KnowledgeSource{}, fmt.Errorf("source type is required")
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	source := KnowledgeSource{
		SourceID:     ids.New("kbsrc"),
		KBID:         kbID,
		Type:         input.Type,
		Name:         strings.TrimSpace(input.Name),
		URI:          sanitizeURI(input.URI),
		RootPath:     filepath.Clean(input.RootPath),
		IncludeGlobs: append([]string(nil), input.IncludeGlobs...),
		ExcludeGlobs: append([]string(nil), input.ExcludeGlobs...),
		Metadata:     sanitizeMetadata(input.Metadata),
		Enabled:      enabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if source.RootPath == "." && strings.TrimSpace(input.RootPath) == "" {
		source.RootPath = ""
	}
	if source.Name == "" {
		source.Name = string(source.Type)
	}
	if err := validateSource(source); err != nil {
		return KnowledgeSource{}, err
	}
	if source.Type == KnowledgeSourceURL {
		decision := security.ValidateNetworkURL(s.network, source.URI, http.MethodGet, s.urlMax)
		if !decision.Allowed {
			return KnowledgeSource{}, fmt.Errorf("network policy denied URL source: %s", decision.Reason)
		}
	}
	return source, nil
}

func validateSource(source KnowledgeSource) error {
	switch source.Type {
	case KnowledgeSourceLocalFolder, KnowledgeSourceGitDocs, KnowledgeSourceAPIDocs:
		if source.RootPath == "" {
			return fmt.Errorf("root_path is required for %s source", source.Type)
		}
		info, err := os.Stat(source.RootPath)
		if err != nil {
			return fmt.Errorf("stat root_path: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("root_path must be a directory")
		}
	case KnowledgeSourceURL:
		parsed, err := url.Parse(source.URI)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("valid URL is required")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("only http and https URL sources are supported")
		}
	case KnowledgeSourceUpload, KnowledgeSourcePDF, KnowledgeSourceOffice:
	default:
		return fmt.Errorf("unsupported source type: %s", source.Type)
	}
	return nil
}

func (s *Service) startJob(job KnowledgeIndexJob) (KnowledgeSource, map[string]KnowledgeDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.bases[job.KBID]; !ok {
		return KnowledgeSource{}, nil, fmt.Errorf("knowledge base not found: %s", job.KBID)
	}
	source, ok := s.sources[job.KBID][job.SourceID]
	if !ok {
		return KnowledgeSource{}, nil, fmt.Errorf("knowledge source not found: %s", job.SourceID)
	}
	if !source.Enabled {
		return KnowledgeSource{}, nil, fmt.Errorf("knowledge source is disabled: %s", job.SourceID)
	}
	s.ensureKBMapsLocked(job.KBID)
	s.jobs[job.KBID][job.JobID] = job
	existing := map[string]KnowledgeDocument{}
	for id, doc := range s.documents[job.KBID] {
		if doc.SourceID == job.SourceID {
			existing[id] = doc
		}
	}
	return cloneSource(source), existing, nil
}

func (s *Service) finishJob(kbID, jobID string, mutate func(*KnowledgeIndexJob)) KnowledgeIndexJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[kbID][jobID]
	mutate(&job)
	job.FinishedAt = time.Now().UTC()
	s.jobs[kbID][jobID] = job
	return job
}

func (s *Service) collectSourceDocuments(ctx context.Context, source KnowledgeSource) ([]sourceCandidate, error) {
	switch source.Type {
	case KnowledgeSourceLocalFolder, KnowledgeSourceGitDocs, KnowledgeSourceAPIDocs:
		return s.collectLocalFolder(source)
	case KnowledgeSourceURL:
		return s.collectURL(ctx, source)
	default:
		return nil, fmt.Errorf("source sync is not supported for %s", source.Type)
	}
}

func (s *Service) collectLocalFolder(source KnowledgeSource) ([]sourceCandidate, error) {
	root, err := filepath.Abs(source.RootPath)
	if err != nil {
		return nil, err
	}
	candidates := []sourceCandidate{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		if entry.IsDir() {
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !sourcePathIncluded(rel, source.IncludeGlobs, source.ExcludeGlobs) {
			return nil
		}
		if _, err := s.parsers.Parse(context.Background(), ParseInput{Source: source, Filename: rel, Content: []byte("probe")}); err != nil && strings.Contains(err.Error(), "no parser supports") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		candidates = append(candidates, sourceCandidate{
			filename:  rel,
			sourceURI: filepath.ToSlash(rel),
			content:   content,
		})
		return nil
	})
	return candidates, err
}

func (s *Service) collectURL(ctx context.Context, source KnowledgeSource) ([]sourceCandidate, error) {
	decision := security.ValidateNetworkURL(s.network, source.URI, http.MethodGet, s.urlMax)
	if !decision.Allowed {
		return nil, fmt.Errorf("network policy denied URL source: %s", decision.Reason)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URI, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("url source returned status %d", resp.StatusCode)
	}
	reader := io.LimitReader(resp.Body, s.urlMax+1)
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > s.urlMax {
		return nil, fmt.Errorf("url source exceeds max response size")
	}
	filename := filepath.Base(req.URL.Path)
	if filename == "." || filename == "/" || filename == "" {
		filename = "index.html"
	}
	return []sourceCandidate{{
		filename:    filename,
		sourceURI:   sanitizeURI(source.URI),
		contentType: resp.Header.Get("Content-Type"),
		content:     content,
	}}, nil
}

func (s *Service) prepareDocument(ctx context.Context, source KnowledgeSource, filename, sourceURI string, parsed *ParsedDocument, now time.Time) (KnowledgeDocument, []KnowledgeChunk, []VectorChunk, error) {
	if parsed == nil || strings.TrimSpace(parsed.Text) == "" {
		return KnowledgeDocument{}, nil, nil, fmt.Errorf("parsed document is empty: %s", filename)
	}
	if scan := security.ScanText(parsed.Text); security.MustBlockLongTermStorage(scan) {
		return KnowledgeDocument{}, nil, nil, fmt.Errorf("refusing to index secret-bearing knowledge document: %s", filename)
	}
	doc := KnowledgeDocument{
		DocumentID:  stableDocumentID(source.KBID, source.SourceID, sourceURI),
		KBID:        source.KBID,
		SourceID:    source.SourceID,
		SourceURI:   sanitizeURI(sourceURI),
		SourceFile:  buildSourceFile(source.KBID, source.SourceID, sourceURI),
		Title:       firstNonEmpty(parsed.Title, filepath.Base(filename)),
		ContentHash: hashContent(parsed.Text),
		Metadata:    sanitizeMetadata(parsed.Metadata),
		UpdatedAt:   now,
	}
	chunks := buildKnowledgeChunks(source, doc, parsed, now)
	records := make([]VectorChunk, 0, len(chunks))
	for _, chunk := range chunks {
		vector, err := s.embedder.Embed(ctx, chunk.Text)
		if err != nil {
			return KnowledgeDocument{}, nil, nil, err
		}
		records = append(records, VectorChunk{
			ID:         chunk.ChunkID,
			Text:       chunk.Text,
			Vector:     vector,
			Payload:    chunkPayload(chunk),
			SourceFile: chunk.SourceFile,
		})
	}
	return doc, chunks, records, nil
}

func buildKnowledgeChunks(source KnowledgeSource, doc KnowledgeDocument, parsed *ParsedDocument, now time.Time) []KnowledgeChunk {
	sections := parsed.Sections
	if len(sections) == 0 {
		sections = []ParsedSection{{Heading: doc.Title, Text: parsed.Text}}
	}
	chunks := []KnowledgeChunk{}
	for _, section := range sections {
		textParts := splitTextChunks(section.Text, defaultChunkMaxChars)
		for _, text := range textParts {
			idx := len(chunks)
			chunkHash := hashContent(text)
			chunkID := stableChunkID(doc.DocumentID, idx, chunkHash)
			chunks = append(chunks, KnowledgeChunk{
				ChunkID:     chunkID,
				KBID:        source.KBID,
				SourceID:    source.SourceID,
				DocumentID:  doc.DocumentID,
				SourceURI:   doc.SourceURI,
				SourceFile:  doc.SourceFile,
				Title:       doc.Title,
				Section:     section.Heading,
				ChunkIndex:  idx,
				Text:        text,
				ContentHash: chunkHash,
				UpdatedAt:   now,
				Metadata:    sanitizeMetadata(parsed.Metadata),
			})
		}
	}
	return chunks
}

func splitTextChunks(text string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = defaultChunkMaxChars
	}
	paragraphs := SplitMarkdownChunks(text)
	if len(paragraphs) == 0 && strings.TrimSpace(text) != "" {
		paragraphs = []string{strings.TrimSpace(text)}
	}
	chunks := []string{}
	var current strings.Builder
	flush := func() {
		value := strings.TrimSpace(current.String())
		if value != "" {
			chunks = append(chunks, value)
		}
		current.Reset()
	}
	for _, paragraph := range paragraphs {
		if len(paragraph) > maxChars {
			flush()
			for len(paragraph) > maxChars {
				chunks = append(chunks, strings.TrimSpace(paragraph[:maxChars]))
				paragraph = paragraph[maxChars:]
			}
		}
		if current.Len()+len(paragraph)+2 > maxChars {
			flush()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(paragraph)
	}
	flush()
	return chunks
}

func (s *Service) keywordSearch(query string, filters map[string]any, topK int) []RetrievalResult {
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil
	}
	s.mu.RLock()
	candidates := make([]KnowledgeChunk, 0)
	for _, kbChunks := range s.chunks {
		for _, chunk := range kbChunks {
			if payloadMatchesFilters(chunkPayload(chunk), filters) {
				candidates = append(candidates, chunk)
			}
		}
	}
	s.mu.RUnlock()
	if len(candidates) == 0 {
		return nil
	}
	docFreq := map[string]int{}
	chunkTokens := make([][]string, len(candidates))
	totalLen := 0
	for i, chunk := range candidates {
		seen := map[string]bool{}
		chunkTokens[i] = tokenize(chunk.Text)
		totalLen += len(chunkTokens[i])
		for _, token := range chunkTokens[i] {
			if !seen[token] {
				docFreq[token]++
				seen[token] = true
			}
		}
	}
	avgLen := float64(totalLen) / math.Max(1, float64(len(candidates)))
	results := []RetrievalResult{}
	for i, chunk := range candidates {
		freq := map[string]int{}
		for _, token := range chunkTokens[i] {
			freq[token]++
		}
		score := 0.0
		docLen := float64(len(chunkTokens[i]))
		for _, token := range tokens {
			tf := float64(freq[token])
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (float64(len(candidates))-float64(docFreq[token])+0.5)/(float64(docFreq[token])+0.5))
			score += idf * (tf * 2.2) / (tf + 1.2*(0.25+0.75*docLen/math.Max(1, avgLen)))
		}
		if score == 0 {
			continue
		}
		results = append(results, retrievalFromChunk(chunk, score))
	}
	sort.Slice(results, func(i, j int) bool { return results[i].KeywordScore > results[j].KeywordScore })
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results
}

func retrievalFromVector(item VectorSearchResult) RetrievalResult {
	payload := cloneAnyPayload(item.Payload)
	text := item.Text
	if text == "" {
		text, _ = payload["text"].(string)
	}
	chunkID := fmt.Sprint(payload["chunk_id"])
	if chunkID == "" {
		chunkID = item.ID
	}
	score := float64(item.Score)
	return RetrievalResult{
		ChunkID:  chunkID,
		Text:     text,
		Score:    score,
		Citation: citationFromPayload(payload, score),
		Metadata: payload,
	}
}

func retrievalFromChunk(chunk KnowledgeChunk, keywordScore float64) RetrievalResult {
	return RetrievalResult{
		ChunkID:      chunk.ChunkID,
		Text:         chunk.Text,
		Score:        keywordScore,
		KeywordScore: keywordScore,
		Citation:     citationFromChunk(chunk, keywordScore),
		Metadata:     chunkPayload(chunk),
	}
}

func retrievalToCore(result RetrievalResult, kbID string) core.KBChunk {
	metadata := stringifyPayload(result.Metadata)
	document := result.Citation.Title
	if document == "" {
		document = result.Citation.SourceFile
	}
	if kbID == "" {
		kbID = fmt.Sprint(result.Metadata["kb_id"])
	}
	return core.KBChunk{
		ID:       result.ChunkID,
		KBID:     kbID,
		Document: document,
		Content:  result.Text,
		Metadata: metadata,
		Score:    result.Score,
	}
}

func (chunk KnowledgeChunk) toCore() core.KBChunk {
	return core.KBChunk{
		ID:       chunk.ChunkID,
		KBID:     chunk.KBID,
		Document: firstNonEmpty(chunk.Title, chunk.SourceFile),
		Content:  chunk.Text,
		Metadata: stringifyPayload(chunkPayload(chunk)),
	}
}

func applyLocalRerank(query string, results []RetrievalResult) {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return
	}
	querySet := map[string]bool{}
	for _, token := range queryTokens {
		querySet[token] = true
	}
	for i := range results {
		overlap := 0
		for _, token := range tokenize(results[i].Text) {
			if querySet[token] {
				overlap++
			}
		}
		results[i].RerankScore = float64(overlap) / float64(len(querySet))
		results[i].Score += 0.15 * results[i].RerankScore
		results[i].Citation.Score = results[i].Score
	}
}

func tokenize(value string) []string {
	raw := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= '\u4e00' && r <= '\u9fff')
	})
	tokens := make([]string, 0, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func chunkPayload(chunk KnowledgeChunk) map[string]any {
	payload := sanitizeMetadata(chunk.Metadata)
	if payload == nil {
		payload = map[string]any{}
	}
	payload["chunk_id"] = chunk.ChunkID
	payload["kb_id"] = chunk.KBID
	payload["source_id"] = chunk.SourceID
	payload["document_id"] = chunk.DocumentID
	payload["source_uri"] = sanitizeURI(chunk.SourceURI)
	payload["source_file"] = chunk.SourceFile
	payload["title"] = chunk.Title
	payload["section"] = chunk.Section
	payload["chunk_index"] = chunk.ChunkIndex
	payload["content_hash"] = chunk.ContentHash
	payload["updated_at"] = chunk.UpdatedAt.Format(time.RFC3339)
	payload["text"] = security.RedactString(chunk.Text)
	return payload
}

func citationFromChunk(chunk KnowledgeChunk, score float64) Citation {
	return Citation{
		DocumentID: chunk.DocumentID,
		SourceID:   chunk.SourceID,
		SourceURI:  chunk.SourceURI,
		SourceFile: chunk.SourceFile,
		Title:      chunk.Title,
		Section:    chunk.Section,
		ChunkID:    chunk.ChunkID,
		UpdatedAt:  chunk.UpdatedAt,
		Score:      score,
	}
}

func citationFromPayload(payload map[string]any, score float64) Citation {
	updatedAt, _ := time.Parse(time.RFC3339, fmt.Sprint(payload["updated_at"]))
	return Citation{
		DocumentID: fmt.Sprint(payload["document_id"]),
		SourceID:   fmt.Sprint(payload["source_id"]),
		SourceURI:  fmt.Sprint(payload["source_uri"]),
		SourceFile: fmt.Sprint(payload["source_file"]),
		Title:      fmt.Sprint(payload["title"]),
		Section:    fmt.Sprint(payload["section"]),
		ChunkID:    fmt.Sprint(payload["chunk_id"]),
		UpdatedAt:  updatedAt,
		Score:      score,
	}
}

func citationSources(citations []Citation) []string {
	out := make([]string, 0, len(citations))
	seen := map[string]bool{}
	for _, citation := range citations {
		value := firstNonEmpty(citation.SourceFile, citation.SourceURI, citation.DocumentID)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func expectedSourceHit(expected, retrieved []string) bool {
	if len(expected) == 0 {
		return len(retrieved) > 0
	}
	for _, want := range expected {
		for _, got := range retrieved {
			if strings.Contains(got, want) || strings.Contains(want, got) {
				return true
			}
		}
	}
	return false
}

func evalSummary(item RAGEvalCase, recallHit, refused bool) string {
	if item.MustRefuse {
		if refused {
			return "refused as expected"
		}
		return "expected refusal but answer had evidence"
	}
	if recallHit {
		return "expected source retrieved"
	}
	return "expected source was not retrieved"
}

func sourcePathIncluded(rel string, includes, excludes []string) bool {
	rel = filepath.ToSlash(rel)
	for _, pattern := range excludes {
		if globMatch(pattern, rel) {
			return false
		}
	}
	if len(includes) == 0 {
		return true
	}
	for _, pattern := range includes {
		if globMatch(pattern, rel) {
			return true
		}
	}
	return false
}

func globMatch(pattern, rel string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		return false
	}
	if strings.HasPrefix(pattern, "**/*") {
		return strings.HasSuffix(rel, strings.TrimPrefix(pattern, "**/*"))
	}
	ok, _ := filepath.Match(pattern, rel)
	if ok {
		return true
	}
	ok, _ = filepath.Match(pattern, filepath.Base(rel))
	return ok
}

func (s *Service) ensureKBMapsLocked(kbID string) {
	if s.sources[kbID] == nil {
		s.sources[kbID] = map[string]KnowledgeSource{}
	}
	if s.documents[kbID] == nil {
		s.documents[kbID] = map[string]KnowledgeDocument{}
	}
	if s.chunks[kbID] == nil {
		s.chunks[kbID] = map[string]KnowledgeChunk{}
	}
	if s.jobs[kbID] == nil {
		s.jobs[kbID] = map[string]KnowledgeIndexJob{}
	}
	if s.documentChunks[kbID] == nil {
		s.documentChunks[kbID] = map[string][]string{}
	}
}

func (s *Service) replaceChunksLocked(kbID, documentID string, chunks []KnowledgeChunk) {
	s.ensureKBMapsLocked(kbID)
	s.removeDocumentChunksLocked(kbID, documentID)
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		s.chunks[kbID][chunk.ChunkID] = chunk
		ids = append(ids, chunk.ChunkID)
	}
	s.documentChunks[kbID][documentID] = ids
}

func (s *Service) removeDocumentLocked(kbID, documentID string) {
	delete(s.documents[kbID], documentID)
	s.removeDocumentChunksLocked(kbID, documentID)
}

func (s *Service) removeDocumentChunksLocked(kbID, documentID string) {
	for _, chunkID := range s.documentChunks[kbID][documentID] {
		delete(s.chunks[kbID], chunkID)
	}
	delete(s.documentChunks[kbID], documentID)
}

func stableDocumentID(kbID, sourceID, sourceURI string) string {
	return "kbdoc_" + shortHash(kbID+"|"+sourceID+"|"+sourceURI, 20)
}

func stableChunkID(documentID string, index int, contentHash string) string {
	return "kbch_" + shortHash(fmt.Sprintf("%s|%d|%s", documentID, index, contentHash), 20)
}

func buildKBSourceFile(kbID, filename string) string {
	return kbID + "/" + filename
}

func buildDocumentID(kbID, filename string) string {
	return stableDocumentID(kbID, implicitUploadSourceID, filename)
}

func buildSourceFile(kbID, sourceID, sourceURI string) string {
	if sourceID == implicitUploadSourceID {
		return buildKBSourceFile(kbID, sourceURI)
	}
	return kbID + "/" + sourceID + "/" + filepath.ToSlash(sourceURI)
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func shortHash(content string, n int) string {
	value := hashContent(content)
	if n <= 0 || n > len(value) {
		return value
	}
	return value[:n]
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

func cloneSource(source KnowledgeSource) KnowledgeSource {
	out := source
	out.IncludeGlobs = append([]string(nil), source.IncludeGlobs...)
	out.ExcludeGlobs = append([]string(nil), source.ExcludeGlobs...)
	out.Metadata = cloneAnyMap(source.Metadata)
	return out
}

func sanitizeMetadata(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := map[string]any{}
	for key, value := range input {
		if sensitiveKey(key) {
			out[key] = "[REDACTED]"
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			out[key] = sanitizeMetadata(nested)
			continue
		}
		if text, ok := value.(string); ok && sensitiveKeyValue(text) {
			out[key] = "[REDACTED]"
			continue
		}
		out[key] = value
	}
	return out
}

func sanitizeURI(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return value
	}
	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		if sensitiveKey(key) {
			query.Set(key, "[REDACTED]")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, marker := range []string{"secret", "token", "password", "passwd", "private_key", "apikey", "api_key", "authorization", "cookie", "session"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func sensitiveKeyValue(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "-----begin") || strings.Contains(lower, "authorization: bearer") || strings.Contains(lower, "api_key=")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
