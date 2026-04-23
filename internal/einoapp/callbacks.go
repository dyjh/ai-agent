package einoapp

import (
	"time"

	"local-agent/internal/core"
	"local-agent/internal/events"
)

// Callbacks records runtime milestones to the JSONL writer.
type Callbacks struct {
	Writer *events.JSONLWriter
}

// OnEvent records an event if the writer exists.
func (c Callbacks) OnEvent(event core.Event) {
	if c.Writer == nil {
		return
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	_ = c.Writer.WriteRun(event)
}
