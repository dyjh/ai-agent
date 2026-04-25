package kb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PineconeIndexConfig stores Pinecone data-plane connection details.
type PineconeIndexConfig struct {
	IndexHost          string
	APIKey             string
	TimeoutSeconds     int
	EmbeddingDimension int
	Namespaces         map[string]string
}

// PineconeVectorIndex adapts Pinecone namespaces to the VectorIndex contract.
type PineconeVectorIndex struct {
	indexHost          string
	apiKey             string
	timeoutSeconds     int
	embeddingDimension int
	namespaces         map[string]string
	embedder           Embedder
	client             httpDoer
	status             VectorRuntimeStatus
}

// NewPineconeVectorIndex constructs a Pinecone-backed vector index.
func NewPineconeVectorIndex(cfg PineconeIndexConfig, embedder Embedder, client httpDoer) (*PineconeVectorIndex, error) {
	if strings.TrimSpace(cfg.IndexHost) == "" {
		return nil, fmt.Errorf("pinecone index_host is required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("pinecone api_key is required")
	}
	if cfg.EmbeddingDimension <= 0 {
		return nil, fmt.Errorf("embedding dimension must be positive")
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 10
	}
	if client == nil {
		client = &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}
	}
	namespaces := map[string]string{}
	for key, value := range cfg.Namespaces {
		if name := strings.TrimSpace(value); name != "" {
			namespaces[key] = name
		}
	}
	statusCollections := make(map[string]string, len(namespaces))
	for _, name := range namespaces {
		statusCollections[name] = "configured"
	}
	return &PineconeVectorIndex{
		indexHost:          strings.TrimRight(strings.TrimSpace(cfg.IndexHost), "/"),
		apiKey:             cfg.APIKey,
		timeoutSeconds:     cfg.TimeoutSeconds,
		embeddingDimension: cfg.EmbeddingDimension,
		namespaces:         namespaces,
		embedder:           embedder,
		client:             client,
		status: VectorRuntimeStatus{
			VectorBackend: "pinecone",
			Collections:   statusCollections,
		},
	}, nil
}

// Status returns a copy of the current backend metadata.
func (p *PineconeVectorIndex) Status() VectorRuntimeStatus {
	return cloneStatus(p.status)
}

// EnsureCollections checks Pinecone index reachability. Namespaces are created
// lazily by Pinecone on first upsert.
func (p *PineconeVectorIndex) EnsureCollections(ctx context.Context) error {
	return p.Health(ctx)
}

// UpsertChunks writes vector chunks into the Pinecone namespace.
func (p *PineconeVectorIndex) UpsertChunks(ctx context.Context, collection string, chunks []VectorChunk) error {
	if len(chunks) == 0 {
		return nil
	}
	vectors := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		if len(chunk.Vector) != p.embeddingDimension {
			return fmt.Errorf("pinecone vector dimension mismatch: got %d, want %d", len(chunk.Vector), p.embeddingDimension)
		}
		vectors = append(vectors, map[string]any{
			"id":       chunk.ID,
			"values":   chunk.Vector,
			"metadata": pineconeMetadata(sanitizePayload(chunk)),
		})
	}
	return p.requestJSON(ctx, http.MethodPost, "/vectors/upsert", map[string]any{
		"namespace": collection,
		"vectors":   vectors,
	}, nil)
}

// DeleteBySourceFile deletes vectors with matching source_file metadata.
func (p *PineconeVectorIndex) DeleteBySourceFile(ctx context.Context, collection string, sourceFile string) error {
	return p.requestJSON(ctx, http.MethodPost, "/vectors/delete", map[string]any{
		"namespace": collection,
		"filter": map[string]any{
			"source_file": map[string]any{"$eq": sourceFile},
		},
	}, nil)
}

// Search performs a vector query against Pinecone.
func (p *PineconeVectorIndex) Search(ctx context.Context, collection string, query string, filters map[string]any, topK int) ([]VectorSearchResult, error) {
	vector, err := p.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	if topK <= 0 {
		topK = 5
	}
	body := map[string]any{
		"namespace":       collection,
		"vector":          vector,
		"topK":            topK,
		"includeMetadata": true,
	}
	if len(filters) > 0 {
		body["filter"] = buildPineconeFilter(filters)
	}
	var response struct {
		Matches []struct {
			ID       string         `json:"id"`
			Score    float32        `json:"score"`
			Metadata map[string]any `json:"metadata"`
		} `json:"matches"`
	}
	if err := p.requestJSON(ctx, http.MethodPost, "/query", body, &response); err != nil {
		return nil, err
	}
	results := make([]VectorSearchResult, 0, len(response.Matches))
	for _, match := range response.Matches {
		payload := cloneAnyPayload(match.Metadata)
		text, _ := payload["text"].(string)
		results = append(results, VectorSearchResult{
			ID:      match.ID,
			Text:    text,
			Score:   match.Score,
			Payload: payload,
		})
	}
	return results, nil
}

// Health checks Pinecone data-plane reachability.
func (p *PineconeVectorIndex) Health(ctx context.Context) error {
	return p.requestJSON(ctx, http.MethodPost, "/describe_index_stats", map[string]any{}, nil)
}

func (p *PineconeVectorIndex) requestJSON(ctx context.Context, method, path string, requestBody any, responseBody any) error {
	var reader io.Reader
	if requestBody != nil {
		raw, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.url(path), reader)
	if err != nil {
		return err
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("api-key", p.apiKey)
	req.Header.Set("X-Pinecone-API-Version", "2025-04")
	if requestBody != nil {
		req.Header.Set("content-type", "application/json")
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("pinecone request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if responseBody == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(responseBody)
}

func (p *PineconeVectorIndex) url(path string) string {
	endpoint := p.indexHost + path
	if p.timeoutSeconds <= 0 {
		return endpoint
	}
	values := url.Values{}
	values.Set("timeout", fmt.Sprintf("%d", p.timeoutSeconds))
	return endpoint + "?" + values.Encode()
}

func buildPineconeFilter(filters map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range filters {
		out[key] = map[string]any{"$eq": value}
	}
	return out
}

func pineconeMetadata(payload map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range payload {
		switch typed := value.(type) {
		case string:
			out[key] = typed
		case bool:
			out[key] = typed
		case int, int64, float32, float64:
			out[key] = typed
		case []string:
			out[key] = typed
		default:
			out[key] = fmt.Sprint(value)
		}
	}
	return out
}
