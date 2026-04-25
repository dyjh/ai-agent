package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"local-agent/internal/agent"
	"local-agent/internal/api"
	"local-agent/internal/api/handlers"
	"local-agent/internal/config"
	"local-agent/internal/core"
	toolscore "local-agent/internal/tools"
	"local-agent/internal/tools/ops"
)

type fakeOpsRunner struct {
	result ops.CommandResult
	err    error
	calls  [][]string
}

func (f *fakeOpsRunner) Run(_ context.Context, command string, args []string, _ time.Duration, _ int64) (ops.CommandResult, error) {
	f.calls = append(f.calls, append([]string{command}, args...))
	if len(f.result.Command) == 0 {
		f.result.Command = append([]string{command}, args...)
	}
	return f.result, f.err
}

func TestOpsHostProfileCRUD(t *testing.T) {
	manager := ops.NewManager(t.TempDir())
	created, err := manager.CreateHost(ops.HostProfileInput{
		Name: "dev",
		Type: ops.HostTypeLocal,
	})
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}
	if created.HostID == "" || created.Type != ops.HostTypeLocal {
		t.Fatalf("created host = %+v", created)
	}
	if len(manager.ListHosts()) != 2 {
		t.Fatalf("expected default local plus created host")
	}
	updated, err := manager.UpdateHost(created.HostID, ops.HostProfileInput{Name: "dev-renamed"})
	if err != nil {
		t.Fatalf("UpdateHost() error = %v", err)
	}
	if updated.Name != "dev-renamed" {
		t.Fatalf("updated name = %s", updated.Name)
	}
	testResult, err := manager.TestHost(context.Background(), created.HostID)
	if err != nil {
		t.Fatalf("TestHost() error = %v", err)
	}
	if testResult.Status != "ok" {
		t.Fatalf("test status = %s", testResult.Status)
	}
	if err := manager.DeleteHost(created.HostID); err != nil {
		t.Fatalf("DeleteHost() error = %v", err)
	}
	if _, err := manager.CreateHost(ops.HostProfileInput{Name: "bad", Type: "tenant"}); err == nil {
		t.Fatalf("expected invalid host type to fail")
	}
}

func TestOpsHostProfileAPI(t *testing.T) {
	manager := ops.NewManager(t.TempDir())
	server := httptest.NewServer(api.NewRouter(api.Dependencies{Ops: manager}))
	defer server.Close()

	body := bytes.NewBufferString(`{"name":"api-local","type":"local"}`)
	resp, err := http.Post(server.URL+"/v1/ops/hosts", "application/json", body)
	if err != nil {
		t.Fatalf("POST host error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST host status = %d", resp.StatusCode)
	}
	var created ops.HostProfile
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode host: %v", err)
	}
	if created.HostID == "" {
		t.Fatalf("created host missing id")
	}

	getResp, err := http.Get(server.URL + "/v1/ops/hosts/" + created.HostID)
	if err != nil {
		t.Fatalf("GET host error = %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET host status = %d", getResp.StatusCode)
	}
	var listed handlers.OpsHostListResponse
	listResp, err := http.Get(server.URL + "/v1/ops/hosts")
	if err != nil {
		t.Fatalf("GET hosts error = %v", err)
	}
	defer listResp.Body.Close()
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode host list: %v", err)
	}
	if len(listed.Items) < 2 {
		t.Fatalf("host list = %+v", listed.Items)
	}
}

func TestOpsLocalToolsAndSensitiveLogs(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "app.log")
	if err := os.WriteFile(logPath, []byte("line1\npassword=secret\nline3\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	systemResult, err := (&ops.LocalExecutor{Operation: "system_info"}).Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("system_info error = %v", err)
	}
	if systemResult.Output["operation"] != "system_info" {
		t.Fatalf("system_info output = %+v", systemResult.Output)
	}
	logResult, err := (&ops.LocalExecutor{Operation: "logs_tail", MaxOutputBytes: 1024}).Execute(context.Background(), map[string]any{
		"path":      logPath,
		"max_lines": 2,
	})
	if err != nil {
		t.Fatalf("logs_tail error = %v", err)
	}
	if got := logResult.Output["content"].(string); got == "" || got == "password=secret\nline3" {
		t.Fatalf("log content not redacted: %q", got)
	}

	inferrer := agent.NewEffectInferrer(config.PolicyConfig{SensitivePaths: []string{".env"}})
	inference, err := inferrer.Infer(context.Background(), core.ToolProposal{
		Tool:  "ops.local.logs_tail",
		Input: map[string]any{"path": ".env"},
	})
	if err != nil {
		t.Fatalf("Infer() error = %v", err)
	}
	if !inference.Sensitive || !inference.ApprovalRequired {
		t.Fatalf("sensitive log inference = %+v", inference)
	}
}

func TestOpsSSHReadOnlyAndWriteApproval(t *testing.T) {
	manager := ops.NewManager(t.TempDir())
	host, err := manager.CreateHost(ops.HostProfileInput{
		Name: "remote",
		Type: ops.HostTypeSSH,
		SSH:  &ops.SSHHostConfig{Host: "example.test", User: "agent", AuthType: "key", KeyPath: "/home/me/.ssh/id_rsa"},
		Metadata: map[string]string{
			"token": "secret-token",
		},
	})
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}
	if host.SSH == nil || host.SSH.KeyPath != "[REDACTED_PATH]" || host.Metadata["token"] != "[REDACTED]" {
		t.Fatalf("host secrets not redacted: %+v", host)
	}

	runner := &fakeOpsRunner{result: ops.CommandResult{Stdout: "token=abc123\nok\n", ExitCode: 0}}
	registry := toolscore.NewRegistry()
	registry.Register(core.ToolSpec{Name: "ops.ssh.processes"}, &ops.SSHExecutor{Manager: manager, Operation: "processes", Runner: runner})
	registry.Register(core.ToolSpec{Name: "ops.ssh.service_restart"}, &ops.SSHExecutor{Manager: manager, Operation: "service_restart", Runner: runner})
	router := toolscore.NewRouter(registry, agent.NewEffectInferrer(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}), agent.NewPolicyEngine(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}), agent.NewApprovalCenter(), nil)

	readOutcome, err := router.Propose(context.Background(), "run_ops", "conv_ops", core.ToolProposal{
		ID:    "tool_ssh_read",
		Tool:  "ops.ssh.processes",
		Input: map[string]any{"host_id": host.HostID},
	})
	if err != nil {
		t.Fatalf("Propose(read) error = %v", err)
	}
	if readOutcome.Approval != nil || readOutcome.Result == nil {
		t.Fatalf("ssh read outcome = %+v", readOutcome)
	}
	if readOutcome.Result.Output["stdout"].(string) == "token=abc123\nok\n" {
		t.Fatalf("ssh output was not redacted")
	}

	writeOutcome, err := router.Propose(context.Background(), "run_ops", "conv_ops", core.ToolProposal{
		ID:    "tool_ssh_write",
		Tool:  "ops.ssh.service_restart",
		Input: map[string]any{"host_id": host.HostID, "service": "nginx"},
	})
	if err != nil {
		t.Fatalf("Propose(write) error = %v", err)
	}
	if writeOutcome.Approval == nil {
		t.Fatalf("expected approval for ssh service restart")
	}
}

func TestOpsDockerAndK8sPolicyAndRedaction(t *testing.T) {
	runner := &fakeOpsRunner{result: ops.CommandResult{Stdout: "ok", ExitCode: 0}}
	registry := toolscore.NewRegistry()
	registry.Register(core.ToolSpec{Name: "ops.docker.ps"}, &ops.DockerExecutor{Operation: "ps", Runner: runner})
	registry.Register(core.ToolSpec{Name: "ops.docker.restart"}, &ops.DockerExecutor{Operation: "restart", Runner: runner})
	registry.Register(core.ToolSpec{Name: "ops.k8s.get"}, &ops.K8sExecutor{Operation: "get", Runner: runner})
	registry.Register(core.ToolSpec{Name: "ops.k8s.apply"}, &ops.K8sExecutor{Operation: "apply", Runner: runner})
	approvals := agent.NewApprovalCenter()
	router := toolscore.NewRouter(registry, agent.NewEffectInferrer(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}), agent.NewPolicyEngine(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}), approvals, nil)

	dockerRead, err := router.Propose(context.Background(), "", "", core.ToolProposal{ID: "docker_read", Tool: "ops.docker.ps", Input: map[string]any{}})
	if err != nil {
		t.Fatalf("docker read error = %v", err)
	}
	if dockerRead.Approval != nil || dockerRead.Result == nil {
		t.Fatalf("docker read outcome = %+v", dockerRead)
	}
	dockerWrite, err := router.Propose(context.Background(), "", "", core.ToolProposal{ID: "docker_write", Tool: "ops.docker.restart", Input: map[string]any{"container": "web"}})
	if err != nil {
		t.Fatalf("docker write error = %v", err)
	}
	if dockerWrite.Approval == nil || dockerWrite.Decision.ApprovalPayload["rollback_plan"] == nil {
		t.Fatalf("docker write approval payload = %+v", dockerWrite.Decision.ApprovalPayload)
	}

	k8sRead, err := router.Propose(context.Background(), "", "", core.ToolProposal{ID: "k8s_read", Tool: "ops.k8s.get", Input: map[string]any{"resource": "pods"}})
	if err != nil {
		t.Fatalf("k8s read error = %v", err)
	}
	if k8sRead.Approval != nil {
		t.Fatalf("expected k8s get to auto execute")
	}
	k8sApply, err := router.Propose(context.Background(), "", "", core.ToolProposal{ID: "k8s_apply", Tool: "ops.k8s.apply", Input: map[string]any{"manifest_path": "deploy.yaml", "manifest": "apiVersion: v1\nkind: ConfigMap\n"}})
	if err != nil {
		t.Fatalf("k8s apply error = %v", err)
	}
	if k8sApply.Approval == nil || k8sApply.Decision.ApprovalPayload["manifest_summary"] == nil {
		t.Fatalf("k8s apply approval payload = %+v", k8sApply.Decision.ApprovalPayload)
	}

	redactRunner := &fakeOpsRunner{result: ops.CommandResult{Stdout: "ok", ExitCode: 0}}
	result, err := (&ops.K8sExecutor{Operation: "get", Runner: redactRunner}).Execute(context.Background(), map[string]any{
		"resource":        "pods",
		"kubeconfig_path": "/home/me/.kube/kubeconfig",
	})
	if err != nil {
		t.Fatalf("k8s executor error = %v", err)
	}
	command := result.Output["command"].([]string)
	for _, part := range command {
		if part == "/home/me/.kube/kubeconfig" {
			t.Fatalf("kubeconfig path leaked in command: %v", command)
		}
	}

	if _, err := (&ops.DockerExecutor{Operation: "rm", Runner: runner}).Execute(context.Background(), map[string]any{"container": "web"}); err == nil {
		t.Fatalf("expected arbitrary docker operation to be rejected")
	}
}

func TestRunbookPlanDryRunAndExecuteStepRoutesToolRouter(t *testing.T) {
	dir := t.TempDir()
	content := `---
id: diagnose-local-high-cpu
title: Diagnose local high CPU
scope: ops
host_type: local
---

# Diagnose local high CPU

1. Check top processes.
2. Check memory usage.
3. Summarize likely causes.
`
	if err := os.WriteFile(filepath.Join(dir, "diagnose-local-high-cpu.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write runbook: %v", err)
	}
	manager := ops.NewManager(dir)
	plan, err := manager.PlanRunbook("diagnose-local-high-cpu", "local", true)
	if err != nil {
		t.Fatalf("PlanRunbook() error = %v", err)
	}
	if len(plan.Steps) != 3 || plan.Steps[0].Tool != "ops.local.processes" {
		t.Fatalf("plan = %+v", plan)
	}
	runner := &fakeOpsRunner{result: ops.CommandResult{Stdout: "ok", ExitCode: 0}}
	registry := toolscore.NewRegistry()
	registry.Register(core.ToolSpec{Name: "ops.local.processes"}, &ops.LocalExecutor{Operation: "processes", Runner: runner})
	router := toolscore.NewRouter(registry, agent.NewEffectInferrer(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}), agent.NewPolicyEngine(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}), agent.NewApprovalCenter(), nil)
	manager.SetRouter(router)
	stepResult, err := manager.ExecuteRunbookStep(context.Background(), plan.Steps[0], "run", "conv")
	if err != nil {
		t.Fatalf("ExecuteRunbookStep() error = %v", err)
	}
	if stepResult.Output["status"] != "routed" || stepResult.Output["result"] == nil {
		t.Fatalf("step result = %+v", stepResult.Output)
	}
	dryRun, err := (&ops.RunbookExecuteExecutor{Manager: manager}).Execute(context.Background(), map[string]any{
		"runbook_id": "diagnose-local-high-cpu",
		"dry_run":    true,
	})
	if err != nil {
		t.Fatalf("dry run error = %v", err)
	}
	if dryRun.Output["status"] != "dry_run" {
		t.Fatalf("dry run output = %+v", dryRun.Output)
	}
}

func TestPlannerOpsMapping(t *testing.T) {
	planner := agent.HeuristicPlanner{}
	cases := []struct {
		message string
		tool    string
	}{
		{message: "看一下本机 CPU 占用", tool: "ops.local.processes"},
		{message: "看一下磁盘空间", tool: "ops.local.disk_usage"},
		{message: "看一下 docker 容器状态", tool: "ops.docker.ps"},
		{message: "看一下 k8s pod `api-0` 日志", tool: "ops.k8s.logs"},
		{message: "重启服务 `nginx`", tool: "ops.local.service_restart"},
	}
	for _, tc := range cases {
		plan, err := planner.Plan(context.Background(), agent.PlanInput{UserMessage: tc.message})
		if err != nil {
			t.Fatalf("Plan(%q) error = %v", tc.message, err)
		}
		if plan.ToolProposal == nil || plan.ToolProposal.Tool != tc.tool {
			t.Fatalf("Plan(%q) tool = %#v, want %s", tc.message, plan.ToolProposal, tc.tool)
		}
	}
}
