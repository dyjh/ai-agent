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
	cfg config.PolicyConfig
}

// NewEffectInferrer constructs an inferrer.
func NewEffectInferrer(cfg config.PolicyConfig) *EffectInferrer {
	return &EffectInferrer{cfg: cfg}
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
	case "skill.run", "mcp.call_tool":
		return core.EffectInferenceResult{
			Effects:          []string{"unknown.effect"},
			RiskLevel:        "unknown",
			ApprovalRequired: true,
			Confidence:       0.4,
			ReasonSummary:    "external capability requires explicit approval",
		}, nil
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
		if effect == "fs.write" || effect == "code.modify" || effect == "package.install" || effect == "git.write" || effect == "fs.delete" || effect == "network.post" || effect == "network.put" || effect == "network.delete" || effect == "privilege.escalate" {
			return true
		}
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
