package ops

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/security"
	"local-agent/internal/tools"
)

// LocalExecutor implements fixed local operations tools.
type LocalExecutor struct {
	Operation             string
	Runner                CommandRunner
	DefaultTimeoutSeconds int
	MaxOutputBytes        int64
}

// Execute runs a local read-only or approved local write operation.
func (e *LocalExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	switch e.Operation {
	case "system_info":
		return e.systemInfo(input), nil
	case "processes":
		return e.fixedCommand(ctx, input, map[string]any{"operation": e.Operation}, "ps", []string{"-eo", "pid,ppid,pcpu,pmem,comm", "--sort=-pcpu"})
	case "disk_usage":
		return e.fixedCommand(ctx, input, map[string]any{"operation": e.Operation}, "df", []string{"-h"})
	case "memory_usage":
		return e.memoryUsage(ctx, input)
	case "network_info":
		return e.networkInfo(ctx, input)
	case "service_status":
		service, err := serviceName(input)
		if err != nil {
			return nil, err
		}
		return e.fixedCommand(ctx, input, map[string]any{"operation": e.Operation, "service": service}, "systemctl", []string{"status", service, "--no-pager", "-n", "30"})
	case "logs_tail":
		return e.logsTail(input)
	case "service_restart":
		service, err := serviceName(input)
		if err != nil {
			return nil, err
		}
		return e.fixedCommand(ctx, input, map[string]any{"operation": e.Operation, "service": service, "rollback_plan": RollbackForTool("ops.local.service_restart", input)}, "systemctl", []string{"restart", service})
	default:
		return nil, fmt.Errorf("unsupported local ops operation: %s", e.Operation)
	}
}

func (e *LocalExecutor) systemInfo(input map[string]any) *core.ToolResult {
	hostname, _ := os.Hostname()
	uptime := readFirstLine("/proc/uptime")
	return &core.ToolResult{
		Output: map[string]any{
			"operation": "system_info",
			"hostname":  security.RedactString(hostname),
			"os":        runtime.GOOS,
			"arch":      runtime.GOARCH,
			"uptime":    security.RedactString(uptime),
			"truncated": false,
			"limit":     outputLimit(input, e.MaxOutputBytes),
		},
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
	}
}

func (e *LocalExecutor) memoryUsage(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err == nil {
		lines := firstLines(string(data), 12)
		return &core.ToolResult{
			Output: map[string]any{
				"operation": "memory_usage",
				"source":    "/proc/meminfo",
				"content":   trimAndRedact(strings.Join(lines, "\n"), outputLimit(input, e.MaxOutputBytes)),
			},
			StartedAt:  time.Now().UTC(),
			FinishedAt: time.Now().UTC(),
		}, nil
	}
	return e.fixedCommand(ctx, input, map[string]any{"operation": e.Operation}, "free", []string{"-m"})
}

func (e *LocalExecutor) networkInfo(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return e.fixedCommand(ctx, input, map[string]any{"operation": e.Operation}, "ip", []string{"addr", "show"})
	}
	interfaces := make([]map[string]any, 0)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, ":") || strings.HasPrefix(line, "Inter-|") || strings.HasPrefix(line, "face |") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}
		interfaces = append(interfaces, map[string]any{
			"name":      strings.TrimSpace(parts[0]),
			"rx_bytes":  fields[0],
			"rx_errors": fields[2],
			"tx_bytes":  fields[8],
			"tx_errors": fields[10],
		})
	}
	sort.Slice(interfaces, func(i, j int) bool {
		return fmt.Sprint(interfaces[i]["name"]) < fmt.Sprint(interfaces[j]["name"])
	})
	return &core.ToolResult{
		Output: map[string]any{
			"operation":  "network_info",
			"interfaces": interfaces,
		},
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
	}, scanner.Err()
}

func (e *LocalExecutor) logsTail(input map[string]any) (*core.ToolResult, error) {
	path, err := tools.GetString(input, "path")
	if err != nil {
		return nil, err
	}
	maxBytes := outputLimit(input, e.MaxOutputBytes)
	if maxBytes <= 0 {
		maxBytes = defaultOpsMaxOutputBytes
	}
	data, truncated, err := readTailBytes(path, maxBytes)
	if err != nil {
		return nil, err
	}
	maxLines := tools.GetInt(input, "max_lines", 100)
	if maxLines <= 0 {
		maxLines = 100
	}
	content := strings.Join(lastLines(string(data), maxLines), "\n")
	return &core.ToolResult{
		Output: map[string]any{
			"operation": "logs_tail",
			"path":      path,
			"content":   trimAndRedact(content, maxBytes),
			"truncated": truncated,
			"max_lines": maxLines,
		},
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
	}, nil
}

func (e *LocalExecutor) fixedCommand(ctx context.Context, input map[string]any, output map[string]any, command string, args []string) (*core.ToolResult, error) {
	runner := e.Runner
	if runner == nil {
		runner = ExecCommandRunner{}
	}
	result, err := runner.Run(ctx, command, args, timeoutFromInput(input, e.DefaultTimeoutSeconds), outputLimit(input, e.MaxOutputBytes))
	return commandResultToolResult(output, result, err)
}

func serviceName(input map[string]any) (string, error) {
	service, err := tools.GetString(input, "service")
	if err != nil {
		service, err = tools.GetString(input, "service_name")
	}
	if err != nil {
		return "", err
	}
	service = strings.TrimSpace(service)
	if err := ensureSafeIdentifier("service", service); err != nil {
		return "", err
	}
	return service, nil
}

func readFirstLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	return line
}

func readTailBytes(path string, maxBytes int64) ([]byte, bool, error) {
	if strings.TrimSpace(path) == "" {
		return nil, false, errors.New("path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if info.IsDir() {
		return nil, false, fmt.Errorf("path is a directory: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	truncated := info.Size() > maxBytes
	if truncated {
		if _, err := file.Seek(-maxBytes, 2); err != nil {
			return nil, false, err
		}
	}
	data := make([]byte, maxBytes)
	n, err := file.Read(data)
	if n == 0 && err != nil && info.Size() == 0 {
		return []byte{}, truncated, nil
	}
	if err != nil && n == 0 {
		return nil, false, err
	}
	return data[:n], truncated, nil
}

func firstLines(value string, limit int) []string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	if len(lines) <= limit {
		return lines
	}
	return lines[:limit]
}

func lastLines(value string, limit int) []string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	if len(lines) <= limit {
		return lines
	}
	return lines[len(lines)-limit:]
}
