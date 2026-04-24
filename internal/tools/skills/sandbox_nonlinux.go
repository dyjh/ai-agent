//go:build !linux

package skills

import (
	"context"
	"fmt"
)

// LinuxIsolatedRunner is unavailable outside Linux.
type LinuxIsolatedRunner struct{}

// Name returns the stable runner identifier.
func (r *LinuxIsolatedRunner) Name() string {
	return "linux_isolated"
}

// Supports reports whether the runner can satisfy the requested profile.
func (r *LinuxIsolatedRunner) Supports(profile SandboxProfile) bool {
	switch profile {
	case SandboxProfileRestricted, SandboxProfileLinuxIsolated:
		return false
	default:
		return false
	}
}

// Run always fails outside Linux.
func (r *LinuxIsolatedRunner) Run(_ context.Context, _ SandboxRequest) (*SandboxResult, error) {
	return nil, fmt.Errorf("linux isolated sandbox is only available on linux")
}

func linuxIsolationSupportError() error {
	return fmt.Errorf("linux isolated sandbox is only available on linux")
}
