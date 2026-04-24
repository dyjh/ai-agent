package kb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// QdrantIndexConfig stores the connection details needed by the HTTP adapter.
type QdrantIndexConfig struct {
	URL                string
	APIKey             string
	TimeoutSeconds     int
	EmbeddingDimension int
	Distance           string
	Collections        map[string]string
}

// InMemoryVectorIndex is a deterministic local vector index used for tests and fallback mode.
type InMemoryVectorIndex struct {
	mu          sync.RWMutex
	collections map[string]map[string]VectorChunk
	embedder    Embedder
	status      VectorRuntimeStatus
}

// NewInMemoryVectorIndex constructs an in-memory vector index.
func NewInMemoryVectorIndex(embedder Embedder, status VectorRuntimeStatus) *InMemoryVectorIndex {
	if status.VectorBackend == "" {
		status.VectorBackend = "memory"
	}
	return &InMemoryVectorIndex{
		collections: map[string]map[string]VectorChunk{},
		embedder:    embedder,
		status:      cloneStatus(status),
	}
}

// Status returns a copy of the current backend metadata.
func (i *InMemoryVectorIndex) Status() VectorRuntimeStatus {
	return cloneStatus(i.status)
}

// EnsureCollections is a no-op for the in-memory backend.
func (i *InMemoryVectorIndex) EnsureCollections(_ context.Context) error {
	return nil
}

// UpsertChunks stores or replaces chunks in the collection.
func (i *InMemoryVectorIndex) UpsertChunks(_ context.Context, collection string, chunks []VectorChunk) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if _, ok := i.collections[collection]; !ok {
		i.collections[collection] = map[string]VectorChunk{}
	}
	for _, chunk := range chunks {
		i.collections[collection][chunk.ID] = cloneChunk(chunk)
	}
	return nil
}

// DeleteBySourceFile removes all chunks that came from the same source file.
func (i *InMemoryVectorIndex) DeleteBySourceFile(_ context.Context, collection string, sourceFile string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	items := i.collections[collection]
	for id, chunk := range items {
		if chunk.SourceFile == sourceFile {
			delete(items, id)
		}
	}
	return nil
}

// Search performs cosine similarity search over the collection.
func (i *InMemoryVectorIndex) Search(ctx context.Context, collection string, query string, filters map[string]any, topK int) ([]VectorSearchResult, error) {
	vector, err := i.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}

	i.mu.RLock()
	defer i.mu.RUnlock()
	records := i.collections[collection]
	results := make([]VectorSearchResult, 0, len(records))
	for _, chunk := range records {
		if !payloadMatchesFilters(chunk.Payload, filters) {
			continue
		}
		results = append(results, VectorSearchResult{
			ID:      chunk.ID,
			Text:    chunk.Text,
			Score:   float32(cosine(vector, chunk.Vector)),
			Payload: cloneAnyPayload(chunk.Payload),
		})
	}
	sort.Slice(results, func(left, right int) bool {
		return results[left].Score > results[right].Score
	})
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

// Health always succeeds for the in-memory backend.
func (i *InMemoryVectorIndex) Health(_ context.Context) error {
	return nil
}

// QdrantVectorIndex is a thin HTTP adapter over Qdrant's REST API.
type QdrantVectorIndex struct {
	baseURL            string
	apiKey             string
	timeoutSeconds     int
	embeddingDimension int
	distance           string
	collections        map[string]string
	embedder           Embedder
	client             httpDoer
	status             VectorRuntimeStatus
}

// NewQdrantVectorIndex constructs a Qdrant-backed index.
func NewQdrantVectorIndex(cfg QdrantIndexConfig, embedder Embedder, client httpDoer) (*QdrantVectorIndex, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("qdrant url is required")
	}
	if cfg.EmbeddingDimension <= 0 {
		return nil, fmt.Errorf("embedding dimension must be positive")
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 10
	}
	if strings.TrimSpace(cfg.Distance) == "" {
		cfg.Distance = "cosine"
	}
	if client == nil {
		client = &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}
	}

	collections := map[string]string{}
	for key, value := range cfg.Collections {
		if strings.TrimSpace(value) == "" {
			continue
		}
		collections[key] = value
	}

	statusCollections := make(map[string]string, len(collections))
	for _, name := range collections {
		statusCollections[name] = "configured"
	}

	return &QdrantVectorIndex{
		baseURL:            strings.TrimRight(cfg.URL, "/"),
		apiKey:             cfg.APIKey,
		timeoutSeconds:     cfg.TimeoutSeconds,
		embeddingDimension: cfg.EmbeddingDimension,
		distance:           strings.ToLower(cfg.Distance),
		collections:        collections,
		embedder:           embedder,
		client:             client,
		status: VectorRuntimeStatus{
			VectorBackend: "qdrant",
			Qdrant:        "configured",
			Collections:   statusCollections,
		},
	}, nil
}

// Status returns a copy of the current backend metadata.
func (q *QdrantVectorIndex) Status() VectorRuntimeStatus {
	return cloneStatus(q.status)
}

// EnsureCollections creates configured collections when they do not already exist.
func (q *QdrantVectorIndex) EnsureCollections(ctx context.Context) error {
	for _, name := range q.collections {
		exists, err := q.collectionExists(ctx, name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		body := map[string]any{
			"vectors": map[string]any{
				"size":     q.embeddingDimension,
				"distance": q.qdrantDistance(),
			},
		}
		if err := q.requestJSON(ctx, http.MethodPut, "/collections/"+url.PathEscape(name), body, nil); err != nil {
			return err
		}
	}
	return nil
}

// UpsertChunks writes points into the target collection.
func (q *QdrantVectorIndex) UpsertChunks(ctx context.Context, collection string, chunks []VectorChunk) error {
	if len(chunks) == 0 {
		return nil
	}
	points := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		points = append(points, map[string]any{
			"id":      chunk.ID,
			"vector":  chunk.Vector,
			"payload": sanitizePayload(chunk),
		})
	}
	body := map[string]any{"points": points}
	return q.requestJSON(ctx, http.MethodPut, "/collections/"+url.PathEscape(collection)+"/points", body, nil)
}

// DeleteBySourceFile removes all points indexed from the same source file.
func (q *QdrantVectorIndex) DeleteBySourceFile(ctx context.Context, collection string, sourceFile string) error {
	body := map[string]any{
		"filter": map[string]any{
			"must": []map[string]any{
				{
					"key": "source_file",
					"match": map[string]any{
						"value": sourceFile,
					},
				},
			},
		},
	}
	return q.requestJSON(ctx, http.MethodPost, "/collections/"+url.PathEscape(collection)+"/points/delete", body, nil)
}

// Search performs vector search with optional payload filters.
func (q *QdrantVectorIndex) Search(ctx context.Context, collection string, query string, filters map[string]any, topK int) ([]VectorSearchResult, error) {
	vector, err := q.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	if topK <= 0 {
		topK = 5
	}

	body := map[string]any{
		"query":        vector,
		"limit":        topK,
		"with_payload": true,
	}
	if len(filters) > 0 {
		body["filter"] = buildQdrantFilter(filters)
	}

	var response struct {
		Result struct {
			Points []struct {
				ID      any            `json:"id"`
				Score   float32        `json:"score"`
				Payload map[string]any `json:"payload"`
			} `json:"points"`
		} `json:"result"`
	}
	if err := q.requestJSON(ctx, http.MethodPost, "/collections/"+url.PathEscape(collection)+"/points/query", body, &response); err != nil {
		return nil, err
	}

	results := make([]VectorSearchResult, 0, len(response.Result.Points))
	for _, point := range response.Result.Points {
		payload := cloneAnyPayload(point.Payload)
		text, _ := payload["text"].(string)
		results = append(results, VectorSearchResult{
			ID:      fmt.Sprint(point.ID),
			Text:    text,
			Score:   point.Score,
			Payload: payload,
		})
	}
	return results, nil
}

// Health checks Qdrant reachability.
func (q *QdrantVectorIndex) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, q.urlWithTimeout("/healthz"), nil)
	if err != nil {
		return err
	}
	q.decorateHeaders(req)
	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &qdrantAPIError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return nil
}

func (q *QdrantVectorIndex) collectionExists(ctx context.Context, collection string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, q.urlWithTimeout("/collections/"+url.PathEscape(collection)), nil)
	if err != nil {
		return false, err
	}
	q.decorateHeaders(req)
	resp, err := q.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, &qdrantAPIError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return true, nil
}

func (q *QdrantVectorIndex) requestJSON(ctx context.Context, method, path string, requestBody any, responseBody any) error {
	var reader io.Reader
	if requestBody != nil {
		raw, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, q.urlWithTimeout(path), reader)
	if err != nil {
		return err
	}
	q.decorateHeaders(req)
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &qdrantAPIError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	if responseBody == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(responseBody)
}

func (q *QdrantVectorIndex) decorateHeaders(req *http.Request) {
	if q.apiKey != "" {
		req.Header.Set("api-key", q.apiKey)
	}
}

func (q *QdrantVectorIndex) urlWithTimeout(path string) string {
	endpoint := q.baseURL + path
	if q.timeoutSeconds <= 0 {
		return endpoint
	}
	values := url.Values{}
	values.Set("timeout", fmt.Sprintf("%d", q.timeoutSeconds))
	return endpoint + "?" + values.Encode()
}

func (q *QdrantVectorIndex) qdrantDistance() string {
	switch q.distance {
	case "dot":
		return "Dot"
	case "euclid":
		return "Euclid"
	case "manhattan":
		return "Manhattan"
	default:
		return "Cosine"
	}
}

type qdrantAPIError struct {
	StatusCode int
	Body       string
}

func (e *qdrantAPIError) Error() string {
	return fmt.Sprintf("qdrant request failed: status=%d body=%s", e.StatusCode, strings.TrimSpace(e.Body))
}

func cloneStatus(status VectorRuntimeStatus) VectorRuntimeStatus {
	out := status
	if len(status.Collections) > 0 {
		out.Collections = make(map[string]string, len(status.Collections))
		for key, value := range status.Collections {
			out.Collections[key] = value
		}
	}
	return out
}

func cloneChunk(chunk VectorChunk) VectorChunk {
	out := chunk
	out.Payload = cloneAnyPayload(chunk.Payload)
	if len(chunk.Vector) > 0 {
		out.Vector = append([]float32(nil), chunk.Vector...)
	}
	return out
}

func cloneAnyPayload(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func payloadMatchesFilters(payload map[string]any, filters map[string]any) bool {
	if len(filters) == 0 {
		return true
	}
	for key, want := range filters {
		got, ok := payload[key]
		if !ok || fmt.Sprint(got) != fmt.Sprint(want) {
			return false
		}
	}
	return true
}

func buildQdrantFilter(filters map[string]any) map[string]any {
	must := make([]map[string]any, 0, len(filters))
	for key, value := range filters {
		must = append(must, map[string]any{
			"key": key,
			"match": map[string]any{
				"value": value,
			},
		})
	}
	sort.Slice(must, func(left, right int) bool {
		return fmt.Sprint(must[left]["key"]) < fmt.Sprint(must[right]["key"])
	})
	return map[string]any{"must": must}
}

func sanitizePayload(chunk VectorChunk) map[string]any {
	allowed := map[string]bool{
		"text":         true,
		"chunk_id":     true,
		"kb_id":        true,
		"source_id":    true,
		"document_id":  true,
		"filename":     true,
		"source_uri":   true,
		"source_file":  true,
		"title":        true,
		"memory_type":  true,
		"scope":        true,
		"project_key":  true,
		"section":      true,
		"content_hash": true,
		"updated_at":   true,
		"chunk_index":  true,
	}
	payload := map[string]any{}
	for key, value := range chunk.Payload {
		if allowed[key] {
			payload[key] = value
		}
	}
	if chunk.SourceFile != "" {
		payload["source_file"] = chunk.SourceFile
	}
	if chunk.Text != "" {
		payload["text"] = chunk.Text
	}
	return payload
}

func cosine(a, b []float32) float64 {
	size := len(a)
	if len(b) < size {
		size = len(b)
	}
	if size == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := 0; i < size; i++ {
		af := float64(a[i])
		bf := float64(b[i])
		dot += af * bf
		normA += af * af
		normB += bf * bf
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
