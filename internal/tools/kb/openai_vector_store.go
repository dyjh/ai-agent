package kb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// OpenAIVectorStoreConfig stores OpenAI Vector Store connection details.
type OpenAIVectorStoreConfig struct {
	BaseURL        string
	APIKey         string
	TimeoutSeconds int
	VectorStores   map[string]string
}

// OpenAIVectorStoreIndex adapts OpenAI Vector Stores to the VectorIndex contract.
// OpenAI stores own embedding and chunking internally; this adapter uploads one
// local KB chunk per OpenAI file so source metadata can be attached safely.
type OpenAIVectorStoreIndex struct {
	baseURL        string
	apiKey         string
	timeoutSeconds int
	vectorStores   map[string]string
	client         httpDoer
	status         VectorRuntimeStatus
	mu             sync.Mutex
	sourceFiles    map[string]map[string][]string
}

// NewOpenAIVectorStoreIndex constructs an OpenAI-backed vector store index.
func NewOpenAIVectorStoreIndex(cfg OpenAIVectorStoreConfig, client httpDoer) (*OpenAIVectorStoreIndex, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("openai_kb base_url is required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("openai_kb api_key is required")
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 30
	}
	if client == nil {
		client = &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}
	}
	stores := map[string]string{}
	for key, value := range cfg.VectorStores {
		if id := strings.TrimSpace(value); id != "" {
			stores[key] = id
		}
	}
	statusCollections := make(map[string]string, len(stores))
	for _, id := range stores {
		statusCollections[id] = "configured"
	}
	return &OpenAIVectorStoreIndex{
		baseURL:        strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:         cfg.APIKey,
		timeoutSeconds: cfg.TimeoutSeconds,
		vectorStores:   stores,
		client:         client,
		status: VectorRuntimeStatus{
			VectorBackend: "openai",
			Collections:   statusCollections,
		},
		sourceFiles: map[string]map[string][]string{},
	}, nil
}

// Status returns a copy of the current backend metadata.
func (o *OpenAIVectorStoreIndex) Status() VectorRuntimeStatus {
	return cloneStatus(o.status)
}

// EnsureCollections checks that configured OpenAI vector stores exist.
func (o *OpenAIVectorStoreIndex) EnsureCollections(ctx context.Context) error {
	for _, storeID := range o.vectorStores {
		if err := o.requestJSON(ctx, http.MethodGet, "/vector_stores/"+url.PathEscape(storeID), nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// UpsertChunks uploads chunks as OpenAI files and attaches them to the vector store.
func (o *OpenAIVectorStoreIndex) UpsertChunks(ctx context.Context, collection string, chunks []VectorChunk) error {
	if len(chunks) == 0 {
		return nil
	}
	for _, chunk := range chunks {
		payload := sanitizePayload(chunk)
		fileID, err := o.uploadChunkFile(ctx, chunk)
		if err != nil {
			return err
		}
		if err := o.attachFile(ctx, collection, fileID, openAIFileAttributes(payload)); err != nil {
			return err
		}
		if err := o.waitFileReady(ctx, collection, fileID); err != nil {
			return err
		}
		o.rememberSourceFile(collection, chunk.SourceFile, fileID)
	}
	return nil
}

// DeleteBySourceFile removes files this process has attached for the source.
func (o *OpenAIVectorStoreIndex) DeleteBySourceFile(ctx context.Context, collection string, sourceFile string) error {
	fileIDs := o.takeSourceFiles(collection, sourceFile)
	for _, fileID := range fileIDs {
		if err := o.requestJSON(ctx, http.MethodDelete, "/vector_stores/"+url.PathEscape(collection)+"/files/"+url.PathEscape(fileID), nil, nil); err != nil {
			return err
		}
		_ = o.requestJSON(ctx, http.MethodDelete, "/files/"+url.PathEscape(fileID), nil, nil)
	}
	return nil
}

// Search queries OpenAI Vector Store search using text query.
func (o *OpenAIVectorStoreIndex) Search(ctx context.Context, collection string, query string, filters map[string]any, topK int) ([]VectorSearchResult, error) {
	if topK <= 0 {
		topK = 5
	}
	body := map[string]any{
		"query":           query,
		"max_num_results": topK,
	}
	if len(filters) > 0 {
		body["filters"] = buildOpenAISearchFilter(filters)
	}
	var response struct {
		Data []struct {
			FileID     string         `json:"file_id"`
			Filename   string         `json:"filename"`
			Score      float32        `json:"score"`
			Attributes map[string]any `json:"attributes"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"data"`
	}
	if err := o.requestJSON(ctx, http.MethodPost, "/vector_stores/"+url.PathEscape(collection)+"/search", body, &response); err != nil {
		return nil, err
	}
	results := make([]VectorSearchResult, 0, len(response.Data))
	for _, item := range response.Data {
		payload := cloneAnyPayload(item.Attributes)
		if payload == nil {
			payload = map[string]any{}
		}
		text := openAISearchText(item.Content)
		if text != "" {
			payload["text"] = text
		}
		if item.Filename != "" {
			payload["filename"] = item.Filename
		}
		id := firstNonEmptyString(fmt.Sprint(payload["chunk_id"]), item.FileID)
		results = append(results, VectorSearchResult{
			ID:      id,
			Text:    text,
			Score:   item.Score,
			Payload: payload,
		})
	}
	return results, nil
}

// Health checks OpenAI API reachability through a lightweight models request.
func (o *OpenAIVectorStoreIndex) Health(ctx context.Context) error {
	return o.requestJSON(ctx, http.MethodGet, "/models", nil, nil)
}

func (o *OpenAIVectorStoreIndex) uploadChunkFile(ctx context.Context, chunk VectorChunk) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("purpose", "assistants"); err != nil {
		return "", err
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(openAIChunkFilename(chunk))))
	header.Set("Content-Type", "text/markdown")
	part, err := writer.CreatePart(header)
	if err != nil {
		return "", err
	}
	if _, err := io.WriteString(part, chunk.Text); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.url("/files"), &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("authorization", "Bearer "+o.apiKey)
	req.Header.Set("content-type", writer.FormDataContentType())
	req.Header.Set("accept", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai file upload failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", err
	}
	if response.ID == "" {
		return "", fmt.Errorf("openai file upload returned empty file id")
	}
	return response.ID, nil
}

func (o *OpenAIVectorStoreIndex) attachFile(ctx context.Context, storeID, fileID string, attributes map[string]any) error {
	return o.requestJSON(ctx, http.MethodPost, "/vector_stores/"+url.PathEscape(storeID)+"/files", map[string]any{
		"file_id":    fileID,
		"attributes": attributes,
	}, nil)
}

func (o *OpenAIVectorStoreIndex) waitFileReady(ctx context.Context, storeID, fileID string) error {
	deadline := time.Now().Add(time.Duration(o.timeoutSeconds) * time.Second)
	for {
		var response struct {
			Status    string `json:"status"`
			LastError any    `json:"last_error,omitempty"`
		}
		if err := o.requestJSON(ctx, http.MethodGet, "/vector_stores/"+url.PathEscape(storeID)+"/files/"+url.PathEscape(fileID), nil, &response); err != nil {
			return err
		}
		switch strings.ToLower(response.Status) {
		case "", "completed":
			return nil
		case "failed", "cancelled":
			return fmt.Errorf("openai vector store file %s status=%s error=%v", fileID, response.Status, response.LastError)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("openai vector store file %s indexing timed out with status=%s", fileID, response.Status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (o *OpenAIVectorStoreIndex) requestJSON(ctx context.Context, method, path string, requestBody any, responseBody any) error {
	var reader io.Reader
	if requestBody != nil {
		raw, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, o.url(path), reader)
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "Bearer "+o.apiKey)
	req.Header.Set("accept", "application/json")
	if requestBody != nil {
		req.Header.Set("content-type", "application/json")
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("openai vector store request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if responseBody == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(responseBody)
}

func (o *OpenAIVectorStoreIndex) url(path string) string {
	return o.baseURL + path
}

func (o *OpenAIVectorStoreIndex) rememberSourceFile(collection, sourceFile, fileID string) {
	if sourceFile == "" || fileID == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.sourceFiles[collection] == nil {
		o.sourceFiles[collection] = map[string][]string{}
	}
	o.sourceFiles[collection][sourceFile] = append(o.sourceFiles[collection][sourceFile], fileID)
}

func (o *OpenAIVectorStoreIndex) takeSourceFiles(collection, sourceFile string) []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.sourceFiles[collection] == nil {
		return nil
	}
	fileIDs := append([]string(nil), o.sourceFiles[collection][sourceFile]...)
	delete(o.sourceFiles[collection], sourceFile)
	return fileIDs
}

func openAIFileAttributes(payload map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range payload {
		if key == "text" {
			continue
		}
		switch typed := value.(type) {
		case string:
			out[key] = typed
		case bool:
			out[key] = typed
		case int, int64, float32, float64:
			out[key] = typed
		default:
			out[key] = fmt.Sprint(value)
		}
	}
	return out
}

func buildOpenAISearchFilter(filters map[string]any) map[string]any {
	items := make([]map[string]any, 0, len(filters))
	for key, value := range filters {
		items = append(items, map[string]any{
			"type":  "eq",
			"key":   key,
			"value": value,
		})
	}
	if len(items) == 1 {
		return items[0]
	}
	return map[string]any{
		"type":    "and",
		"filters": items,
	}
}

func openAISearchText(content []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	parts := make([]string, 0, len(content))
	for _, item := range content {
		if strings.TrimSpace(item.Text) != "" {
			parts = append(parts, strings.TrimSpace(item.Text))
		}
	}
	return strings.Join(parts, "\n")
}

func openAIChunkFilename(chunk VectorChunk) string {
	name := fmt.Sprintf("%s.md", chunk.ID)
	if filename, _ := chunk.Payload["filename"].(string); strings.TrimSpace(filename) != "" {
		name = filepath.Base(filename)
	}
	return strings.ReplaceAll(name, `"`, `_`)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func escapeQuotes(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}
