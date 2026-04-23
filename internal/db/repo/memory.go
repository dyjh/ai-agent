package repo

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"local-agent/internal/core"
)

// NewMemoryStore creates an in-memory repository set for tests and local fallback.
func NewMemoryStore() *Store {
	backend := &memoryBackend{
		conversations: map[string]core.Conversation{},
		messages:      map[string][]core.Message{},
		rollups:       map[string]core.ConversationUsageRollup{},
	}
	return &Store{
		Conversations: backend,
		Messages:      backend,
		Usage:         backend,
		AgentEvents:   backend,
	}
}

type memoryBackend struct {
	mu            sync.RWMutex
	conversations map[string]core.Conversation
	messages      map[string][]core.Message
	events        []core.AgentEvent
	usages        []core.MessageUsage
	rollups       map[string]core.ConversationUsageRollup
}

func (m *memoryBackend) CreateConversation(_ context.Context, conversation core.Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conversations[conversation.ID] = conversation
	return nil
}

func (m *memoryBackend) List(_ context.Context) ([]core.Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]core.Conversation, 0, len(m.conversations))
	for _, item := range m.conversations {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items, nil
}

func (m *memoryBackend) Get(_ context.Context, id string) (*core.Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.conversations[id]
	if !ok {
		return nil, errors.New("conversation not found")
	}
	cp := item
	return &cp, nil
}

func (m *memoryBackend) Touch(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.conversations[id]
	if !ok {
		return errors.New("conversation not found")
	}
	item.UpdatedAt = time.Now().UTC()
	m.conversations[id] = item
	return nil
}

func (m *memoryBackend) CreateMessage(_ context.Context, message core.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[message.ConversationID] = append(m.messages[message.ConversationID], message)
	return nil
}

func (m *memoryBackend) CreateUsage(_ context.Context, usage core.MessageUsage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usages = append(m.usages, usage)
	return nil
}

func (m *memoryBackend) CreateEvent(_ context.Context, event core.AgentEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *memoryBackend) ListByConversation(_ context.Context, conversationID string) ([]core.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := append([]core.Message(nil), m.messages[conversationID]...)
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (m *memoryBackend) IncrementRollup(_ context.Context, conversationID string, usage core.MessageUsage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rollup := m.rollups[conversationID]
	rollup.ConversationID = conversationID
	rollup.TotalInputTokens += int64(usage.InputTokens)
	rollup.TotalOutputTokens += int64(usage.OutputTokens)
	rollup.TotalTokens += int64(usage.TotalTokens)
	rollup.TotalMessages++
	rollup.UpdatedAt = time.Now().UTC()
	m.rollups[conversationID] = rollup
	return nil
}
