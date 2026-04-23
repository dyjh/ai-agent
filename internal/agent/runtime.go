package agent

import (
	"context"
	"fmt"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/db/repo"
	"local-agent/internal/einoapp"
	"local-agent/internal/ids"
	"local-agent/internal/timeutil"
	"local-agent/internal/tools"
)

// Runtime executes the end-to-end conversation loop.
type Runtime struct {
	Store          *repo.Store
	Planner        Planner
	Runner         einoapp.Runner
	ContextBuilder *ContextBuilder
	Router         tools.Router
}

// Run executes one user message.
func (r *Runtime) Run(ctx context.Context, request RunRequest, sink EventSink) (*RunResponse, error) {
	runID := ids.New("run")
	r.emit(sink, core.Event{
		Type:           "run.started",
		RunID:          runID,
		ConversationID: request.ConversationID,
		CreatedAt:      timeutil.NowUTC(),
	})

	userMessage := core.Message{
		ID:             ids.New("msg"),
		ConversationID: request.ConversationID,
		Role:           "user",
		Content:        request.Content,
		CreatedAt:      time.Now().UTC(),
	}
	if r.Store != nil && r.Store.Messages != nil {
		if err := r.Store.Messages.CreateMessage(ctx, userMessage); err != nil {
			return nil, err
		}
	}
	if r.Store != nil && r.Store.Conversations != nil && request.ConversationID != "" {
		_ = r.Store.Conversations.Touch(ctx, request.ConversationID)
	}

	plan, err := r.Planner.Plan(ctx, request.Content)
	if err != nil {
		return nil, err
	}
	if plan.Preamble != "" {
		r.emit(sink, core.Event{
			Type:           "assistant.delta",
			RunID:          runID,
			ConversationID: request.ConversationID,
			Content:        plan.Preamble,
			CreatedAt:      time.Now().UTC(),
		})
	}

	response := &RunResponse{RunID: runID}

	if plan.ToolProposal != nil {
		outcome, err := r.Router.Propose(ctx, runID, request.ConversationID, *plan.ToolProposal)
		if err != nil {
			return nil, err
		}
		if outcome.Approval != nil {
			response.Approval = outcome.Approval
			r.emit(sink, core.Event{
				Type:           "approval.requested",
				RunID:          runID,
				ConversationID: request.ConversationID,
				ApprovalID:     outcome.Approval.ID,
				RiskLevel:      outcome.Inference.RiskLevel,
				Payload: map[string]any{
					"summary":        outcome.Approval.Summary,
					"input_snapshot": outcome.Approval.InputSnapshot,
				},
				CreatedAt: time.Now().UTC(),
			})
			assistantMessage, err := r.persistAssistant(ctx, request.ConversationID, "该操作需要审批后才能执行。", 1, runID, sink)
			if err != nil {
				return nil, err
			}
			response.AssistantMessage = assistantMessage
			r.emit(sink, core.Event{Type: "run.completed", RunID: runID, ConversationID: request.ConversationID, CreatedAt: time.Now().UTC()})
			return response, nil
		}
		response.ToolResult = outcome.Result
		summary := summarizeToolResult(outcome.Result)
		assistantMessage, err := r.persistAssistant(ctx, request.ConversationID, summary, 1, runID, sink)
		if err != nil {
			return nil, err
		}
		response.AssistantMessage = assistantMessage
		r.emit(sink, core.Event{
			Type:           "tool.output",
			RunID:          runID,
			ConversationID: request.ConversationID,
			ToolCallID:     outcome.Result.ToolCallID,
			Payload:        outcome.Result.Output,
			CreatedAt:      time.Now().UTC(),
		})
		r.emit(sink, core.Event{Type: "run.completed", RunID: runID, ConversationID: request.ConversationID, CreatedAt: time.Now().UTC()})
		return response, nil
	}

	messages, err := r.ContextBuilder.Build(ctx, request.ConversationID, request.Content)
	if err != nil {
		return nil, err
	}
	modelMessage, err := r.Runner.Run(ctx, einoapp.AgentInput{Messages: messages})
	if err != nil {
		return nil, err
	}
	assistantMessage, err := r.persistAssistant(ctx, request.ConversationID, modelMessage.Content, 0, runID, sink)
	if err != nil {
		return nil, err
	}
	response.AssistantMessage = assistantMessage
	r.emit(sink, core.Event{Type: "run.completed", RunID: runID, ConversationID: request.ConversationID, CreatedAt: time.Now().UTC()})
	return response, nil
}

func (r *Runtime) persistAssistant(ctx context.Context, conversationID, content string, toolCallCount int, runID string, sink EventSink) (*core.Message, error) {
	message := &core.Message{
		ID:             ids.New("msg"),
		ConversationID: conversationID,
		Role:           "assistant",
		Content:        content,
		CreatedAt:      time.Now().UTC(),
	}
	if r.Store != nil && r.Store.Messages != nil {
		if err := r.Store.Messages.CreateMessage(ctx, *message); err != nil {
			return nil, err
		}
	}
	if r.Store != nil && r.Store.Usage != nil {
		usage := core.MessageUsage{
			ID:             ids.New("usage"),
			MessageID:      message.ID,
			ConversationID: conversationID,
			InputTokens:    estimateTokens(content),
			OutputTokens:   estimateTokens(content),
			TotalTokens:    estimateTokens(content) * 2,
			ToolCallCount:  toolCallCount,
			CreatedAt:      time.Now().UTC(),
		}
		if err := r.Store.Usage.CreateUsage(ctx, usage); err != nil {
			return nil, err
		}
		_ = r.Store.Usage.IncrementRollup(ctx, conversationID, usage)
	}
	r.emit(sink, core.Event{
		Type:           "assistant.message",
		RunID:          runID,
		ConversationID: conversationID,
		Content:        content,
		CreatedAt:      time.Now().UTC(),
	})
	return message, nil
}

func (r *Runtime) emit(sink EventSink, event core.Event) {
	if sink == nil {
		return
	}
	sink.Emit(event)
}

func summarizeToolResult(result *core.ToolResult) string {
	if result == nil {
		return "工具执行完成。"
	}
	if stdout, ok := result.Output["stdout"].(string); ok && stdout != "" {
		return fmt.Sprintf("工具执行完成，输出如下：\n%s", stdout)
	}
	if status, ok := result.Output["status"].(string); ok && status != "" {
		return "工具执行完成，状态为 " + status + "。"
	}
	return "工具执行完成。"
}

func estimateTokens(content string) int {
	if content == "" {
		return 0
	}
	return len(content)/4 + 1
}
