package ws

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"nhooyr.io/websocket"

	"local-agent/internal/agent"
	"local-agent/internal/api/handlers"
	"local-agent/internal/core"
)

// ChatHandler serves the WebSocket chat endpoint.
type ChatHandler struct {
	Deps handlers.Dependencies
}

// NewChatHandler creates a WS chat handler.
func NewChatHandler(deps handlers.Dependencies) *ChatHandler {
	return &ChatHandler{Deps: deps}
}

// ServeHTTP upgrades and serves the bidirectional chat protocol.
func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx := r.Context()
	sink := SocketSink{Conn: conn, Ctx: ctx}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		var message struct {
			Type       string `json:"type"`
			Content    string `json:"content"`
			ApprovalID string `json:"approval_id"`
			Approved   bool   `json:"approved"`
		}
		if err := json.Unmarshal(data, &message); err != nil {
			_ = conn.Close(websocket.StatusInvalidFramePayloadData, err.Error())
			return
		}

		switch message.Type {
		case "user.message":
			_, err = h.Deps.Runtime.Run(ctx, agent.RunRequest{
				ConversationID: chi.URLParam(r, "conversation_id"),
				Content:        message.Content,
			}, sink)
		case "approval.respond":
			if message.Approved {
				if _, err = h.Deps.Approvals.Approve(message.ApprovalID); err == nil {
					var result *core.ToolResult
					result, err = h.Deps.Router.ExecuteApproved(context.Background(), message.ApprovalID)
					if err == nil && result != nil {
						sink.Emit(core.Event{
							Type:       "tool.output",
							ApprovalID: message.ApprovalID,
							Payload:    result.Output,
						})
						sink.Emit(core.Event{
							Type:       "tool.completed",
							ApprovalID: message.ApprovalID,
							ToolCallID: result.ToolCallID,
						})
						sink.Emit(core.Event{
							Type:    "assistant.message",
							Content: "审批通过后的工具执行已完成。",
						})
					}
				}
			} else {
				_, err = h.Deps.Approvals.Reject(message.ApprovalID, "rejected from websocket")
				if err == nil {
					sink.Emit(core.Event{
						Type:       "approval.rejected",
						ApprovalID: message.ApprovalID,
					})
				}
			}
		}
		if err != nil {
			_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"error","content":"`+err.Error()+`"}`))
		}
	}
}
