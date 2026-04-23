package tests

import (
	"testing"

	"local-agent/internal/security"
)

func TestRedactMapMasksSensitiveKeys(t *testing.T) {
	payload := map[string]any{
		"input_snapshot": map[string]any{
			"api_key": "sk-secret",
			"nested": map[string]any{
				"password": "plain",
			},
			"safe": "value",
		},
	}

	redacted := security.RedactMap(payload)
	snapshot := redacted["input_snapshot"].(map[string]any)
	if snapshot["api_key"] != "[REDACTED]" {
		t.Fatalf("api_key was not redacted: %v", snapshot["api_key"])
	}
	nested := snapshot["nested"].(map[string]any)
	if nested["password"] != "[REDACTED]" {
		t.Fatalf("password was not redacted: %v", nested["password"])
	}
	if snapshot["safe"] != "value" {
		t.Fatalf("safe value changed: %v", snapshot["safe"])
	}
}
