package kb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"strings"
	"time"

	"local-agent/internal/config"
)

// Embedder turns text into vectors.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// NewEmbedder creates the embedding provider selected by runtime config.
func NewEmbedder(cfg config.EmbeddingConfig, dimensions int) (Embedder, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = config.ProviderFake
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	switch provider {
	case config.ProviderFake:
		return FakeEmbedder{Dimensions: dimensions}, nil
	case config.ProviderOpenAICompatible:
		if strings.TrimSpace(cfg.Model) == "" {
			return nil, fmt.Errorf("embedding model is required for provider %s", provider)
		}
		baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
		if baseURL == "" {
			return nil, fmt.Errorf("embedding base_url is required for provider %s", provider)
		}
		return HTTPEmbedder{
			Provider:           provider,
			BaseURL:            baseURL,
			APIKey:             cfg.APIKey,
			Model:              cfg.Model,
			ExpectedDimensions: dimensions,
			Client:             &http.Client{Timeout: timeout},
		}, nil
	case config.ProviderOllama:
		if strings.TrimSpace(cfg.Model) == "" {
			return nil, fmt.Errorf("embedding model is required for provider %s", provider)
		}
		baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
		if baseURL == "" {
			baseURL = "http://127.0.0.1:11434"
		}
		return HTTPEmbedder{
			Provider:           provider,
			BaseURL:            baseURL,
			APIKey:             cfg.APIKey,
			Model:              cfg.Model,
			ExpectedDimensions: dimensions,
			Client:             &http.Client{Timeout: timeout},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s", cfg.Provider)
	}
}

// FakeEmbedder provides deterministic embeddings for tests and local MVP mode.
type FakeEmbedder struct {
	Dimensions int
}

// Embed creates a deterministic pseudo-embedding.
func (e FakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	dims := e.Dimensions
	if dims <= 0 {
		dims = 32
	}

	vector := make([]float32, dims)
	for index, token := range []byte(text) {
		hasher := fnv.New32a()
		_, _ = hasher.Write([]byte{token})
		slot := int(hasher.Sum32()) % dims
		vector[slot] += float32((index%7)+1) / 10
	}
	return vector, nil
}

// HTTPEmbedder calls OpenAI-compatible or Ollama embedding endpoints.
type HTTPEmbedder struct {
	Provider           string
	BaseURL            string
	APIKey             string
	Model              string
	ExpectedDimensions int
	Client             *http.Client
}

// Embed creates one embedding vector through the configured HTTP provider.
func (e HTTPEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	switch strings.ToLower(strings.TrimSpace(e.Provider)) {
	case config.ProviderOpenAICompatible:
		return e.embedOpenAICompatible(ctx, text)
	case config.ProviderOllama:
		return e.embedOllama(ctx, text)
	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s", e.Provider)
	}
}

func (e HTTPEmbedder) embedOpenAICompatible(ctx context.Context, text string) ([]float32, error) {
	var response struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Error any `json:"error,omitempty"`
	}
	if err := e.postJSON(ctx, openAIEmbeddingEndpoint(e.BaseURL), map[string]any{
		"model": e.Model,
		"input": text,
	}, &response); err != nil {
		return nil, err
	}
	if len(response.Data) == 0 || len(response.Data[0].Embedding) == 0 {
		return nil, errors.New("embedding provider returned no vectors")
	}
	return e.checkDimensions(response.Data[0].Embedding)
}

func (e HTTPEmbedder) embedOllama(ctx context.Context, text string) ([]float32, error) {
	var embedResponse struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	err := e.postJSON(ctx, strings.TrimRight(e.BaseURL, "/")+"/api/embed", map[string]any{
		"model": e.Model,
		"input": text,
	}, &embedResponse)
	if err == nil && len(embedResponse.Embeddings) > 0 && len(embedResponse.Embeddings[0]) > 0 {
		return e.checkDimensions(embedResponse.Embeddings[0])
	}

	var legacyResponse struct {
		Embedding []float32 `json:"embedding"`
	}
	legacyErr := e.postJSON(ctx, strings.TrimRight(e.BaseURL, "/")+"/api/embeddings", map[string]any{
		"model":  e.Model,
		"prompt": text,
	}, &legacyResponse)
	if legacyErr != nil {
		if err != nil {
			return nil, fmt.Errorf("ollama embed failed: %v; legacy endpoint failed: %w", err, legacyErr)
		}
		return nil, legacyErr
	}
	if len(legacyResponse.Embedding) == 0 {
		return nil, errors.New("ollama returned no embedding")
	}
	return e.checkDimensions(legacyResponse.Embedding)
}

func (e HTTPEmbedder) postJSON(ctx context.Context, endpoint string, payload any, out any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	if e.APIKey != "" {
		req.Header.Set("authorization", "Bearer "+e.APIKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("embedding provider returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return err
	}
	return nil
}

func (e HTTPEmbedder) checkDimensions(vector []float32) ([]float32, error) {
	if e.ExpectedDimensions > 0 && len(vector) != e.ExpectedDimensions {
		return nil, fmt.Errorf("embedding dimension mismatch: got %d, want %d", len(vector), e.ExpectedDimensions)
	}
	return vector, nil
}

func openAIEmbeddingEndpoint(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/embeddings") {
		return baseURL
	}
	return baseURL + "/embeddings"
}
