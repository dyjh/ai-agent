package ws

import (
	"context"
	"encoding/json"

	"nhooyr.io/websocket"

	"local-agent/internal/core"
)

// SocketSink streams runtime events over WebSocket.
type SocketSink struct {
	Conn *websocket.Conn
	Ctx  context.Context
}

// Emit writes one event to the socket.
func (s SocketSink) Emit(event core.Event) {
	raw, _ := json.Marshal(event)
	_ = s.Conn.Write(s.Ctx, websocket.MessageText, raw)
}
