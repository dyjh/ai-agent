package tests

import (
	"context"
	"testing"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
)

func TestPolicyEngine(t *testing.T) {
	engine := agent.NewPolicyEngine(config.PolicyConfig{
		MinConfidenceForAutoExecute: 0.85,
	})

	testCases := []struct {
		name         string
		inference    core.EffectInferenceResult
		wantApproval bool
	}{
		{
			name: "read auto executes",
			inference: core.EffectInferenceResult{
				Effects:       []string{"read", "process.read"},
				RiskLevel:     "read",
				Confidence:    0.95,
				ReasonSummary: "process query",
			},
			wantApproval: false,
		},
		{
			name: "write requires approval",
			inference: core.EffectInferenceResult{
				Effects:       []string{"fs.write", "code.modify"},
				RiskLevel:     "write",
				Confidence:    0.99,
				ReasonSummary: "write",
			},
			wantApproval: true,
		},
		{
			name: "sensitive read requires approval",
			inference: core.EffectInferenceResult{
				Effects:       []string{"sensitive_read", "env_file.read"},
				RiskLevel:     "sensitive",
				Sensitive:     true,
				Confidence:    0.95,
				ReasonSummary: "sensitive",
			},
			wantApproval: true,
		},
		{
			name: "unknown requires approval",
			inference: core.EffectInferenceResult{
				Effects:       []string{"unknown.effect"},
				RiskLevel:     "unknown",
				Confidence:    0.9,
				ReasonSummary: "unknown",
			},
			wantApproval: true,
		},
		{
			name: "low confidence requires approval",
			inference: core.EffectInferenceResult{
				Effects:       []string{"read", "system.read"},
				RiskLevel:     "read",
				Confidence:    0.3,
				ReasonSummary: "low confidence",
			},
			wantApproval: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := engine.Decide(context.Background(), core.ToolProposal{}, tc.inference)
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if decision.RequiresApproval != tc.wantApproval {
				t.Fatalf("RequiresApproval = %v, want %v", decision.RequiresApproval, tc.wantApproval)
			}
		})
	}
}
