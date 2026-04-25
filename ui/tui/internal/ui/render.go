package ui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"local-agent-ui-tui/internal/client"
)

// RenderEvent returns a compact terminal line for an event.
func RenderEvent(event client.Event) string {
	switch event.Type {
	case "assistant.delta":
		return event.Content
	case "assistant.message":
		return "\nassistant: " + event.Content
	case "approval.requested":
		id := event.ApprovalID
		if id == "" && event.Payload != nil {
			if approval, ok := event.Payload["approval"].(map[string]any); ok {
				id, _ = approval["id"].(string)
			}
		}
		return fmt.Sprintf("\napproval requested: %s", id)
	case "tool.output":
		return "\ntool output:\n" + RenderJSON(event.Payload)
	case "run.started", "run.completed", "run.failed", "run.cancelled":
		return fmt.Sprintf("\n%s %s", event.Type, event.RunID)
	default:
		if event.Content != "" {
			return fmt.Sprintf("\n%s: %s", event.Type, event.Content)
		}
		return "\n" + event.Type
	}
}

func RenderJSON(value any) string {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprint(value)
	}
	return Redact(string(raw))
}

func Redact(input string) string {
	pattern := regexp.MustCompile(`(?i)(["']?(?:api[_-]?key|token|secret|password|cookie|session)["']?\s*[:=]\s*)("[^"\n]*"|[^,\n}\s]+)`)
	out := pattern.ReplaceAllString(input, `${1}"[REDACTED]"`)
	bearer := regexp.MustCompile(`(?i)bearer\s+[a-z0-9._~+/-]+=*`)
	return bearer.ReplaceAllString(out, "Bearer [REDACTED]")
}

// PreviewDiff returns a small diff summary suitable for terminal display.
func PreviewDiff(diff string) string {
	files := 0
	added := 0
	deleted := 0
	conflict := false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			files++
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			added++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			deleted++
		case strings.Contains(line, "<<<<<<<") || strings.Contains(line, "=======") || strings.Contains(line, ">>>>>>>"):
			conflict = true
		}
	}
	if files == 0 && diff != "" {
		files = 1
	}
	status := "ok"
	if conflict {
		status = "conflict-markers"
	}
	return fmt.Sprintf("diff files=%d added=%d deleted=%d status=%s", files, added, deleted, status)
}
