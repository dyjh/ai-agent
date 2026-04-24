package tests

import (
	"context"
	"os"
	"strings"
	"testing"

	"local-agent/internal/tools/skills"
)

func TestSandboxRunnerAppliesEnvAllowlist(t *testing.T) {
	t.Setenv("LANG", "zh_CN.UTF-8")
	t.Setenv("SECRET_TOKEN", "top-secret")

	root := createSkillFixture(t, skillFixtureOptions{
		ID:      "env_skill",
		Effects: []string{"process.read"},
		Script:  "#!/bin/sh\nprintf '{\"lang\":\"%s\",\"hidden\":\"%s\"}' \"$LANG\" \"$SECRET_TOKEN\"\n",
		Permissions: skills.PermissionsConfig{
			Env: skills.EnvPermissions{
				Allow: []string{"LANG"},
			},
		},
	})

	manager := skills.NewManager(t.TempDir())
	if _, err := manager.Upload(root, "", ""); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	runner := &skills.Runner{
		Manager:        manager,
		Sandbox:        &skills.LocalRestrictedRunner{},
		MaxOutputChars: 4096,
	}
	result, err := runner.Execute(context.Background(), map[string]any{
		"skill_id": "env_skill",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := result.Output["output"].(map[string]any)
	if output["lang"] != "zh_CN.UTF-8" {
		t.Fatalf("lang = %v, want zh_CN.UTF-8", output["lang"])
	}
	if output["hidden"] != "" {
		t.Fatalf("hidden = %v, want empty", output["hidden"])
	}
}

func TestSandboxRunnerAppliesMaxOutputBytes(t *testing.T) {
	root := createSkillFixture(t, skillFixtureOptions{
		ID:         "output_skill",
		Effects:    []string{"process.read"},
		OutputMode: skills.OutputModeText,
		Script: "#!/bin/sh\n" +
			"i=0\n" +
			"while [ $i -lt 256 ]; do printf 'A'; i=$((i+1)); done\n",
		Permissions: skills.PermissionsConfig{
			Process: skills.ProcessPermissions{
				MaxOutputBytes: 32,
			},
		},
	})

	manager := skills.NewManager(t.TempDir())
	if _, err := manager.Upload(root, "", ""); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	runner := &skills.Runner{
		Manager:        manager,
		Sandbox:        &skills.LocalRestrictedRunner{},
		MaxOutputChars: 4096,
	}
	result, err := runner.Execute(context.Background(), map[string]any{
		"skill_id": "output_skill",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	stdout := result.Output["stdout"].(string)
	if len(stdout) != 32 {
		t.Fatalf("stdout length = %d, want 32", len(stdout))
	}
	warnings := result.Output["warnings"].([]string)
	found := false
	for _, warning := range warnings {
		if strings.Contains(warning, "truncated") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want truncation warning", warnings)
	}
}

func TestSandboxRunnerRejectsRuntimePathOutsideAllowedScope(t *testing.T) {
	root := createSkillFixture(t, skillFixtureOptions{
		ID:      "bad_command",
		Effects: []string{"process.read"},
	})
	if err := os.WriteFile(skills.ManifestPath(root), []byte(strings.TrimSpace(`
id: bad_command
version: 1.0.0
runtime:
  type: executable
  command: /tmp/outside.sh
`)), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}

	manager := skills.NewManager(t.TempDir())
	if _, err := manager.Upload(root, "", ""); err == nil {
		t.Fatalf("expected runtime path validation error")
	}
}
