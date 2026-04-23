package repo

import (
	"context"

	"local-agent/internal/core"
)

// ConversationRepository persists conversations.
type ConversationRepository interface {
	CreateConversation(ctx context.Context, conversation core.Conversation) error
	List(ctx context.Context) ([]core.Conversation, error)
	Get(ctx context.Context, id string) (*core.Conversation, error)
	Touch(ctx context.Context, id string) error
}

// MessageRepository persists messages.
type MessageRepository interface {
	CreateMessage(ctx context.Context, message core.Message) error
	ListByConversation(ctx context.Context, conversationID string) ([]core.Message, error)
}

// UsageRepository persists token usage.
type UsageRepository interface {
	CreateUsage(ctx context.Context, usage core.MessageUsage) error
	IncrementRollup(ctx context.Context, conversationID string, usage core.MessageUsage) error
}

// AgentEventRepository persists event rows.
type AgentEventRepository interface {
	CreateEvent(ctx context.Context, event core.AgentEvent) error
}

// Store groups the persistence adapters.
type Store struct {
	Conversations ConversationRepository
	Messages      MessageRepository
	Usage         UsageRepository
	AgentEvents   AgentEventRepository
}
