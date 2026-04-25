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
	if strings.HasPrefix(proposal.Tool, "ops.") {
		return e.inferOps(proposal), nil
	}
	if strings.HasPrefix(proposal.Tool, "runbook.") {
		return e.inferRunbook(proposal), nil
	}
	switch proposal.Tool {
	case "shell.exec":
		return e.inferShell(proposal), nil
	case "code.read_file", "code.search", "code.search_text", "code.search_symbol", "code.list_files":
		return e.inferCodeRead(proposal), nil
	case "code.inspect_project", "code.detect_language", "code.detect_test_command":
		return readOnlyCodeInference("read-only project inspection"), nil
	case "code.run_tests", "code.fix_test_failure_loop":
		return e.inferCodeTest(proposal), nil
	case "code.parse_test_failure":
		return readOnlyCodeInference("test failure parsing is read-only"), nil
	case "kb.search", "kb.retrieve", "kb.answer":
		return core.EffectInferenceResult{
			Effects:       []string{"kb.read"},
			RiskLevel:     "read",
			Confidence:    0.95,
			ReasonSummary: "read-only knowledge retrieval",
		}, nil
	case "memory.search":
		return core.EffectInferenceResult{
			Effects:       []string{"memory.read"},
			RiskLevel:     "read",
			Confidence:    0.95,
			ReasonSummary: "read-only memory search",
		}, nil
	case "memory.extract_candidates":
		if inputBool(proposal.Input, "queue") {
			return core.EffectInferenceResult{
				Effects:          []string{"memory.review.write"},
				RiskLevel:        "write",
				ApprovalRequired: true,
				Confidence:       0.9,
				ReasonSummary:    "memory extraction writes candidate review records",
			}, nil
		}
		return core.EffectInferenceResult{
			Effects:       []string{"memory.review"},
			RiskLevel:     "read",
			Confidence:    0.9,
			ReasonSummary: "memory candidate extraction does not commit long-term memory",
		}, nil
	case "memory.detect_conflicts", "memory.merge_candidates":
		return core.EffectInferenceResult{
			Effects:       []string{"memory.read"},
			RiskLevel:     "read",
			Confidence:    0.9,
			ReasonSummary: "memory governance analysis is read-only",
		}, nil
	case "code.propose_patch":
		if e.proposalTouchesSensitivePath(proposal) {
			return core.EffectInferenceResult{
				Effects:          []string{"sensitive_read", "code.plan"},
				RiskLevel:        "sensitive",
				Sensitive:        true,
				ApprovalRequired: true,
				Confidence:       0.95,
				ReasonSummary:    "patch proposal targets a sensitive path",
			}, nil
		}
		return core.EffectInferenceResult{
			Effects:       []string{"read", "code.plan"},
			RiskLevel:     "read",
			Confidence:    0.9,
			ReasonSummary: "proposal-only patch tool",
		}, nil
	case "code.validate_patch", "code.dry_run_patch":
		if e.proposalTouchesSensitivePath(proposal) {
			return core.EffectInferenceResult{
				Effects:          []string{"sensitive_read", "code.plan"},
				RiskLevel:        "sensitive",
				Sensitive:        true,
				ApprovalRequired: true,
				Confidence:       0.95,
				ReasonSummary:    "patch validation reads a sensitive path",
			}, nil
		}
		return core.EffectInferenceResult{
			Effects:       []string{"read", "code.plan"},
			RiskLevel:     "read",
			Confidence:    0.95,
			ReasonSummary: "patch validation is read-only",
		}, nil
	case "code.explain_diff":
		return readOnlyCodeInference("diff explanation is read-only"), nil
	case "code.apply_patch":
		if e.proposalTouchesSensitivePath(proposal) {
			return core.EffectInferenceResult{
				Effects:          []string{"fs.write", "code.modify", "sensitive_write"},
				RiskLevel:        "sensitive",
				Sensitive:        true,
				ApprovalRequired: true,
				Confidence:       0.99,
				ReasonSummary:    "patch application modifies a sensitive path",
			}, nil
		}
		return core.EffectInferenceResult{
			Effects:          []string{"fs.write", "code.modify"},
			RiskLevel:        "write",
			ApprovalRequired: true,
			Confidence:       0.99,
			ReasonSummary:    "patch application modifies workspace files",
		}, nil
	case "git.status", "git.diff", "git.log", "git.branch", "git.diff_summary", "git.commit_message_proposal":
		return core.EffectInferenceResult{
			Effects:       []string{"read", "git.read"},
			RiskLevel:     "read",
			Confidence:    0.95,
			ReasonSummary: "read-only git command",
		}, nil
	case "git.add", "git.commit":
		return core.EffectInferenceResult{
			Effects:          []string{"git.write", "fs.write"},
			RiskLevel:        "write",
			ApprovalRequired: true,
			Confidence:       0.99,
			ReasonSummary:    "git command mutates repository state",
		}, nil
	case "git.restore":
		return core.EffectInferenceResult{
			Effects:          []string{"git.write", "fs.write", "code.modify"},
			RiskLevel:        "write",
			ApprovalRequired: true,
			Confidence:       0.99,
			ReasonSummary:    "git restore modifies workspace files",
		}, nil
	case "git.clean":
		return core.EffectInferenceResult{
			Effects:          []string{"fs.delete", "dangerous"},
			RiskLevel:        "danger",
			ApprovalRequired: true,
			Confidence:       0.99,
			ReasonSummary:    "git clean deletes untracked files",
		}, nil
	case "memory.patch":
		return core.EffectInferenceResult{
			Effects:          []string{"fs.write", "memory.modify"},
			RiskLevel:        "write",
			ApprovalRequired: true,
			Confidence:       0.95,
			ReasonSummary:    "memory patch modifies markdown facts",
		}, nil
	case "memory.item_create", "memory.item_update", "memory.item_archive", "memory.item_restore", "memory.item_delete":
		return core.EffectInferenceResult{
			Effects:          []string{"fs.write", "memory.modify"},
			RiskLevel:        "write",
			ApprovalRequired: true,
			Confidence:       0.98,
			ReasonSummary:    "memory item operation modifies Markdown memory",
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

func (e *EffectInferrer) inferCodeRead(proposal core.ToolProposal) core.EffectInferenceResult {
	if e.proposalTouchesSensitivePath(proposal) || inputBool(proposal.Input, "include_sensitive") {
		return core.EffectInferenceResult{
			Effects:          []string{"sensitive_read", "code.read"},
			RiskLevel:        "sensitive",
			Sensitive:        true,
			ApprovalRequired: true,
			Confidence:       0.95,
			ReasonSummary:    "code read touches sensitive resources",
		}
	}
	return readOnlyCodeInference("read-only code workspace tool")
}

func (e *EffectInferrer) inferOps(proposal core.ToolProposal) core.EffectInferenceResult {
	if e.proposalTouchesSensitivePath(proposal) {
		return core.EffectInferenceResult{
			Effects:          []string{"sensitive_read", "log.read"},
			RiskLevel:        "sensitive",
			Sensitive:        true,
			ApprovalRequired: true,
			Confidence:       0.95,
			ReasonSummary:    "ops tool touches sensitive resources",
		}
	}
	switch proposal.Tool {
	case "ops.local.system_info":
		return readOnlyOpsInference([]string{"system.read"}, "local system information read")
	case "ops.local.processes":
		return readOnlyOpsInference([]string{"process.read", "system.metrics.read"}, "local process metrics read")
	case "ops.local.disk_usage":
		return readOnlyOpsInference([]string{"disk.read", "system.metrics.read"}, "local disk usage read")
	case "ops.local.memory_usage":
		return readOnlyOpsInference([]string{"memory.read", "system.metrics.read"}, "local memory usage read")
	case "ops.local.network_info":
		return readOnlyOpsInference([]string{"network.read"}, "local network information read")
	case "ops.local.service_status":
		return readOnlyOpsInference([]string{"service.read"}, "local service status read")
	case "ops.local.logs_tail":
		return readOnlyOpsInference([]string{"log.read"}, "local log tail read")
	case "ops.local.service_restart":
		return core.EffectInferenceResult{
			Effects:          []string{"service.restart", "system.write"},
			RiskLevel:        "danger",
			ApprovalRequired: true,
			Confidence:       0.99,
			ReasonSummary:    "local service restart changes process state",
		}
	case "ops.ssh.system_info":
		return readOnlyOpsInference([]string{"ssh.read", "system.read"}, "ssh system information read")
	case "ops.ssh.processes":
		return readOnlyOpsInference([]string{"ssh.read", "process.read", "system.metrics.read"}, "ssh process metrics read")
	case "ops.ssh.disk_usage":
		return readOnlyOpsInference([]string{"ssh.read", "disk.read", "system.metrics.read"}, "ssh disk usage read")
	case "ops.ssh.memory_usage":
		return readOnlyOpsInference([]string{"ssh.read", "memory.read", "system.metrics.read"}, "ssh memory usage read")
	case "ops.ssh.logs_tail":
		return readOnlyOpsInference([]string{"ssh.read", "log.read"}, "ssh log tail read")
	case "ops.ssh.service_status":
		return readOnlyOpsInference([]string{"ssh.read", "service.read"}, "ssh service status read")
	case "ops.ssh.service_restart":
		return core.EffectInferenceResult{
			Effects:          []string{"ssh.write", "service.restart", "system.write"},
			RiskLevel:        "danger",
			ApprovalRequired: true,
			Confidence:       0.99,
			ReasonSummary:    "ssh service restart changes remote process state",
		}
	case "ops.docker.ps", "ops.docker.inspect":
		return readOnlyOpsInference([]string{"container.read"}, "docker container metadata read")
	case "ops.docker.logs":
		return readOnlyOpsInference([]string{"container.read", "log.read"}, "docker logs read")
	case "ops.docker.stats":
		return readOnlyOpsInference([]string{"container.read", "system.metrics.read"}, "docker stats read")
	case "ops.docker.restart", "ops.docker.stop", "ops.docker.start":
		return core.EffectInferenceResult{
			Effects:          []string{"container.write", "container." + strings.TrimPrefix(proposal.Tool, "ops.docker.")},
			RiskLevel:        "write",
			ApprovalRequired: true,
			Confidence:       0.99,
			ReasonSummary:    "docker lifecycle operation changes container state",
		}
	case "ops.k8s.get", "ops.k8s.describe", "ops.k8s.events":
		return readOnlyOpsInference([]string{"k8s.read"}, "kubernetes resource read")
	case "ops.k8s.logs":
		return readOnlyOpsInference([]string{"k8s.read", "log.read"}, "kubernetes logs read")
	case "ops.k8s.apply", "ops.k8s.rollout_restart":
		return core.EffectInferenceResult{
			Effects:          []string{"k8s.write", strings.TrimPrefix(proposal.Tool, "ops.")},
			RiskLevel:        "write",
			ApprovalRequired: true,
			Confidence:       0.99,
			ReasonSummary:    "kubernetes operation changes cluster state",
		}
	case "ops.k8s.delete":
		return core.EffectInferenceResult{
			Effects:          []string{"k8s.delete", "dangerous"},
			RiskLevel:        "danger",
			ApprovalRequired: true,
			Confidence:       0.99,
			ReasonSummary:    "kubernetes delete is destructive",
		}
	default:
		return core.EffectInferenceResult{
			Effects:          []string{"unknown.effect"},
			RiskLevel:        "unknown",
			ApprovalRequired: true,
			Confidence:       0.3,
			ReasonSummary:    "unknown ops tool",
		}
	}
}

func (e *EffectInferrer) inferRunbook(proposal core.ToolProposal) core.EffectInferenceResult {
	switch proposal.Tool {
	case "runbook.list", "runbook.read", "runbook.plan":
		return core.EffectInferenceResult{
			Effects:       []string{"read", "runbook.read"},
			RiskLevel:     "read",
			Confidence:    0.95,
			ReasonSummary: "runbook planning is read-only",
		}
	case "runbook.execute_step", "runbook.execute":
		return core.EffectInferenceResult{
			Effects:       []string{"runbook.execute", "workflow.route"},
			RiskLevel:     "read",
			Confidence:    0.9,
			ReasonSummary: "runbook execution routes each concrete step through ToolRouter",
		}
	default:
		return core.EffectInferenceResult{
			Effects:          []string{"unknown.effect"},
			RiskLevel:        "unknown",
			ApprovalRequired: true,
			Confidence:       0.3,
			ReasonSummary:    "unknown runbook tool",
		}
	}
}

func readOnlyOpsInference(effects []string, reason string) core.EffectInferenceResult {
	allEffects := append([]string{"read"}, effects...)
	return core.EffectInferenceResult{
		Effects:       uniq(allEffects),
		RiskLevel:     "read",
		Confidence:    0.95,
		ReasonSummary: reason,
	}
}

func (e *EffectInferrer) inferCodeTest(proposal core.ToolProposal) core.EffectInferenceResult {
	command, _ := proposal.Input["command"].(string)
	if command == "" {
		command, _ = proposal.Input["test_command"].(string)
	}
	if command == "" && (inputBool(proposal.Input, "use_detected") || proposal.Tool == "code.fix_test_failure_loop") {
		return core.EffectInferenceResult{
			Effects:       []string{"code.test", "process.read", "fs.read"},
			RiskLevel:     "read",
			Confidence:    0.9,
			ReasonSummary: "detected test command runs under code test allowlist",
		}
	}
	if command == "" {
		return core.EffectInferenceResult{
			Effects:          []string{"unknown.effect"},
			RiskLevel:        "unknown",
			ApprovalRequired: true,
			Confidence:       0.3,
			ReasonSummary:    "test command is not specified",
		}
	}
	structure := shell.ParseStructure(command)
	if structure.HasWriteRedirect || strings.Contains(command, "&&") || strings.Contains(command, ";") {
		return core.EffectInferenceResult{
			Effects:          []string{"unknown.effect", "fs.write"},
			RiskLevel:        "unknown",
			ApprovalRequired: true,
			Confidence:       0.5,
			ReasonSummary:    "test command contains shell control or redirect syntax",
		}
	}
	if len(structure.Segments) == 0 {
		return core.EffectInferenceResult{
			Effects:          []string{"unknown.effect"},
			RiskLevel:        "unknown",
			ApprovalRequired: true,
			Confidence:       0.3,
			ReasonSummary:    "empty test command",
		}
	}
	first := structure.Segments[0].Name
	args := structure.Segments[0].Args
	if testCommandHasInstallOrMutation(first, args) {
		return core.EffectInferenceResult{
			Effects:          []string{"code.test", "package.install", "fs.write", "unknown.effect"},
			RiskLevel:        "write",
			ApprovalRequired: true,
			Confidence:       0.8,
			ReasonSummary:    "test command appears to install dependencies or mutate project files",
		}
	}
	if isRecognizedTestCommand(first, args) {
		return core.EffectInferenceResult{
			Effects:       []string{"code.test", "process.read", "fs.read"},
			RiskLevel:     "read",
			Confidence:    0.9,
			ReasonSummary: "recognized read-only test command",
		}
	}
	return core.EffectInferenceResult{
		Effects:          []string{"unknown.effect", "code.test"},
		RiskLevel:        "unknown",
		ApprovalRequired: true,
		Confidence:       0.45,
		ReasonSummary:    "test command is not in the code test allowlist",
	}
}

func readOnlyCodeInference(reason string) core.EffectInferenceResult {
	return core.EffectInferenceResult{
		Effects:       []string{"read", "code.read"},
		RiskLevel:     "read",
		Confidence:    0.95,
		ReasonSummary: reason,
	}
}

func (e *EffectInferrer) proposalTouchesSensitivePath(proposal core.ToolProposal) bool {
	for _, path := range proposalPathInputs(proposal.Input) {
		if security.IsSensitivePath(path, e.cfg.SensitivePaths) {
			return true
		}
	}
	return false
}

func proposalPathInputs(input map[string]any) []string {
	var paths []string
	if path, ok := input["path"].(string); ok && path != "" {
		paths = append(paths, path)
	}
	if path, ok := input["workspace"].(string); ok && path != "" {
		paths = append(paths, path)
	}
	if files, ok := input["files"].([]any); ok {
		for _, item := range files {
			file, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if path, ok := file["path"].(string); ok && path != "" {
				paths = append(paths, path)
			}
		}
	}
	if files, ok := input["files"].([]map[string]any); ok {
		for _, file := range files {
			if path, ok := file["path"].(string); ok && path != "" {
				paths = append(paths, path)
			}
		}
	}
	if diff, ok := input["diff"].(string); ok && diff != "" {
		paths = append(paths, diffPathInputs(diff)...)
	}
	return paths
}

func diffPathInputs(diff string) []string {
	var paths []string
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+++ ") && !strings.HasPrefix(line, "--- ") {
			continue
		}
		path := strings.TrimSpace(line[4:])
		if idx := strings.IndexAny(path, "\t "); idx >= 0 {
			path = path[:idx]
		}
		if path == "/dev/null" || path == "" {
			continue
		}
		path = strings.TrimPrefix(path, "a/")
		path = strings.TrimPrefix(path, "b/")
		paths = append(paths, path)
	}
	return paths
}

func isRecognizedTestCommand(command string, args []string) bool {
	switch command {
	case "go":
		return len(args) > 0 && args[0] == "test"
	case "npm", "pnpm", "yarn":
		return len(args) > 0 && (args[0] == "test" || (len(args) > 1 && args[0] == "run" && args[1] == "test"))
	case "pytest":
		return true
	case "python", "python3":
		return len(args) >= 2 && args[0] == "-m" && args[1] == "pytest"
	case "cargo":
		return len(args) > 0 && args[0] == "test"
	case "make":
		return len(args) > 0 && args[0] == "test"
	default:
		return false
	}
}

func testCommandHasInstallOrMutation(command string, args []string) bool {
	all := append([]string{command}, args...)
	for i, item := range all {
		switch item {
		case "install", "add", "upgrade", "remove", "update":
			return true
		case "get":
			if i > 0 && all[i-1] == "go" {
				return true
			}
		case "tidy":
			if i > 1 && all[i-2] == "go" && all[i-1] == "mod" {
				return true
			}
		case "generate":
			if i > 0 && all[i-1] == "go" {
				return true
			}
		}
	}
	return false
}

func inputBool(input map[string]any, key string) bool {
	value, _ := input[key].(bool)
	return value
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
	if profile.RequiresApproval {
		result.ApprovalRequired = true
		result.Confidence = minConfidence(result.Confidence, 0.6)
		if profile.WillFallback {
			result.ReasonSummary = "skill sandbox falls back to best-effort enforcement and requires approval"
		} else if result.ReasonSummary == "skill manifest declares read-only effects" {
			result.ReasonSummary = "skill sandbox requires approval due to enforcement limits"
		}
	} else if profile.WillFallback {
		result.Confidence = minConfidence(result.Confidence, 0.7)
		if result.ReasonSummary == "skill manifest declares read-only effects" {
			result.ReasonSummary = "skill sandbox falls back to best-effort enforcement"
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
			case "PATCH":
				return []string{"network.patch"}
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
	if effect == "network.post" || effect == "network.put" || effect == "network.patch" || effect == "network.delete" {
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
