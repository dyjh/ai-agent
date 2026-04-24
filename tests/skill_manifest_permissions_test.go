package tests

import (
	"strings"
	"testing"

	"local-agent/internal/tools/skills"
)

func TestSkillManifestDefaultsUseConservativePermissions(t *testing.T) {
	manifest := skills.Manifest{
		ID:      "cpu_analyzer",
		Version: "1.0.0",
		Runtime: skills.Runtime{
			Type:    skills.RuntimeTypeExecutable,
			Command: "./bin/cpu_analyzer",
		},
	}

	if err := manifest.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if len(manifest.Permissions.Filesystem.Read) != 1 || manifest.Permissions.Filesystem.Read[0] != "." {
		t.Fatalf("default read paths = %v, want [.] ", manifest.Permissions.Filesystem.Read)
	}
	if len(manifest.Permissions.Filesystem.Write) != 0 {
		t.Fatalf("default write paths = %v, want none", manifest.Permissions.Filesystem.Write)
	}
	if manifest.Permissions.Network.Enabled {
		t.Fatalf("default network permission should be disabled")
	}
	if manifest.Sandbox.Profile != skills.SandboxProfileRestricted {
		t.Fatalf("sandbox.profile = %s, want %s", manifest.Sandbox.Profile, skills.SandboxProfileRestricted)
	}
	if manifest.Permissions.Process.MaxRuntimeSeconds <= 0 || manifest.Permissions.Process.MaxOutputBytes <= 0 {
		t.Fatalf("process defaults should be populated: %#v", manifest.Permissions.Process)
	}
}

func TestSkillManifestRejectsInvalidSandboxProfile(t *testing.T) {
	manifest := skills.Manifest{
		ID:      "bad_sandbox",
		Version: "1.0.0",
		Runtime: skills.Runtime{
			Type:    skills.RuntimeTypeExecutable,
			Command: "./bin/skill.sh",
		},
		Sandbox: skills.SandboxConfig{
			Profile: "super_locked",
		},
	}

	err := manifest.Normalize()
	if err == nil {
		t.Fatalf("expected sandbox validation error")
	}
	if !strings.Contains(err.Error(), "sandbox.profile") {
		t.Fatalf("error = %v, want sandbox.profile", err)
	}
}

func TestSkillManifestRejectsWritePermissionWithoutWriteEffect(t *testing.T) {
	manifest := skills.Manifest{
		ID:      "bad_write",
		Version: "1.0.0",
		Runtime: skills.Runtime{
			Type:    skills.RuntimeTypeExecutable,
			Command: "./bin/skill.sh",
		},
		Effects: []string{"process.read"},
		Permissions: skills.PermissionsConfig{
			Filesystem: skills.FilesystemPermissions{
				Write: []string{"./workspace"},
			},
		},
	}

	err := manifest.Normalize()
	if err == nil {
		t.Fatalf("expected write/effect mismatch")
	}
	if !strings.Contains(err.Error(), "write-related effect") {
		t.Fatalf("error = %v, want write-related effect mismatch", err)
	}
}

func TestSkillManifestRejectsNetworkPermissionWithoutNetworkEffect(t *testing.T) {
	manifest := skills.Manifest{
		ID:      "bad_network",
		Version: "1.0.0",
		Runtime: skills.Runtime{
			Type:    skills.RuntimeTypeExecutable,
			Command: "./bin/skill.sh",
		},
		Effects: []string{"process.read"},
		Permissions: skills.PermissionsConfig{
			Network: skills.NetworkPermissions{
				Enabled:    true,
				AllowHosts: []string{"api.openai.com"},
			},
		},
	}

	err := manifest.Normalize()
	if err == nil {
		t.Fatalf("expected network/effect mismatch")
	}
	if !strings.Contains(err.Error(), "network.* effect") {
		t.Fatalf("error = %v, want network effect mismatch", err)
	}
}
