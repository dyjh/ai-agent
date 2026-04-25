package tests

import (
	"strings"
	"testing"

	"local-agent-ui-tui/internal/client"
	"local-agent-ui-tui/internal/ui"
)

func TestRenderApprovalEvent(t *testing.T) {
	out := ui.RenderEvent(client.Event{Type: "approval.requested", ApprovalID: "appr_123"})
	if !strings.Contains(out, "appr_123") {
		t.Fatalf("expected approval id, got %q", out)
	}
}

func TestDiffPreview(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n@@\n-old\n+new\n"
	out := ui.PreviewDiff(diff)
	if !strings.Contains(out, "files=1") || !strings.Contains(out, "added=1") || !strings.Contains(out, "deleted=1") {
		t.Fatalf("unexpected diff preview: %s", out)
	}
}

func TestRenderJSONRedacts(t *testing.T) {
	out := ui.RenderJSON(map[string]any{"api_key": "secret-value"})
	if strings.Contains(out, "secret-value") {
		t.Fatalf("expected redacted output, got %s", out)
	}
}
