package agent

import (
	"context"
	"strings"

	"local-agent/internal/config"
	"local-agent/internal/core"
)

// PolicyEngine converts inferred effects into an execution decision.
type PolicyEngine struct {
	cfg config.PolicyConfig
}

// NewPolicyEngine constructs a policy engine.
func NewPolicyEngine(cfg config.PolicyConfig) *PolicyEngine {
	return &PolicyEngine{cfg: cfg}
}

// Decide decides whether a proposal may execute automatically.
func (p *PolicyEngine) Decide(_ context.Context, _ core.ToolProposal, inference core.EffectInferenceResult) (core.PolicyDecision, error) {
	decision := core.PolicyDecision{
		Allowed:          true,
		RequiresApproval: false,
		RiskLevel:        inference.RiskLevel,
		Reason:           inference.ReasonSummary,
	}

	if inference.Confidence < p.cfg.MinConfidenceForAutoExecute {
		decision.RequiresApproval = true
		decision.Reason = "confidence below auto-execute threshold"
	}

	if inference.Sensitive || inference.ApprovalRequired {
		decision.RequiresApproval = true
	}

	for _, effect := range inference.Effects {
		if effect == "unknown.effect" {
			decision.RequiresApproval = true
			decision.RiskLevel = "unknown"
			decision.Reason = "unknown effect requires approval"
			break
		}
		if effectNeedsApproval(effect) {
			decision.RequiresApproval = true
		}
	}

	if decision.RequiresApproval {
		decision.ApprovalPayload = map[string]any{
			"risk_level": inference.RiskLevel,
			"effects":    inference.Effects,
		}
	}

	return decision, nil
}

func effectNeedsApproval(effect string) bool {
	if strings.Contains(effect, "sensitive") || strings.Contains(effect, "env_file") {
		return true
	}
	if strings.Contains(effect, "write") || strings.Contains(effect, "modify") || strings.Contains(effect, "delete") || strings.Contains(effect, "install") {
		return true
	}
	if effect == "network.post" || effect == "network.put" || effect == "network.delete" {
		return true
	}
	if strings.Contains(effect, "restart") || strings.Contains(effect, "stop") || strings.Contains(effect, "kill") || strings.Contains(effect, "escalate") {
		return true
	}
	return false
}
