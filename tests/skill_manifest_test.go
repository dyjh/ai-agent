package tests

import (
	"strings"
	"testing"

	"local-agent/internal/tools/skills"
)

func TestSkillManifestDefaultsUnknownEffect(t *testing.T) {
	manifest := skills.Manifest{
		ID:      "cpu_analyzer",
		Name:    "CPU Analyzer",
		Version: "1.0.0",
		Runtime: skills.Runtime{
			Type:    skills.RuntimeTypeExecutable,
			Command: "./bin/cpu_analyzer",
		},
		InputSchema: map[string]any{
			"type": "object",
		},
	}

	if err := manifest.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if len(manifest.Effects) != 1 || manifest.Effects[0] != "unknown.effect" {
		t.Fatalf("effects = %v, want [unknown.effect]", manifest.Effects)
	}
	if manifest.Runtime.InputMode != skills.InputModeJSONStdin {
		t.Fatalf("input_mode = %s, want %s", manifest.Runtime.InputMode, skills.InputModeJSONStdin)
	}
	if manifest.Runtime.OutputMode != skills.OutputModeJSONStdout {
		t.Fatalf("output_mode = %s, want %s", manifest.Runtime.OutputMode, skills.OutputModeJSONStdout)
	}
}

func TestSkillManifestRequiresFields(t *testing.T) {
	manifest := skills.Manifest{
		Name: "missing-id",
		Runtime: skills.Runtime{
			Type:    skills.RuntimeTypeExecutable,
			Command: "./bin/skill",
		},
	}

	err := manifest.Normalize()
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "skill id") {
		t.Fatalf("error = %v, want missing skill id", err)
	}
}

func TestSkillManifestInvalidRuntime(t *testing.T) {
	manifest := skills.Manifest{
		ID:      "cpu_analyzer",
		Version: "1.0.0",
		Runtime: skills.Runtime{
			Type:    "invalid",
			Command: "./bin/cpu_analyzer",
		},
	}

	err := manifest.Normalize()
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "unsupported skill runtime.type") {
		t.Fatalf("error = %v, want unsupported runtime", err)
	}
}
