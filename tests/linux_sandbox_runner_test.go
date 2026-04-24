package tests

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"local-agent/internal/tools/skills"
)

func TestLinuxSandboxRunnerAppliesEnvAllowlistAndMetadata(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux isolated sandbox only runs on linux")
	}

	t.Setenv("LANG", "zh_CN.UTF-8")
	t.Setenv("SECRET_TOKEN", "top-secret")

	root := createGoBinarySkillFixture(t, goBinarySkillOptions{
		ID:      "linux_env_skill",
		Effects: []string{"process.read"},
		Source: `package main
import (
	"encoding/json"
	"io"
	"os"
)
func main() {
	data, _ := io.ReadAll(os.Stdin)
	var input any
	_ = json.Unmarshal(data, &input)
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"lang": os.Getenv("LANG"),
		"hidden": os.Getenv("SECRET_TOKEN"),
		"input": input,
	})
}
`,
		Permissions: skills.PermissionsConfig{
			Env: skills.EnvPermissions{
				Allow: []string{"LANG"},
			},
		},
		SandboxProfile: skills.SandboxProfileLinuxIsolated,
	})

	manager := skills.NewManager(t.TempDir())
	if _, err := manager.Upload(root, "", ""); err != nil {
		skipUnsupportedLinuxSandbox(t, err)
		t.Fatalf("Upload() error = %v", err)
	}

	runner := &skills.Runner{Manager: manager, MaxOutputChars: 4096}
	result, err := runner.Execute(context.Background(), map[string]any{
		"skill_id": "linux_env_skill",
		"args": map[string]any{
			"top_n": 3,
		},
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

	metadata := result.Output["metadata"].(map[string]any)
	if metadata["runner"] != "linux_isolated" {
		t.Fatalf("metadata.runner = %v, want linux_isolated", metadata["runner"])
	}
	profile := result.Output["execution_profile"].(skills.ExecutionProfile)
	if profile.Runner != "linux_isolated" {
		t.Fatalf("execution_profile.runner = %s, want linux_isolated", profile.Runner)
	}
	if !profile.StrongIsolation {
		t.Fatalf("expected strong_isolation=true")
	}
}

func TestLinuxSandboxRunnerAppliesMaxOutputBytes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux isolated sandbox only runs on linux")
	}

	root := createGoBinarySkillFixture(t, goBinarySkillOptions{
		ID:      "linux_output_skill",
		Effects: []string{"process.read"},
		Source: `package main
import (
	"fmt"
	"strings"
)
func main() {
	fmt.Print(strings.Repeat("A", 256))
}
`,
		OutputMode: skills.OutputModeText,
		Permissions: skills.PermissionsConfig{
			Process: skills.ProcessPermissions{
				MaxOutputBytes: 32,
			},
		},
		SandboxProfile: skills.SandboxProfileLinuxIsolated,
	})

	manager := skills.NewManager(t.TempDir())
	if _, err := manager.Upload(root, "", ""); err != nil {
		skipUnsupportedLinuxSandbox(t, err)
		t.Fatalf("Upload() error = %v", err)
	}

	runner := &skills.Runner{Manager: manager, MaxOutputChars: 4096}
	result, err := runner.Execute(context.Background(), map[string]any{
		"skill_id": "linux_output_skill",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	stdout := result.Output["stdout"].(string)
	if len(stdout) != 32 {
		t.Fatalf("stdout length = %d, want 32", len(stdout))
	}
}

func TestLinuxSandboxRunnerHonorsTimeout(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux isolated sandbox only runs on linux")
	}

	root := createGoBinarySkillFixture(t, goBinarySkillOptions{
		ID:      "linux_timeout_skill",
		Effects: []string{"process.read"},
		Source: `package main
import (
	"fmt"
	"time"
)
func main() {
	time.Sleep(2 * time.Second)
	fmt.Print("ok")
}
`,
		TimeoutSeconds: 1,
		SandboxProfile: skills.SandboxProfileLinuxIsolated,
	})

	manager := skills.NewManager(t.TempDir())
	if _, err := manager.Upload(root, "", ""); err != nil {
		skipUnsupportedLinuxSandbox(t, err)
		t.Fatalf("Upload() error = %v", err)
	}

	runner := &skills.Runner{Manager: manager, MaxOutputChars: 4096}
	result, err := runner.Execute(context.Background(), map[string]any{
		"skill_id": "linux_timeout_skill",
	})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if result.Output["status"] != "timeout" {
		t.Fatalf("status = %v, want timeout", result.Output["status"])
	}
}

type goBinarySkillOptions struct {
	ID             string
	Effects        []string
	Source         string
	Permissions    skills.PermissionsConfig
	SandboxProfile skills.SandboxProfile
	OutputMode     string
	TimeoutSeconds int
}

func createGoBinarySkillFixture(t *testing.T, opts goBinarySkillOptions) string {
	t.Helper()

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	srcPath := filepath.Join(root, "main.go")
	if err := os.WriteFile(srcPath, []byte(opts.Source), 0o644); err != nil {
		t.Fatalf("WriteFile(main.go) error = %v", err)
	}

	binPath := filepath.Join(binDir, "skill")
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build error = %v output = %s", err, string(output))
	}

	outputMode := opts.OutputMode
	if outputMode == "" {
		outputMode = skills.OutputModeJSONStdout
	}
	timeout := opts.TimeoutSeconds
	if timeout <= 0 {
		timeout = 5
	}
	manifest := skills.Manifest{
		ID:          opts.ID,
		Name:        opts.ID,
		Version:     "1.0.0",
		Description: "linux isolated binary fixture",
		Runtime: skills.Runtime{
			Type:           skills.RuntimeTypeExecutable,
			Command:        "./bin/skill",
			Cwd:            ".",
			InputMode:      skills.InputModeJSONStdin,
			OutputMode:     outputMode,
			TimeoutSeconds: timeout,
		},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"top_n": map[string]any{"type": "integer"},
			},
		},
		Effects:     opts.Effects,
		Permissions: opts.Permissions,
		Sandbox: skills.SandboxConfig{
			Profile: opts.SandboxProfile,
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

func skipUnsupportedLinuxSandbox(t *testing.T, err error) {
	t.Helper()
	text := err.Error()
	if strings.Contains(text, "user namespaces") || strings.Contains(text, "only available on linux") {
		t.Skipf("linux isolated sandbox unsupported in this environment: %v", err)
	}
}
