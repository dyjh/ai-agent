package einoapp

import (
	"context"
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

// NewChatModel creates either a real Eino OpenAI-compatible model or a mock model.
func NewChatModel(ctx context.Context, cfg config.LLMConfig) (ChatModel, error) {
	if cfg.APIKey == "" || cfg.Model == "" {
		return MockChatModel{}, nil
	}
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
	})
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
