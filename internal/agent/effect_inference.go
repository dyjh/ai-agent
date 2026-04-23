package agent

import (
	"context"
	"strings"

	"local-agent/internal/config"
	"local-agent/internal/core"
	"local-agent/internal/security"
	"local-agent/internal/tools/shell"
)

// EffectInferrer derives effects from tool proposals and shell structure.
type EffectInferrer struct {
	cfg    config.PolicyConfig
	skills SkillProfileResolver
	mcp    MCPPolicyResolver
}

// SkillProfileResolver exposes registered skill effects to the inferrer.
type SkillProfileResolver interface {
	PolicyProfile(id string) (core.SkillPolicyProfile, error)
}

// MCPPolicyResolver exposes MCP tool policy overlays to the inferrer.
type MCPPolicyResolver interface {
	PolicyProfile(serverID, toolName string) (core.MCPPolicyProfile, error)
}

// NewEffectInferrer constructs an inferrer.
func NewEffectInferrer(cfg config.PolicyConfig, resolvers ...any) *EffectInferrer {
	inferrer := &EffectInferrer{cfg: cfg}
	for _, resolver := range resolvers {
		if skills, ok := resolver.(SkillProfileResolver); ok {
			inferrer.skills = skills
		}
		if mcp, ok := resolver.(MCPPolicyResolver); ok {
			inferrer.mcp = mcp
		}
	}
	return inferrer
}

// Infer infers effects for a tool proposal.
func (e *EffectInferrer) Infer(_ context.Context, proposal core.ToolProposal) (core.EffectInferenceResult, error) {
	switch proposal.Tool {
	case "shell.exec":
		return e.inferShell(proposal), nil
	case "code.read_file", "code.search":
		return core.EffectInferenceResult{
			Effects:       []string{"read", "code.read"},
			RiskLevel:     "read",
			Confidence:    0.95,
			ReasonSummary: "read-only tool",
		}, nil
	case "kb.search":
		return core.EffectInferenceResult{
			Effects:       []string{"kb.read"},
			RiskLevel:     "read",
			Confidence:    0.95,
			ReasonSummary: "read-only knowledge search",
		}, nil
	case "memory.search":
		return core.EffectInferenceResult{
			Effects:       []string{"memory.read"},
			RiskLevel:     "read",
			Confidence:    0.95,
			ReasonSummary: "read-only memory search",
		}, nil
	case "code.propose_patch":
		return core.EffectInferenceResult{
			Effects:       []string{"read", "code.plan"},
			RiskLevel:     "read",
			Confidence:    0.9,
			ReasonSummary: "proposal-only patch tool",
		}, nil
	case "code.apply_patch":
		return core.EffectInferenceResult{
			Effects:          []string{"fs.write", "code.modify"},
			RiskLevel:        "write",
			ApprovalRequired: true,
			Confidence:       0.99,
			ReasonSummary:    "patch application modifies workspace files",
		}, nil
	case "memory.patch":
		return core.EffectInferenceResult{
			Effects:          []string{"fs.write", "memory.modify"},
			RiskLevel:        "write",
			ApprovalRequired: true,
			Confidence:       0.95,
			ReasonSummary:    "memory patch modifies markdown facts",
		}, nil
	case "skill.run":
		return e.inferSkill(proposal), nil
	case "mcp.call_tool":
		return e.inferMCP(proposal)
	default:
		return core.EffectInferenceResult{
			Effects:          []string{"unknown.effect"},
			RiskLevel:        "unknown",
			ApprovalRequired: true,
			Confidence:       0.2,
			ReasonSummary:    "tool has no known effect profile",
		}, nil
	}
}

func (e *EffectInferrer) inferMCP(proposal core.ToolProposal) (core.EffectInferenceResult, error) {
	serverID, _ := proposal.Input["server_id"].(string)
	toolName, _ := proposal.Input["tool_name"].(string)
	if serverID == "" || toolName == "" || e.mcp == nil {
		return core.EffectInferenceResult{
			Effects:          []string{"unknown.effect"},
			RiskLevel:        "unknown",
			ApprovalRequired: true,
			Confidence:       0.3,
			ReasonSummary:    "MCP server_id or tool_name is unavailable",
		}, nil
	}

	profile, err := e.mcp.PolicyProfile(serverID, toolName)
	if err != nil {
		return core.EffectInferenceResult{}, err
	}
	effects := profile.Effects
	if len(effects) == 0 {
		effects = []string{"unknown.effect"}
	}

	result := core.EffectInferenceResult{
		Effects:          append([]string(nil), effects...),
		RiskLevel:        profile.RiskLevel,
		ApprovalRequired: profile.RequiresApproval,
		Confidence:       profile.Confidence,
		ReasonSummary:    profile.Reason,
	}
	if result.RiskLevel == "" {
		result.RiskLevel = "read"
	}
	if result.Confidence <= 0 {
		result.Confidence = 0.4
	}
	if result.ReasonSummary == "" {
		result.ReasonSummary = "MCP policy profile"
	}

	for _, effect := range effects {
		switch {
		case effect == "unknown.effect":
			result.RiskLevel = "unknown"
			result.ApprovalRequired = true
			result.Confidence = minConfidence(result.Confidence, 0.4)
			result.ReasonSummary = "MCP tool has unknown effects"
		case strings.Contains(effect, "sensitive") || strings.Contains(effect, "env_file"):
			result.RiskLevel = "sensitive"
			result.Sensitive = true
			result.ApprovalRequired = true
		case strings.Contains(effect, "kill") || strings.Contains(effect, "restart") || strings.Contains(effect, "stop") || strings.Contains(effect, "escalate") || strings.Contains(effect, "danger"):
			result.RiskLevel = "danger"
			result.ApprovalRequired = true
		case effectRequiresApproval(effect):
			result.RiskLevel = "write"
			result.ApprovalRequired = true
		}
	}

	if profile.Approval == "require" {
		result.ApprovalRequired = true
	}
	return result, nil
}

func (e *EffectInferrer) inferSkill(proposal core.ToolProposal) core.EffectInferenceResult {
	skillID, _ := proposal.Input["skill_id"].(string)
	if skillID == "" || e.skills == nil {
		return core.EffectInferenceResult{
			Effects:          []string{"unknown.effect"},
			RiskLevel:        "unknown",
			ApprovalRequired: true,
			Confidence:       0.3,
			ReasonSummary:    "skill manifest is unavailable",
		}
	}

	profile, err := e.skills.PolicyProfile(skillID)
	if err != nil {
		return core.EffectInferenceResult{
			Effects:          []string{"unknown.effect"},
			RiskLevel:        "unknown",
			ApprovalRequired: true,
			Confidence:       0.3,
			ReasonSummary:    "skill is not registered",
		}
	}

	effects := profile.Effects
	if len(effects) == 0 {
		effects = []string{"unknown.effect"}
	}

	result := core.EffectInferenceResult{
		Effects:       append([]string(nil), effects...),
		RiskLevel:     "read",
		Confidence:    0.95,
		ReasonSummary: "skill manifest declares read-only effects",
	}

	for _, effect := range effects {
		switch {
		case effect == "unknown.effect":
			result.RiskLevel = "unknown"
			result.ApprovalRequired = true
			result.Confidence = 0.4
			result.ReasonSummary = "skill manifest has unknown effects"
		case effectRequiresApproval(effect):
			result.ApprovalRequired = true
			if strings.Contains(effect, "sensitive") || strings.Contains(effect, "env_file") {
				result.RiskLevel = "sensitive"
				result.Sensitive = true
				result.ReasonSummary = "skill manifest declares sensitive access"
				continue
			}
			if strings.Contains(effect, "kill") || strings.Contains(effect, "restart") || strings.Contains(effect, "stop") || strings.Contains(effect, "escalate") {
				result.RiskLevel = "danger"
				result.ReasonSummary = "skill manifest declares dangerous effects"
				continue
			}
			result.RiskLevel = "write"
			result.ReasonSummary = "skill manifest declares mutating effects"
		}
	}

	if profile.ApprovalDefault == "require" {
		result.ApprovalRequired = true
		if result.ReasonSummary == "skill manifest declares read-only effects" {
			result.ReasonSummary = "skill manifest requires approval by default"
		}
	}

	return result
}

func (e *EffectInferrer) inferShell(proposal core.ToolProposal) core.EffectInferenceResult {
	command, _ := proposal.Input["command"].(string)
	structure := shell.ParseStructure(command)

	effects := make([]string, 0, 4)
	confidence := 0.9
	riskLevel := "read"
	approvalRequired := false
	reason := "read-only shell query"

	if structure.HasWriteRedirect {
		effects = append(effects, "fs.write")
		riskLevel = "write"
		approvalRequired = true
		reason = "shell redirect writes to the filesystem"
	}

	if sensitive := containsSensitive(structure.PossibleFileTargets, e.cfg.SensitivePaths); sensitive {
		effects = append(effects, "sensitive_read", "env_file.read")
		riskLevel = "sensitive"
		approvalRequired = true
		reason = "command touches sensitive resources"
	}

	if len(structure.Segments) == 0 {
		return core.EffectInferenceResult{
			Effects:          []string{"unknown.effect"},
			RiskLevel:        "unknown",
			Sensitive:        false,
			ApprovalRequired: true,
			Confidence:       0.1,
			ReasonSummary:    "empty shell command",
		}
	}

	first := structure.Segments[0].Name
	rest := append([]string(nil), structure.Segments[0].Args...)

	switch first {
	case "ps", "pgrep", "top":
		effects = append(effects, "read", "process.read")
		reason = "process inspection command"
	case "uname", "uptime", "free", "df", "pwd", "whoami":
		effects = append(effects, "read", "system.read")
		reason = "system inspection command"
	case "git":
		effects = append(effects, e.classifyGit(rest)...)
		if hasMutatingEffect(effects) {
			riskLevel = "write"
			approvalRequired = true
			reason = "git command mutates repository state"
		} else {
			reason = "git read-only command"
		}
	case "cat", "less", "head", "tail", "ls", "find", "rg", "grep", "sed":
		effects = append(effects, "read", "code.read")
		if first == "sed" && contains(rest, "-i") {
			effects = append(effects, "fs.write", "code.modify")
			riskLevel = "write"
			approvalRequired = true
			reason = "sed -i modifies files"
		}
	case "tee", "touch", "mkdir", "mv", "cp", "rm":
		effects = append(effects, classifyFSMutation(first)...)
		riskLevel = "write"
		approvalRequired = true
		reason = "filesystem mutation command"
	case "go", "npm", "pnpm", "yarn", "pip", "apt", "brew":
		effects = append(effects, e.classifyPackageCommand(first, rest)...)
		if hasMutatingEffect(effects) {
			riskLevel = "write"
			approvalRequired = true
			reason = "package or module management command"
		} else {
			reason = "read-only tooling query"
		}
	case "curl", "wget":
		effects = append(effects, e.classifyNetworkCommand(rest)...)
		if hasMutatingEffect(effects) {
			riskLevel = "write"
			approvalRequired = true
			reason = "network write or upload detected"
		} else {
			reason = "network read command"
		}
	case "sudo":
		effects = append(effects, "privilege.escalate")
		riskLevel = "danger"
		approvalRequired = true
		confidence = 0.98
		reason = "sudo requires explicit approval"
	default:
		effects = append(effects, "unknown.effect")
		riskLevel = "unknown"
		approvalRequired = true
		confidence = 0.4
		reason = "command not covered by current manifest classifier"
	}

	if len(proposal.ExpectedEffects) > 0 {
		effects = append(effects, proposal.ExpectedEffects...)
	}

	effects = uniq(effects)

	return core.EffectInferenceResult{
		Effects:          effects,
		RiskLevel:        riskLevel,
		Sensitive:        contains(effects, "sensitive_read"),
		ApprovalRequired: approvalRequired,
		Confidence:       confidence,
		ReasonSummary:    reason,
	}
}

func (e *EffectInferrer) classifyGit(args []string) []string {
	if len(args) == 0 {
		return []string{"read", "git.read"}
	}
	switch args[0] {
	case "status", "diff", "log", "show", "branch", "rev-parse":
		return []string{"read", "git.read"}
	default:
		return []string{"git.write", "fs.write"}
	}
}

func (e *EffectInferrer) classifyPackageCommand(command string, args []string) []string {
	if len(args) == 0 {
		return []string{"read", "system.read"}
	}
	head := args[0]
	switch command {
	case "go":
		if head == "version" || head == "env" || head == "list" {
			return []string{"read", "system.read"}
		}
		if head == "get" || (head == "mod" && len(args) > 1 && args[1] == "tidy") {
			return []string{"package.install", "fs.write"}
		}
	case "npm", "pnpm", "yarn", "pip", "apt", "brew":
		if contains(args, "install") || contains(args, "add") || contains(args, "upgrade") || contains(args, "remove") {
			return []string{"package.install", "fs.write"}
		}
	}
	return []string{"read", "system.read"}
}

func (e *EffectInferrer) classifyNetworkCommand(args []string) []string {
	if contains(args, "-X") {
		for i, arg := range args {
			if arg != "-X" || i+1 >= len(args) {
				continue
			}
			method := strings.ToUpper(args[i+1])
			switch method {
			case "POST":
				return []string{"network.post"}
			case "PUT":
				return []string{"network.put"}
			case "DELETE":
				return []string{"network.delete"}
			}
		}
	}
	return []string{"read", "network.read"}
}

func classifyFSMutation(command string) []string {
	switch command {
	case "rm":
		return []string{"fs.delete"}
	case "mv", "cp", "tee", "touch", "mkdir":
		return []string{"fs.write", "code.modify"}
	default:
		return []string{"fs.write"}
	}
}

func containsSensitive(paths []string, extra []string) bool {
	for _, path := range paths {
		if security.IsSensitivePath(path, extra) {
			return true
		}
	}
	return false
}

func hasMutatingEffect(effects []string) bool {
	for _, effect := range effects {
		if effectRequiresApproval(effect) {
			return true
		}
	}
	return false
}

func effectRequiresApproval(effect string) bool {
	if effect == "unknown.effect" {
		return true
	}
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

func contains(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func uniq(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
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

func minConfidence(current, limit float64) float64 {
	if current <= 0 || current > limit {
		return limit
	}
	return current
}
