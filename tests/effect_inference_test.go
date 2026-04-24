package tests

import (
	"context"
	"testing"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
)

func TestEffectInference(t *testing.T) {
	inferrer := agent.NewEffectInferrer(config.PolicyConfig{
		SensitivePaths: []string{".env"},
	})

	testCases := []struct {
		name       string
		proposal   core.ToolProposal
		wantEffect string
		wantRisk   string
	}{
		{
			name: "process read",
			proposal: core.ToolProposal{
				Tool: "shell.exec",
				Input: map[string]any{
					"command": "ps -eo pid,pcpu,comm --sort=-pcpu | head -n 5",
				},
			},
			wantEffect: "process.read",
			wantRisk:   "read",
		},
		{
			name: "env read is sensitive",
			proposal: core.ToolProposal{
				Tool: "shell.exec",
				Input: map[string]any{
					"command": "cat .env",
				},
			},
			wantEffect: "sensitive_read",
			wantRisk:   "sensitive",
		},
		{
			name: "apply patch is write",
			proposal: core.ToolProposal{
				Tool: "code.apply_patch",
				Input: map[string]any{
					"path": "foo.go",
				},
			},
			wantEffect: "code.modify",
			wantRisk:   "write",
		},
		{
			name: "code read sensitive path",
			proposal: core.ToolProposal{
				Tool: "code.read_file",
				Input: map[string]any{
					"path": ".env",
				},
			},
			wantEffect: "sensitive_read",
			wantRisk:   "sensitive",
		},
		{
			name: "code search include sensitive",
			proposal: core.ToolProposal{
				Tool: "code.search_text",
				Input: map[string]any{
					"path":              ".",
					"query":             "TOKEN",
					"include_sensitive": true,
				},
			},
			wantEffect: "sensitive_read",
			wantRisk:   "sensitive",
		},
		{
			name: "unknown tool",
			proposal: core.ToolProposal{
				Tool: "mystery.tool",
			},
			wantEffect: "unknown.effect",
			wantRisk:   "unknown",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := inferrer.Infer(context.Background(), tc.proposal)
			if err != nil {
				t.Fatalf("Infer() error = %v", err)
			}
			if !containsString(got.Effects, tc.wantEffect) {
				t.Fatalf("effects = %v, want %s", got.Effects, tc.wantEffect)
			}
			if got.RiskLevel != tc.wantRisk {
				t.Fatalf("risk = %s, want %s", got.RiskLevel, tc.wantRisk)
			}
		})
	}
}
