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
		runs:          map[string]core.AgentRunRecord{},
		runSteps:      map[string][]core.AgentRunStepRecord{},
	}
	return &Store{
		Conversations: backend,
		Messages:      backend,
		Usage:         backend,
		AgentEvents:   backend,
		AgentRuns:     backend,
		AgentRunSteps: backend,
	}
}

type memoryBackend struct {
	mu            sync.RWMutex
	conversations map[string]core.Conversation
	messages      map[string][]core.Message
	events        []core.AgentEvent
	usages        []core.MessageUsage
	rollups       map[string]core.ConversationUsageRollup
	runs          map[string]core.AgentRunRecord
	runSteps      map[string][]core.AgentRunStepRecord
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

func (m *memoryBackend) UpsertRun(_ context.Context, run core.AgentRunRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[run.RunID] = cloneRunRecord(run)
	return nil
}

func (m *memoryBackend) GetRun(_ context.Context, runID string) (*core.AgentRunRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.runs[runID]
	if !ok {
		return nil, errors.New("run not found")
	}
	cp := cloneRunRecord(record)
	return &cp, nil
}

func (m *memoryBackend) ListRunsByStatus(_ context.Context, statuses []string, limit int) ([]core.AgentRunRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	allowed := make(map[string]struct{}, len(statuses))
	for _, status := range statuses {
		allowed[status] = struct{}{}
	}
	items := make([]core.AgentRunRecord, 0, len(m.runs))
	for _, item := range m.runs {
		if len(allowed) > 0 {
			if _, ok := allowed[item.Status]; !ok {
				continue
			}
		}
		items = append(items, cloneRunRecord(item))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (m *memoryBackend) DeleteRun(_ context.Context, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runs, runID)
	delete(m.runSteps, runID)
	return nil
}

func (m *memoryBackend) UpsertStep(_ context.Context, step core.AgentRunStepRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := append([]core.AgentRunStepRecord(nil), m.runSteps[step.RunID]...)
	replaced := false
	for idx := range items {
		if items[idx].StepID == step.StepID {
			items[idx] = cloneStepRecord(step)
			replaced = true
			break
		}
	}
	if !replaced {
		items = append(items, cloneStepRecord(step))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].StepIndex < items[j].StepIndex
	})
	m.runSteps[step.RunID] = items
	return nil
}

func (m *memoryBackend) ListStepsByRun(_ context.Context, runID string) ([]core.AgentRunStepRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := append([]core.AgentRunStepRecord(nil), m.runSteps[runID]...)
	out := make([]core.AgentRunStepRecord, 0, len(items))
	for _, item := range items {
		out = append(out, cloneStepRecord(item))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StepIndex < out[j].StepIndex
	})
	return out, nil
}

func (m *memoryBackend) DeleteStepsByRun(_ context.Context, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runSteps, runID)
	return nil
}

func cloneRunRecord(record core.AgentRunRecord) core.AgentRunRecord {
	cp := record
	cp.StateJSON = core.CloneMap(record.StateJSON)
	return cp
}

func cloneStepRecord(record core.AgentRunStepRecord) core.AgentRunStepRecord {
	cp := record
	cp.ProposalJSON = core.CloneMap(record.ProposalJSON)
	cp.InferenceJSON = core.CloneMap(record.InferenceJSON)
	cp.PolicyJSON = core.CloneMap(record.PolicyJSON)
	cp.ApprovalJSON = core.CloneMap(record.ApprovalJSON)
	cp.ToolResultJSON = core.CloneMap(record.ToolResultJSON)
	return cp
}
