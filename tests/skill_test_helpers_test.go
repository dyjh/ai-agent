package tests

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"local-agent/internal/tools/skills"
)

type skillFixtureOptions struct {
	ID              string
	Name            string
	Effects         []string
	ApprovalDefault string
	TimeoutSeconds  int
	InputMode       string
	OutputMode      string
	RuntimeType     string
	Script          string
	InputSchema     map[string]any
}

func createSkillFixture(t *testing.T, opts skillFixtureOptions) string {
	t.Helper()

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	scriptPath := filepath.Join(binDir, "skill.sh")
	script := opts.Script
	if script == "" {
		script = "#!/bin/sh\ncat\n"
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	runtimeType := opts.RuntimeType
	if runtimeType == "" {
		runtimeType = skills.RuntimeTypeExecutable
	}
	inputMode := opts.InputMode
	if inputMode == "" {
		inputMode = skills.InputModeJSONStdin
	}
	outputMode := opts.OutputMode
	if outputMode == "" {
		outputMode = skills.OutputModeJSONStdout
	}
	timeout := opts.TimeoutSeconds
	if timeout <= 0 {
		timeout = 5
	}
	skillID := opts.ID
	if skillID == "" {
		skillID = "test_skill"
	}
	name := opts.Name
	if name == "" {
		name = "Test Skill"
	}
	inputSchema := opts.InputSchema
	if inputSchema == nil {
		inputSchema = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"top_n": map[string]any{"type": "integer"},
			},
		}
	}

	manifest := skills.Manifest{
		ID:          skillID,
		Name:        name,
		Version:     "1.0.0",
		Description: "fixture skill",
		Runtime: skills.Runtime{
			Type:           runtimeType,
			Command:        "./bin/skill.sh",
			Cwd:            ".",
			InputMode:      inputMode,
			OutputMode:     outputMode,
			TimeoutSeconds: timeout,
		},
		InputSchema: inputSchema,
		Effects:     opts.Effects,
		Approval: skills.ApprovalConfig{
			Default: opts.ApprovalDefault,
		},
	}

	data, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal(manifest) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}

	return root
}
