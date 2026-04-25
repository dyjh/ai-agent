package agent

import (
	"context"
	"strings"

	"local-agent/internal/config"
	"local-agent/internal/core"
	"local-agent/internal/security"
	"local-agent/internal/tools/ops"
)

// PolicyEngine converts inferred effects into an execution decision.
type PolicyEngine struct {
	cfg config.PolicyConfig
}

// NewPolicyEngine constructs a policy engine.
func NewPolicyEngine(cfg config.PolicyConfig) *PolicyEngine {
	return &PolicyEngine{cfg: config.NormalizePolicy(cfg)}
}

// Decide decides whether a proposal may execute automatically.
func (p *PolicyEngine) Decide(_ context.Context, proposal core.ToolProposal, inference core.EffectInferenceResult) (core.PolicyDecision, error) {
	profile := p.resolveProfile(proposal)
	decision := core.PolicyDecision{
		Allowed:          true,
		RequiresApproval: false,
		RiskLevel:        inference.RiskLevel,
		Reason:           inference.ReasonSummary,
		PolicyProfile:    profile.Name,
	}
	threshold := profile.MinConfidenceForAutoExecute
	if threshold <= 0 {
		threshold = p.cfg.MinConfidenceForAutoExecute
	}

	if inference.Confidence < threshold {
		decision.RequiresApproval = true
		decision.Reason = "confidence below auto-execute threshold"
	}

	if inference.Sensitive || inference.ApprovalRequired {
		decision.RequiresApproval = true
	}

	for _, effect := range inference.Effects {
		if profileDeniesEffect(profile, effect) {
			decision.Allowed = false
			decision.RequiresApproval = false
			decision.RiskLevel = riskOrDefault(inference.RiskLevel, "danger")
			decision.Reason = "effect denied by policy profile"
			break
		}
		if effect == "unknown.effect" {
			decision.RequiresApproval = true
			decision.RiskLevel = "unknown"
			decision.Reason = "unknown effect requires approval"
			break
		}
		if profileRequiresApproval(profile, effect) || effectNeedsApproval(effect) {
			decision.RequiresApproval = true
		}
	}

	if decision.Allowed && !profile.AutoExecuteReadonly && !decision.RequiresApproval {
		decision.RequiresApproval = true
		decision.Reason = "policy profile disables read-only auto execution"
	}

	if decision.RequiresApproval {
		decision.ApprovalPayload = map[string]any{
			"risk_level":     inference.RiskLevel,
			"effects":        inference.Effects,
			"policy_profile": profile.Name,
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
	if decision.ApprovalPayload != nil {
		decision.ApprovalPayload = security.RedactMap(decision.ApprovalPayload)
	}
	decision.RiskTrace = buildRiskTrace(proposal, inference, decision, profile)
	if decision.ApprovalPayload != nil {
		decision.ApprovalPayload["risk_trace"] = decision.RiskTrace
	}

	return decision, nil
}

func (p *PolicyEngine) resolveProfile(proposal core.ToolProposal) config.PolicyProfile {
	name := strings.TrimSpace(p.cfg.ActiveProfile)
	if raw, ok := proposal.Input["policy_profile"].(string); ok && strings.TrimSpace(raw) != "" {
		name = strings.TrimSpace(raw)
	}
	if profile, ok := p.cfg.Profiles[name]; ok {
		if profile.Name == "" {
			profile.Name = name
		}
		return profile
	}
	if profile, ok := p.cfg.Profiles["default"]; ok {
		if profile.Name == "" {
			profile.Name = "default"
		}
		return profile
	}
	return config.DefaultPolicyProfiles()["default"]
}

func profileRequiresApproval(profile config.PolicyProfile, effect string) bool {
	for _, pattern := range profile.RequireApprovalFor {
		if effectPatternMatches(pattern, effect) {
			return true
		}
	}
	return false
}

func profileDeniesEffect(profile config.PolicyProfile, effect string) bool {
	for _, pattern := range profile.DenyEffects {
		if effectPatternMatches(pattern, effect) {
			return true
		}
	}
	return false
}

func effectPatternMatches(pattern, effect string) bool {
	pattern = strings.TrimSpace(pattern)
	effect = strings.TrimSpace(effect)
	if pattern == "" || effect == "" {
		return false
	}
	if pattern == "*" || pattern == effect {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		return strings.HasPrefix(effect, strings.TrimSuffix(pattern, "*"))
	}
	switch pattern {
	case "network.write":
		return effect == "network.post" || effect == "network.put" || effect == "network.patch" || effect == "network.delete" || effect == "webhook.call" || effect == "email.send"
	case "docker.restart":
		return effect == "container.restart"
	case "service.stop":
		return strings.Contains(effect, "service.stop")
	}
	return false
}

func riskOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func buildRiskTrace(proposal core.ToolProposal, inference core.EffectInferenceResult, decision core.PolicyDecision, profile config.PolicyProfile) *core.RiskTrace {
	decisionName := "auto_execute"
	switch {
	case !decision.Allowed:
		decisionName = "deny"
	case decision.RequiresApproval:
		decisionName = "approval_required"
	}
	signals := append([]string(nil), inference.Signals...)
	signals = append(signals, signalsFromEffects(inference.Effects, inference.Sensitive, inference.ApprovalRequired)...)
	if profile.Name != "" {
		signals = append(signals, "policy_profile:"+profile.Name)
	}
	return &core.RiskTrace{
		ToolID:        proposal.Tool,
		Effects:       append([]string(nil), inference.Effects...),
		RiskLevel:     decision.RiskLevel,
		Confidence:    inference.Confidence,
		Signals:       uniqStrings(signals),
		PolicyProfile: profile.Name,
		Decision:      decisionName,
		Reason:        security.RedactString(decision.Reason),
	}
}

func signalsFromEffects(effects []string, sensitive bool, approvalRequired bool) []string {
	signals := []string{}
	if sensitive {
		signals = append(signals, "sensitive_resource")
	}
	if approvalRequired {
		signals = append(signals, "tool_profile_requires_approval")
	}
	for _, effect := range effects {
		switch {
		case effect == "unknown.effect":
			signals = append(signals, "unknown_effect")
		case strings.Contains(effect, "sensitive") || strings.Contains(effect, "env_file"):
			signals = append(signals, "sensitive_effect")
		case strings.Contains(effect, "write") || strings.Contains(effect, "modify") || strings.Contains(effect, "delete") || strings.Contains(effect, "install"):
			signals = append(signals, "mutating_effect")
		case strings.HasPrefix(effect, "network."):
			signals = append(signals, "network_effect")
		case strings.Contains(effect, "restart") || strings.Contains(effect, "stop") || strings.Contains(effect, "kill") || strings.Contains(effect, "escalate") || strings.Contains(effect, "danger"):
			signals = append(signals, "dangerous_effect")
		}
	}
	return signals
}

func uniqStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
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
	if effect == "network.post" || effect == "network.put" || effect == "network.patch" || effect == "network.delete" {
		return true
	}
	if strings.Contains(effect, "restart") || strings.Contains(effect, "stop") || strings.Contains(effect, "kill") || strings.Contains(effect, "escalate") {
		return true
	}
	return false
}
