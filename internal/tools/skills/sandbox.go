package skills

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"syscall"
	"time"
)

// ExecutionProfile captures the effective local execution limits applied to a skill.
type ExecutionProfile struct {
	RequestedSandboxProfile string   `json:"requested_sandbox_profile,omitempty"`
	SandboxProfile          string   `json:"sandbox_profile"`
	Runner                  string   `json:"runner"`
	Platform                string   `json:"platform,omitempty"`
	PlatformSupported       bool     `json:"platform_supported"`
	WillFallback            bool     `json:"will_fallback"`
	RequiresApproval        bool     `json:"requires_approval"`
	StrongIsolation         bool     `json:"strong_isolation"`
	FilesystemEnforced      bool     `json:"filesystem_enforced"`
	NetworkEnforced         bool     `json:"network_enforced"`
	AllowedReadPaths        []string `json:"allowed_read_paths,omitempty"`
	AllowedWritePaths       []string `json:"allowed_write_paths,omitempty"`
	NetworkEnabled          bool     `json:"network_enabled"`
	AllowedHosts            []string `json:"allowed_hosts,omitempty"`
	AllowedEnv              []string `json:"allowed_env,omitempty"`
	TimeoutSeconds          int      `json:"timeout_seconds"`
	MaxOutputBytes          int64    `json:"max_output_bytes"`
	MaxProcesses            int      `json:"max_processes,omitempty"`
	MemoryLimitMB           int      `json:"memory_limit_mb,omitempty"`
	RootFS                  string   `json:"-"`
	Warnings                []string `json:"warnings,omitempty"`
}

// SandboxRequest is the normalized execution request submitted to a sandbox runner.
type SandboxRequest struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	CWD     string            `json:"cwd"`
	Env     map[string]string `json:"env,omitempty"`
	Stdin   []byte            `json:"stdin,omitempty"`
	Profile ExecutionProfile  `json:"profile"`
}

// SandboxResult is the normalized execution result returned by a sandbox runner.
type SandboxResult struct {
	Stdout     []byte         `json:"stdout,omitempty"`
	Stderr     []byte         `json:"stderr,omitempty"`
	ExitCode   int            `json:"exit_code"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
	Warnings   []string       `json:"warnings,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// SandboxRunner executes a prepared local skill command.
type SandboxRunner interface {
	Run(ctx context.Context, req SandboxRequest) (*SandboxResult, error)
	Name() string
	Supports(profile SandboxProfile) bool
}

// LocalRestrictedRunner is a best-effort local runner with explicit validation and IO limits.
type LocalRestrictedRunner struct{}

// Name returns the stable runner identifier.
func (r *LocalRestrictedRunner) Name() string {
	return "local_restricted"
}

// Supports reports the profiles that may execute through the best-effort local runner.
func (r *LocalRestrictedRunner) Supports(profile SandboxProfile) bool {
	switch profile {
	case SandboxProfileBestEffortLocal, SandboxProfileTrustedLocal:
		return true
	default:
		return false
	}
}

// Run executes the request with timeout, bounded output capture, and explicit environment control.
func (r *LocalRestrictedRunner) Run(ctx context.Context, req SandboxRequest) (*SandboxResult, error) {
	if req.Command == "" {
		return nil, fmt.Errorf("sandbox command is required")
	}
	if req.CWD == "" {
		return nil, fmt.Errorf("sandbox cwd is required")
	}
	timeout := req.Profile.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	maxOutput := req.Profile.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = defaultProcessMaxOutputBytes
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, req.Command, req.Args...)
	cmd.Dir = req.CWD
	cmd.Env = envMapToSlice(req.Env)
	cmd.Stdin = bytes.NewReader(req.Stdin)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}

	stdout := &limitedBuffer{limit: maxOutput}
	stderr := &limitedBuffer{limit: maxOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	startedAt := time.Now().UTC()
	runErr := startAndWait(runCtx, cmd)
	finishedAt := time.Now().UTC()
	result := &SandboxResult{
		Stdout:     stdout.Bytes(),
		Stderr:     stderr.Bytes(),
		ExitCode:   sandboxExitCode(runErr),
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		Warnings:   append([]string(nil), req.Profile.Warnings...),
		Metadata: map[string]any{
			"runner":           r.Name(),
			"strong_isolation": false,
		},
	}
	if stdout.Truncated() {
		result.Warnings = append(result.Warnings, fmt.Sprintf("stdout truncated at %d bytes", maxOutput))
	}
	if stderr.Truncated() {
		result.Warnings = append(result.Warnings, fmt.Sprintf("stderr truncated at %d bytes", maxOutput))
	}

	if runCtx.Err() == context.DeadlineExceeded {
		return result, fmt.Errorf("skill timed out after %ds", timeout)
	}
	return result, runErr
}

func startAndWait(ctx context.Context, cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	return waitStartedProcess(ctx, cmd)
}

func waitStartedProcess(ctx context.Context, cmd *exec.Cmd) error {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killProcessGroup(cmd)
		case <-done:
		}
	}()

	err := cmd.Wait()
	close(done)
	return err
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}

type limitedBuffer struct {
	limit     int64
	buffer    bytes.Buffer
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	originalLen := len(p)
	if b.limit <= 0 {
		b.truncated = true
		return originalLen, nil
	}
	remaining := b.limit - int64(b.buffer.Len())
	if remaining <= 0 {
		b.truncated = true
		return originalLen, nil
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, err := b.buffer.Write(p)
	if err != nil {
		return 0, err
	}
	return originalLen, nil
}

func (b *limitedBuffer) Bytes() []byte {
	return append([]byte(nil), b.buffer.Bytes()...)
}

func (b *limitedBuffer) Truncated() bool {
	return b.truncated
}

func envMapToSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func sandboxExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if errors.Is(err, io.EOF) {
		return 0
	}
	return -1
}
