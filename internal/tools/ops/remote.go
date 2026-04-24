package ops

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"local-agent/internal/core"
	"local-agent/internal/security"
	"local-agent/internal/tools"
)

// SSHExecutor implements fixed SSH operations.
type SSHExecutor struct {
	Manager               *Manager
	Operation             string
	Runner                CommandRunner
	DefaultTimeoutSeconds int
	MaxOutputBytes        int64
}

// DockerExecutor implements fixed Docker operations.
type DockerExecutor struct {
	Operation             string
	Runner                CommandRunner
	DefaultTimeoutSeconds int
	MaxOutputBytes        int64
}

// K8sExecutor implements fixed kubectl operations.
type K8sExecutor struct {
	Manager               *Manager
	Operation             string
	Runner                CommandRunner
	DefaultTimeoutSeconds int
	MaxOutputBytes        int64
}

// Execute runs a fixed SSH command selected by operation.
func (e *SSHExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	if e.Manager == nil {
		return nil, fmt.Errorf("ops manager is required")
	}
	hostID, _ := input["host_id"].(string)
	host, err := e.Manager.getHostRaw(hostID)
	if err != nil {
		return nil, err
	}
	if host.Type != HostTypeSSH || host.SSH == nil {
		return nil, fmt.Errorf("host %s is not an ssh host", host.HostID)
	}
	command, err := e.sshRemoteCommand(input)
	if err != nil {
		return nil, err
	}
	args := sshArgs(host.SSH, command)
	output := map[string]any{"operation": e.Operation, "host_id": host.HostID}
	if e.Operation == "logs_tail" {
		output["path"] = input["path"]
	}
	if e.Operation == "service_status" || e.Operation == "service_restart" {
		output["service"] = input["service"]
	}
	if e.Operation == "service_restart" {
		output["rollback_plan"] = RollbackForTool("ops.ssh.service_restart", input)
	}
	result, runErr := e.runner().Run(ctx, "ssh", args, timeoutFromInput(input, e.DefaultTimeoutSeconds), outputLimit(input, e.MaxOutputBytes))
	return commandResultToolResult(output, result, runErr)
}

func (e *SSHExecutor) sshRemoteCommand(input map[string]any) (string, error) {
	switch e.Operation {
	case "system_info":
		return "uname -a && uptime", nil
	case "processes":
		return "ps -eo pid,ppid,pcpu,pmem,comm --sort=-pcpu | head -n 25", nil
	case "disk_usage":
		return "df -h", nil
	case "memory_usage":
		return "free -m || cat /proc/meminfo", nil
	case "logs_tail":
		path, err := tools.GetString(input, "path")
		if err != nil {
			return "", err
		}
		lines := tools.GetInt(input, "max_lines", 100)
		if lines <= 0 {
			lines = 100
		}
		return "tail -n " + strconv.Itoa(lines) + " -- " + shellQuote(path), nil
	case "service_status":
		service, err := serviceName(input)
		if err != nil {
			return "", err
		}
		return "systemctl status " + shellQuote(service) + " --no-pager -n 30", nil
	case "service_restart":
		service, err := serviceName(input)
		if err != nil {
			return "", err
		}
		return "systemctl restart " + shellQuote(service), nil
	default:
		return "", fmt.Errorf("unsupported ssh ops operation: %s", e.Operation)
	}
}

func (e *SSHExecutor) runner() CommandRunner {
	if e.Runner != nil {
		return e.Runner
	}
	return ExecCommandRunner{}
}

func sshArgs(cfg *SSHHostConfig, remoteCommand string) []string {
	port := cfg.Port
	if port == 0 {
		port = 22
	}
	args := []string{"-o", "BatchMode=yes", "-p", strconv.Itoa(port)}
	if cfg.AuthType == "key" && cfg.KeyPath != "" {
		args = append(args, "-i", cfg.KeyPath)
	}
	args = append(args, cfg.User+"@"+cfg.Host, remoteCommand)
	return args
}

// Execute runs a fixed Docker command selected by operation.
func (e *DockerExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	args, output, err := e.dockerArgs(input)
	if err != nil {
		return nil, err
	}
	result, runErr := e.runner().Run(ctx, "docker", args, timeoutFromInput(input, e.DefaultTimeoutSeconds), outputLimit(input, e.MaxOutputBytes))
	return commandResultToolResult(output, result, runErr)
}

func (e *DockerExecutor) dockerArgs(input map[string]any) ([]string, map[string]any, error) {
	output := map[string]any{"operation": e.Operation}
	switch e.Operation {
	case "ps":
		return []string{"ps", "--format", "json"}, output, nil
	case "inspect":
		container, err := containerName(input)
		if err != nil {
			return nil, nil, err
		}
		output["container"] = container
		return []string{"inspect", container}, output, nil
	case "logs":
		container, err := containerName(input)
		if err != nil {
			return nil, nil, err
		}
		lines := tools.GetInt(input, "max_lines", 200)
		if lines <= 0 {
			lines = 200
		}
		output["container"] = container
		output["max_lines"] = lines
		return []string{"logs", "--tail", strconv.Itoa(lines), container}, output, nil
	case "stats":
		return []string{"stats", "--no-stream", "--format", "json"}, output, nil
	case "restart", "stop", "start":
		container, err := containerName(input)
		if err != nil {
			return nil, nil, err
		}
		output["container"] = container
		output["rollback_plan"] = RollbackForTool("ops.docker."+e.Operation, input)
		return []string{e.Operation, container}, output, nil
	default:
		return nil, nil, fmt.Errorf("unsupported docker ops operation: %s", e.Operation)
	}
}

func (e *DockerExecutor) runner() CommandRunner {
	if e.Runner != nil {
		return e.Runner
	}
	return ExecCommandRunner{}
}

// Execute runs a fixed kubectl command selected by operation.
func (e *K8sExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	args, output, err := e.kubectlArgs(input)
	if err != nil {
		return nil, err
	}
	result, runErr := e.runner().Run(ctx, "kubectl", args, timeoutFromInput(input, e.DefaultTimeoutSeconds), outputLimit(input, e.MaxOutputBytes))
	return commandResultToolResult(output, result, runErr)
}

func (e *K8sExecutor) kubectlArgs(input map[string]any) ([]string, map[string]any, error) {
	args := e.kubeSelectionArgs(input)
	output := map[string]any{
		"operation": e.Operation,
	}
	switch e.Operation {
	case "get":
		resource, err := requiredIdentifier(input, "resource")
		if err != nil {
			return nil, nil, err
		}
		args = append(args, "get", resource)
		if name, _ := input["name"].(string); strings.TrimSpace(name) != "" {
			if err := ensureSafeIdentifier("name", name); err != nil {
				return nil, nil, err
			}
			args = append(args, name)
			output["name"] = name
		}
		output["resource"] = resource
	case "describe":
		resource, name, err := resourceName(input)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, "describe", resource, name)
		output["resource"] = resource
		output["name"] = name
	case "logs":
		target, err := tools.GetString(input, "target")
		if err != nil {
			return nil, nil, err
		}
		if err := ensureSafeIdentifier("target", target); err != nil {
			return nil, nil, err
		}
		lines := tools.GetInt(input, "max_lines", 200)
		if lines <= 0 {
			lines = 200
		}
		args = append(args, "logs", "--tail", strconv.Itoa(lines), target)
		output["target"] = target
		output["max_lines"] = lines
	case "events":
		args = append(args, "get", "events", "--sort-by=.lastTimestamp")
	case "apply":
		path, err := manifestPath(input)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, "apply", "-f", path)
		output["manifest_path"] = filepath.Base(path)
		output["manifest_summary"] = ManifestSummary(input)
		output["rollback_plan"] = RollbackForTool("ops.k8s.apply", input)
	case "delete":
		resource, name, err := resourceName(input)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, "delete", resource, name)
		output["resource"] = resource
		output["name"] = name
		output["rollback_plan"] = RollbackForTool("ops.k8s.delete", input)
	case "rollout_restart":
		resource, name, err := resourceName(input)
		if err != nil {
			return nil, nil, err
		}
		target := resource + "/" + name
		args = append(args, "rollout", "restart", target)
		output["target"] = target
		output["rollback_plan"] = RollbackForTool("ops.k8s.rollout_restart", input)
	default:
		return nil, nil, fmt.Errorf("unsupported k8s ops operation: %s", e.Operation)
	}
	if namespace, _ := input["namespace"].(string); namespace != "" {
		output["namespace"] = namespace
	}
	return args, output, nil
}

func (e *K8sExecutor) kubeSelectionArgs(input map[string]any) []string {
	if hostID, _ := input["host_id"].(string); e.Manager != nil && strings.TrimSpace(hostID) != "" {
		if host, err := e.Manager.getHostRaw(hostID); err == nil && host.K8s != nil {
			if _, ok := input["kubeconfig_path"].(string); !ok && host.K8s.KubeconfigPath != "" {
				input["kubeconfig_path"] = host.K8s.KubeconfigPath
			}
			if _, ok := input["context"].(string); !ok && host.K8s.Context != "" {
				input["context"] = host.K8s.Context
			}
			if _, ok := input["namespace"].(string); !ok && host.K8s.Namespace != "" {
				input["namespace"] = host.K8s.Namespace
			}
		}
	}
	var args []string
	if kubeconfig, _ := input["kubeconfig_path"].(string); strings.TrimSpace(kubeconfig) != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	if contextName, _ := input["context"].(string); strings.TrimSpace(contextName) != "" {
		args = append(args, "--context", contextName)
	}
	if namespace, _ := input["namespace"].(string); strings.TrimSpace(namespace) != "" {
		args = append(args, "-n", namespace)
	}
	return args
}

func (e *K8sExecutor) runner() CommandRunner {
	if e.Runner != nil {
		return e.Runner
	}
	return ExecCommandRunner{}
}

func containerName(input map[string]any) (string, error) {
	container, err := tools.GetString(input, "container")
	if err != nil {
		container, err = tools.GetString(input, "container_id")
	}
	if err != nil {
		return "", err
	}
	if err := ensureSafeIdentifier("container", container); err != nil {
		return "", err
	}
	return container, nil
}

func requiredIdentifier(input map[string]any, key string) (string, error) {
	value, err := tools.GetString(input, key)
	if err != nil {
		return "", err
	}
	if err := ensureSafeIdentifier(key, value); err != nil {
		return "", err
	}
	return value, nil
}

func resourceName(input map[string]any) (string, string, error) {
	resource, err := requiredIdentifier(input, "resource")
	if err != nil {
		return "", "", err
	}
	name, err := requiredIdentifier(input, "name")
	if err != nil {
		return "", "", err
	}
	return resource, name, nil
}

func manifestPath(input map[string]any) (string, error) {
	path, err := tools.GetString(input, "manifest_path")
	if err != nil {
		path, err = tools.GetString(input, "path")
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("manifest_path is required")
	}
	return path, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// ManifestSummary returns a redacted summary suitable for approval payloads.
func ManifestSummary(input map[string]any) map[string]any {
	out := map[string]any{}
	if path, _ := input["manifest_path"].(string); path != "" {
		out["manifest_path"] = filepath.Base(path)
	}
	if path, _ := input["path"].(string); path != "" {
		out["manifest_path"] = filepath.Base(path)
	}
	if namespace, _ := input["namespace"].(string); namespace != "" {
		out["namespace"] = namespace
	}
	if manifest, _ := input["manifest"].(string); manifest != "" {
		redacted := security.RedactString(manifest)
		lines := strings.Split(redacted, "\n")
		limit := 20
		if len(lines) < limit {
			limit = len(lines)
		}
		out["manifest_preview"] = strings.Join(lines[:limit], "\n")
		out["manifest_lines"] = len(lines)
	}
	return out
}
