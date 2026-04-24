//go:build linux

package skills

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// LinuxIsolatedRunner provides a stronger Linux-only execution path based on
// chroot plus dedicated Linux namespaces.
type LinuxIsolatedRunner struct{}

// Name returns the stable runner identifier.
func (r *LinuxIsolatedRunner) Name() string {
	return "linux_isolated"
}

// Supports reports the profiles that may execute through the Linux isolated runner.
func (r *LinuxIsolatedRunner) Supports(profile SandboxProfile) bool {
	switch profile {
	case SandboxProfileRestricted, SandboxProfileLinuxIsolated:
		return true
	default:
		return false
	}
}

// Run executes the request inside a Linux chroot and isolated namespace set.
func (r *LinuxIsolatedRunner) Run(ctx context.Context, req SandboxRequest) (*SandboxResult, error) {
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

	stdout := &limitedBuffer{limit: maxOutput}
	stderr := &limitedBuffer{limit: maxOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	namespaces := []string{"user", "mount", "pid", "ipc", "uts"}
	cloneFlags := uintptr(syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS | syscall.CLONE_NEWPID | syscall.CLONE_NEWIPC | syscall.CLONE_NEWUTS)
	if !req.Profile.NetworkEnabled {
		cloneFlags |= syscall.CLONE_NEWNET
		namespaces = append(namespaces, "network")
	}

	sysProcAttr := &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: 0,
			Gid: 0,
		},
		Cloneflags: cloneFlags,
		UidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0,
			HostID:      os.Getuid(),
			Size:        1,
		}},
		GidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0,
			HostID:      os.Getgid(),
			Size:        1,
		}},
		GidMappingsEnableSetgroups: false,
		Pdeathsig:                  syscall.SIGKILL,
		Setpgid:                    true,
	}
	if req.Profile.FilesystemEnforced {
		if strings.TrimSpace(req.Profile.RootFS) == "" {
			return nil, fmt.Errorf("linux isolated sandbox requires profile.rootfs when filesystem enforcement is enabled")
		}
		sysProcAttr.Chroot = req.Profile.RootFS
	}
	cmd.SysProcAttr = sysProcAttr

	startedAt := time.Now().UTC()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start linux isolated sandbox: %w", err)
	}

	metadata := map[string]any{
		"runner":           r.Name(),
		"strong_isolation": true,
		"chroot":           req.Profile.FilesystemEnforced,
		"namespaces":       namespaces,
	}
	if req.Profile.FilesystemEnforced {
		metadata["rootfs"] = req.Profile.RootFS
	}
	if err := applyLinuxRlimits(cmd.Process.Pid, req.Profile, metadata); err != nil {
		killProcessGroup(cmd)
		return nil, fmt.Errorf("apply linux sandbox limits: %w", err)
	}

	runErr := waitStartedProcess(runCtx, cmd)
	finishedAt := time.Now().UTC()
	result := &SandboxResult{
		Stdout:     stdout.Bytes(),
		Stderr:     stderr.Bytes(),
		ExitCode:   sandboxExitCode(runErr),
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		Warnings:   append([]string(nil), req.Profile.Warnings...),
		Metadata:   metadata,
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

func applyLinuxRlimits(pid int, profile ExecutionProfile, metadata map[string]any) error {
	applied := map[string]any{}

	if profile.MaxProcesses > 0 {
		limit := &unix.Rlimit{Cur: uint64(profile.MaxProcesses), Max: uint64(profile.MaxProcesses)}
		if err := unix.Prlimit(pid, unix.RLIMIT_NPROC, limit, nil); err != nil {
			return err
		}
		applied["max_processes"] = profile.MaxProcesses
	}
	if profile.MemoryLimitMB > 0 {
		bytes := uint64(profile.MemoryLimitMB) << 20
		limit := &unix.Rlimit{Cur: bytes, Max: bytes}
		if err := unix.Prlimit(pid, unix.RLIMIT_AS, limit, nil); err != nil {
			return err
		}
		applied["memory_limit_mb"] = profile.MemoryLimitMB
	}
	if len(applied) > 0 {
		metadata["rlimits"] = applied
	}
	return nil
}

func linuxIsolationSupportError() error {
	if _, err := os.Stat("/proc/self/ns/user"); err != nil {
		return fmt.Errorf("linux user namespaces are unavailable")
	}

	if os.Geteuid() != 0 {
		enabled, err := readProcToggle("/proc/sys/kernel/unprivileged_userns_clone")
		if err == nil && !enabled {
			return fmt.Errorf("unprivileged user namespaces are disabled")
		}
	}

	maxNamespaces, err := readProcInt("/proc/sys/user/max_user_namespaces")
	if err == nil && maxNamespaces == 0 {
		return fmt.Errorf("user namespaces are disabled by /proc/sys/user/max_user_namespaces")
	}

	return nil
}

func readProcToggle(path string) (bool, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(string(value)) {
	case "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, fmt.Errorf("unexpected boolean value in %s", path)
	}
}

func readProcInt(path string) (int64, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(value)), 10, 64)
}
