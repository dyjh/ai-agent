package einoapp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"local-agent/internal/config"
)

// ChatModel is the minimal Eino chat surface used by the runtime.
type ChatModel interface {
	Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error)
}

// NewChatModel creates a provider-backed Eino model or a mock model.
func NewChatModel(ctx context.Context, cfg config.LLMConfig) (ChatModel, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = config.ProviderMock
	}
	switch provider {
	case config.ProviderMock:
		return MockChatModel{}, nil
	case config.ProviderOpenAICompatible:
		if cfg.APIKey == "" || cfg.Model == "" {
			return MockChatModel{}, nil
		}
		return openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
		})
	case config.ProviderOllama:
		if strings.TrimSpace(cfg.Model) == "" {
			return nil, fmt.Errorf("ollama model is required")
		}
		return openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:  "ollama",
			BaseURL: ollamaOpenAIBaseURL(cfg.BaseURL),
			Model:   cfg.Model,
		})
	default:
		return nil, fmt.Errorf("unsupported llm provider: %s", cfg.Provider)
	}
}

func ollamaOpenAIBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	}
	u, err := url.Parse(baseURL)
	if err == nil && strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/v1") {
		return baseURL
	}
	return baseURL + "/v1"
}

// MockChatModel is used when no real provider is configured.
type MockChatModel struct{}

// Generate returns a deterministic assistant response for tests and local smoke runs.
func (MockChatModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	last := ""
	if len(messages) > 0 {
		last = messages[len(messages)-1].Content
	}
	return &schema.Message{
		Role:    schema.Assistant,
		Content: "Mock response: " + strings.TrimSpace(last),
	}, nil
}
