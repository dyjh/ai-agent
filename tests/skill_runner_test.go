package tests

import (
	"context"
	"strings"
	"testing"

	"local-agent/internal/tools/skills"
)

func TestSkillRunnerExecutesExecutableSkill(t *testing.T) {
	root := createSkillFixture(t, skillFixtureOptions{
		ID:      "echo_skill",
		Effects: []string{"process.read", "system.metrics.read"},
		Script: "#!/bin/sh\n" +
			"input=$(cat)\n" +
			"printf '{\"received\":%s,\"api_key\":\"super-secret\"}' \"$input\"\n",
	})

	manager := skills.NewManager(t.TempDir())
	if _, err := manager.Upload(root, "", ""); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	runner := &skills.Runner{Manager: manager, MaxOutputChars: 4096}
	result, err := runner.Execute(context.Background(), map[string]any{
		"skill_id": "echo_skill",
		"args": map[string]any{
			"top_n": 3,
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Output["status"] != "ok" {
		t.Fatalf("status = %v, want ok", result.Output["status"])
	}

	output, ok := result.Output["output"].(map[string]any)
	if !ok {
		t.Fatalf("output has unexpected type %T", result.Output["output"])
	}
	if output["api_key"] != "[REDACTED]" {
		t.Fatalf("api_key = %v, want [REDACTED]", output["api_key"])
	}
	received, ok := output["received"].(map[string]any)
	if !ok {
		t.Fatalf("received has unexpected type %T", output["received"])
	}
	if received["top_n"] != float64(3) {
		t.Fatalf("received.top_n = %v, want 3", received["top_n"])
	}

	stdout, _ := result.Output["stdout"].(string)
	if !strings.Contains(stdout, "[REDACTED]") {
		t.Fatalf("stdout should be redacted, got %q", stdout)
	}
}

func TestSkillRunnerRejectsDisabledSkill(t *testing.T) {
	root := createSkillFixture(t, skillFixtureOptions{
		ID:      "disabled_skill",
		Effects: []string{"process.read"},
	})

	manager := skills.NewManager(t.TempDir())
	if _, err := manager.Upload(root, "", ""); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if _, err := manager.SetEnabled("disabled_skill", false); err != nil {
		t.Fatalf("SetEnabled() error = %v", err)
	}

	runner := &skills.Runner{Manager: manager, MaxOutputChars: 4096}
	result, err := runner.Execute(context.Background(), map[string]any{
		"skill_id": "disabled_skill",
	})
	if err == nil {
		t.Fatalf("expected disabled skill error")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("error = %v, want disabled", err)
	}
	if result == nil || result.Output["status"] != "error" {
		t.Fatalf("expected error result, got %#v", result)
	}
}

func TestSkillRunnerEnforcesTimeout(t *testing.T) {
	root := createSkillFixture(t, skillFixtureOptions{
		ID:             "slow_skill",
		Effects:        []string{"process.read"},
		TimeoutSeconds: 1,
		Script:         "#!/bin/sh\nsleep 2\nprintf '{\"ok\":true}'\n",
	})

	manager := skills.NewManager(t.TempDir())
	if _, err := manager.Upload(root, "", ""); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	runner := &skills.Runner{Manager: manager, MaxOutputChars: 4096}
	result, err := runner.Execute(context.Background(), map[string]any{
		"skill_id": "slow_skill",
	})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if result == nil {
		t.Fatalf("expected timeout result")
	}
	if result.Output["status"] != "timeout" {
		t.Fatalf("status = %v, want timeout", result.Output["status"])
	}
}
