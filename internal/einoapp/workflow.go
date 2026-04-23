package einoapp

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/cloudwego/eino/schema"

	"local-agent/internal/core"
)

// AgentInput is the normalized input for both workflow runs and direct Eino model calls.
// Workflow callers use ConversationID/Message; Runner callers use Messages.
type AgentInput struct {
	ConversationID string            `json:"conversation_id,omitempty"`
	Message        string            `json:"message,omitempty"`
	Metadata       map[string]any    `json:"metadata,omitempty"`
	Messages       []*schema.Message `json:"messages,omitempty"`
}

// AgentEvent is the workflow event shape emitted to HTTP, WebSocket, CLI, JSONL, and callbacks.
type AgentEvent = core.Event

// AgentEventStream is a pull-based stream for workflow start/resume events.
type AgentEventStream interface {
	Next(ctx context.Context) (*AgentEvent, error)
}

// AgentWorkflow is the stable facade for new runs and human-in-the-loop resumes.
type AgentWorkflow interface {
	Start(ctx context.Context, input AgentInput) (AgentEventStream, error)
	Resume(ctx context.Context, runID string, approvalID string, approved bool) (AgentEventStream, error)
}

// SliceEventStream is a deterministic in-memory event stream used by HTTP and tests.
type SliceEventStream struct {
	mu     sync.Mutex
	events []AgentEvent
	next   int
}

// NewSliceEventStream constructs an AgentEventStream from a completed event slice.
func NewSliceEventStream(events []AgentEvent) *SliceEventStream {
	cp := append([]AgentEvent(nil), events...)
	return &SliceEventStream{events: cp}
}

// Next returns the next event or io.EOF when the stream is exhausted.
func (s *SliceEventStream) Next(ctx context.Context) (*AgentEvent, error) {
	if s == nil {
		return nil, io.EOF
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.next >= len(s.events) {
		return nil, io.EOF
	}
	event := s.events[s.next]
	s.next++
	return &event, nil
}

// DrainEventStream reads all currently available stream events.
func DrainEventStream(ctx context.Context, stream AgentEventStream) ([]AgentEvent, error) {
	if stream == nil {
		return nil, nil
	}
	var events []AgentEvent
	for {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			return events, nil
		}
		if err != nil {
			return events, err
		}
		events = append(events, *event)
	}
}
