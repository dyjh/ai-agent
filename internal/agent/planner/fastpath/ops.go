package fastpath

import (
	"strings"

	"local-agent/internal/agent/planner/normalize"
	"local-agent/internal/agent/planner/semantic"
)

func docker(req normalize.NormalizedRequest, signals map[string]bool) semantic.SemanticPlan {
	if signals["logs"] {
		return one("ops", "ops.docker.logs", "读取 Docker 容器日志", map[string]any{"container": target(req.Original, "container", "container"), "max_lines": 200}, 0.9, "fastpath docker logs")
	}
	if strings.Contains(req.NormalizedText, "restart") || strings.Contains(req.NormalizedText, "重启") {
		return one("ops", "ops.docker.restart", "重启 Docker 容器", map[string]any{"container": target(req.Original, "container", "container")}, 0.88, "fastpath docker restart")
	}
	return one("ops", "ops.docker.ps", "查看 Docker 容器状态", map[string]any{}, 0.9, "fastpath docker ps")
}

func k8s(req normalize.NormalizedRequest, signals map[string]bool) semantic.SemanticPlan {
	if signals["logs"] {
		return one("ops", "ops.k8s.logs", "读取 Kubernetes Pod 日志", map[string]any{"target": target(req.Original, "pod", "pod"), "max_lines": 200}, 0.9, "fastpath k8s logs")
	}
	return one("ops", "ops.k8s.get", "查看 Kubernetes 资源", map[string]any{"resource": k8sResource(req.Original, "pods")}, 0.9, "fastpath k8s get")
}

func ssh(req normalize.NormalizedRequest, signals map[string]bool) semantic.SemanticPlan {
	hostID := req.HostID
	if hostID == "" {
		hostID = "local"
	}
	if signals["logs"] {
		return one("ops", "ops.ssh.logs_tail", "读取 SSH 主机日志", map[string]any{"host_id": hostID, "path": logPath(req), "max_lines": 100}, 0.88, "fastpath ssh logs")
	}
	return one("ops", "ops.ssh.processes", "查看 SSH 主机进程", map[string]any{"host_id": hostID}, 0.88, "fastpath ssh processes")
}

func logPath(req normalize.NormalizedRequest) string {
	if quoted := firstQuoted(req); quoted != "" {
		return quoted
	}
	return "/var/log/syslog"
}

func target(message, marker, fallback string) string {
	if quoted := quotedFromRaw(message); quoted != "" {
		return quoted
	}
	fields := strings.Fields(message)
	for idx, field := range fields {
		lower := strings.ToLower(strings.Trim(field, "`'\"，,。;；"))
		if lower == marker || lower == "容器" || lower == "pod" || lower == "pods" || lower == "service" || lower == "服务" {
			if idx+1 < len(fields) {
				return strings.Trim(fields[idx+1], "`'\"，,。;；")
			}
		}
	}
	return fallback
}

func k8sResource(message, fallback string) string {
	fields := strings.Fields(message)
	for idx, field := range fields {
		lower := strings.ToLower(strings.Trim(field, "`'\"，,。;；"))
		if lower == "get" && idx+1 < len(fields) {
			return strings.Trim(fields[idx+1], "`'\"，,。;；")
		}
	}
	return fallback
}
