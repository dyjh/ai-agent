package catalog

import "strings"

func domainForTool(tool string) string {
	switch {
	case strings.HasPrefix(tool, "code."):
		return "code"
	case strings.HasPrefix(tool, "git."):
		return "git"
	case strings.HasPrefix(tool, "ops."), strings.HasPrefix(tool, "runbook."):
		return "ops"
	case strings.HasPrefix(tool, "kb."):
		return "rag"
	case strings.HasPrefix(tool, "memory."):
		return "memory"
	case strings.HasPrefix(tool, "skill."):
		return "skill"
	case strings.HasPrefix(tool, "mcp."):
		return "mcp"
	case strings.HasPrefix(tool, "security."):
		return "security"
	default:
		return "unknown"
	}
}

func requiresApproval(tool string, effects []string) bool {
	if !autoSelectable(tool, effects) {
		return true
	}
	for _, effect := range effects {
		if effect == "unknown.effect" || strings.Contains(effect, "write") || strings.Contains(effect, "modify") || strings.Contains(effect, "delete") || strings.Contains(effect, "install") || strings.Contains(effect, "restart") || strings.Contains(effect, "danger") || strings.Contains(effect, "sensitive") {
			return true
		}
	}
	return false
}

func autoSelectable(tool string, effects []string) bool {
	switch tool {
	case "shell.exec", "code.apply_patch", "git.add", "git.commit", "git.restore", "git.clean", "ops.local.service_restart", "ops.docker.restart", "ops.k8s.apply", "ops.k8s.delete", "memory.patch", "memory.item_create", "memory.item_update", "memory.item_delete", "memory.item_restore", "skill.run", "mcp.call_tool":
		return false
	}
	for _, effect := range effects {
		if effect == "unknown.effect" || strings.Contains(effect, "danger") || strings.Contains(effect, "delete") || strings.Contains(effect, "install") {
			return false
		}
	}
	return true
}
