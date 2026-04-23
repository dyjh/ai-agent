package agent

import "local-agent/internal/core"

// RunRequest is the runtime input.
type RunRequest struct {
	ConversationID string `json:"conversation_id"`
	Content        string `json:"content"`
}

// RunResponse is the runtime result.
type RunResponse struct {
	RunID            string               `json:"run_id"`
	AssistantMessage *core.Message        `json:"assistant_message,omitempty"`
	Approval         *core.ApprovalRecord `json:"approval,omitempty"`
	ToolResult       *core.ToolResult     `json:"tool_result,omitempty"`
}

// EventSink receives runtime events for HTTP/WS streaming.
type EventSink interface {
	Emit(event core.Event)
}
