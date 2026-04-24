package tests

import (
	"strings"
	"testing"

	"local-agent/internal/tools/skills"
)

func TestSkillValidateReportsRestrictedFallback(t *testing.T) {
	root := createSkillFixture(t, skillFixtureOptions{
		ID:      "validate_restricted_skill",
		Effects: []string{"process.read"},
	})

	manager := skills.NewManager(t.TempDir())
	manager.SetSandboxes(&skills.LocalRestrictedRunner{})
	if _, err := manager.Upload(root, "", ""); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	validation, err := manager.Validate("validate_restricted_skill", map[string]any{}, 4096)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if validation.Status != "warning" {
		t.Fatalf("status = %s, want warning", validation.Status)
	}
	if !validation.WillFallback {
		t.Fatalf("expected fallback=true")
	}
	if !validation.RequiresApproval {
		t.Fatalf("expected requires_approval=true")
	}
}

func TestSkillValidateTrustedLocalDoesNotFallback(t *testing.T) {
	root := createSkillFixture(t, skillFixtureOptions{
		ID:             "trusted_local_skill",
		Effects:        []string{"process.read"},
		SandboxProfile: skills.SandboxProfileTrustedLocal,
	})

	manager := skills.NewManager(t.TempDir())
	manager.SetSandboxes(&skills.LocalRestrictedRunner{})
	if _, err := manager.Upload(root, "", ""); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	validation, err := manager.Validate("trusted_local_skill", map[string]any{}, 4096)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if validation.WillFallback {
		t.Fatalf("trusted_local should not fallback")
	}
	if validation.RequiresApproval {
		t.Fatalf("trusted_local should not require approval from sandbox selection alone")
	}
}

func TestSkillValidateRejectsLinuxIsolatedAllowHosts(t *testing.T) {
	root := createSkillFixture(t, skillFixtureOptions{
		ID:             "linux_allow_hosts_skill",
		Effects:        []string{"network.get"},
		SandboxProfile: skills.SandboxProfileLinuxIsolated,
		Permissions: skills.PermissionsConfig{
			Network: skills.NetworkPermissions{
				Enabled:    true,
				AllowHosts: []string{"api.openai.com"},
			},
		},
	})

	manager := skills.NewManager(t.TempDir())
	if _, err := manager.Upload(root, "", ""); err == nil {
		t.Fatalf("expected upload to fail")
	} else if !strings.Contains(err.Error(), "allow_hosts") {
		t.Fatalf("error = %v, want allow_hosts enforcement error", err)
	}
}
