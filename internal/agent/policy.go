package agent

import (
	"context"
	"strings"

	"local-agent/internal/config"
	"local-agent/internal/core"
	"local-agent/internal/tools/ops"
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
func (p *PolicyEngine) Decide(_ context.Context, proposal core.ToolProposal, inference core.EffectInferenceResult) (core.PolicyDecision, error) {
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
		if strings.HasPrefix(proposal.Tool, "ops.") {
			decision.ApprovalPayload["operation"] = proposal.Tool
			decision.ApprovalPayload["impact"] = opsApprovalImpact(proposal)
			decision.ApprovalPayload["rollback_plan"] = ops.RollbackForTool(proposal.Tool, proposal.Input)
			if proposal.Tool == "ops.k8s.apply" {
				decision.ApprovalPayload["manifest_summary"] = ops.ManifestSummary(proposal.Input)
			}
		}
	}

	return decision, nil
}

func opsApprovalImpact(proposal core.ToolProposal) map[string]any {
	impact := map[string]any{}
	for _, key := range []string{"host_id", "service", "service_name", "container", "container_id", "resource", "name", "namespace", "target"} {
		if value, ok := proposal.Input[key]; ok && value != "" {
			impact[key] = value
		}
	}
	switch {
	case strings.HasPrefix(proposal.Tool, "ops.docker."):
		impact["scope"] = "docker container lifecycle"
	case strings.HasPrefix(proposal.Tool, "ops.k8s."):
		impact["scope"] = "kubernetes cluster resource"
	case strings.HasPrefix(proposal.Tool, "ops.ssh."):
		impact["scope"] = "remote ssh host"
	case strings.HasPrefix(proposal.Tool, "ops.local."):
		impact["scope"] = "local host"
	}
	return impact
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
