package events

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/security"
)

// JSONLWriter writes run and audit logs to local files.
type JSONLWriter struct {
	runRoot   string
	auditRoot string
	mu        sync.Mutex
}

// NewJSONLWriter constructs a JSONL writer.
func NewJSONLWriter(runRoot, auditRoot string) *JSONLWriter {
	return &JSONLWriter{
		runRoot:   runRoot,
		auditRoot: auditRoot,
	}
}

// WriteRun appends an event to the run-scoped JSONL file and the audit file.
func (w *JSONLWriter) WriteRun(event core.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	event.Payload = security.RedactMap(event.Payload)
	event.Content = security.RedactString(event.Content)

	day := event.CreatedAt.Format("2006-01-02")
	runDir := filepath.Join(w.runRoot, day)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(w.auditRoot, 0o755); err != nil {
		return err
	}

	runPath := filepath.Join(runDir, "run_"+event.RunID+".jsonl")
	auditPath := filepath.Join(w.auditRoot, day+".jsonl")

	if err := appendJSONL(runPath, event); err != nil {
		return err
	}
	return appendJSONL(auditPath, event)
}

func appendJSONL(path string, event core.Event) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}

	_, err = file.Write(append(raw, '\n'))
	return err
}
