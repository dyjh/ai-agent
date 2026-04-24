package tests

import (
	"testing"

	"local-agent/internal/tools/skills"
)

func TestSandboxProfileSelectionBestEffortUsesLocalRunner(t *testing.T) {
	root := createSkillFixture(t, skillFixtureOptions{
		ID:             "best_effort_skill",
		Effects:        []string{"process.read"},
		SandboxProfile: skills.SandboxProfileBestEffortLocal,
	})

	manager := skills.NewManager(t.TempDir())
	manager.SetSandboxes(&skills.LocalRestrictedRunner{})
	if _, err := manager.Upload(root, "", ""); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	validation, err := manager.Validate("best_effort_skill", map[string]any{}, 4096)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if validation.Runner != "local_restricted" {
		t.Fatalf("runner = %s, want local_restricted", validation.Runner)
	}
	if validation.SandboxProfile != string(skills.SandboxProfileBestEffortLocal) {
		t.Fatalf("sandbox_profile = %s, want %s", validation.SandboxProfile, skills.SandboxProfileBestEffortLocal)
	}
	if validation.WillFallback {
		t.Fatalf("best_effort_local should not fallback")
	}
	if validation.RequiresApproval {
		t.Fatalf("best_effort_local should not require approval from sandbox selection alone")
	}
}

func TestSandboxProfileSelectionRestrictedFallsBackToLocalWhenNeeded(t *testing.T) {
	root := createSkillFixture(t, skillFixtureOptions{
		ID:      "restricted_skill",
		Effects: []string{"process.read"},
	})

	manager := skills.NewManager(t.TempDir())
	manager.SetSandboxes(&skills.LocalRestrictedRunner{})
	if _, err := manager.Upload(root, "", ""); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	validation, err := manager.Validate("restricted_skill", map[string]any{}, 4096)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if validation.Runner != "local_restricted" {
		t.Fatalf("runner = %s, want local_restricted", validation.Runner)
	}
	if validation.SandboxProfile != string(skills.SandboxProfileBestEffortLocal) {
		t.Fatalf("sandbox_profile = %s, want %s", validation.SandboxProfile, skills.SandboxProfileBestEffortLocal)
	}
	if !validation.WillFallback {
		t.Fatalf("restricted profile should report fallback")
	}
	if !validation.RequiresApproval {
		t.Fatalf("restricted fallback should require approval")
	}
	if validation.Status != "warning" {
		t.Fatalf("status = %s, want warning", validation.Status)
	}
}

func TestLinuxIsolatedProfileRejectsWhenOnlyLocalRunnerExists(t *testing.T) {
	root := createSkillFixture(t, skillFixtureOptions{
		ID:             "linux_only_skill",
		Effects:        []string{"process.read"},
		SandboxProfile: skills.SandboxProfileLinuxIsolated,
	})

	manager := skills.NewManager(t.TempDir())
	manager.SetSandboxes(&skills.LocalRestrictedRunner{})
	if _, err := manager.Upload(root, "", ""); err == nil {
		t.Fatalf("expected linux_isolated upload to fail without a linux runner")
	}
}
