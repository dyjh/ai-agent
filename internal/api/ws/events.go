package ws

import (
	"context"
	"encoding/json"

	"nhooyr.io/websocket"

	"local-agent/internal/core"
	"local-agent/internal/security"
)

// SocketSink streams runtime events over WebSocket.
type SocketSink struct {
	Conn *websocket.Conn
	Ctx  context.Context
}

// Emit writes one event to the socket.
func (s SocketSink) Emit(event core.Event) {
	event.Payload = security.RedactMap(event.Payload)
	event.Content = security.RedactString(event.Content)
	raw, _ := json.Marshal(event)
	_ = s.Conn.Write(s.Ctx, websocket.MessageText, raw)
}
