package tests

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"local-agent/internal/tools/skills"
)

type skillFixtureOptions struct {
	ID              string
	Name            string
	Version         string
	Effects         []string
	ApprovalDefault string
	TimeoutSeconds  int
	InputMode       string
	OutputMode      string
	RuntimeType     string
	Script          string
	InputSchema     map[string]any
	Permissions     skills.PermissionsConfig
	SandboxProfile  string
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
		Version:     valueOrDefault(opts.Version, "1.0.0"),
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
		Permissions: opts.Permissions,
		Approval: skills.ApprovalConfig{
			Default: opts.ApprovalDefault,
		},
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

func createSkillZipFromDir(t *testing.T, root string) string {
	t.Helper()

	zipPath := filepath.Join(t.TempDir(), "skill.zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("Create(zip) error = %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		header.Method = zip.Deflate
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		if _, err := io.Copy(entry, src); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk(zip) error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close(zip) error = %v", err)
	}
	return zipPath
}

func createZipWithEntries(t *testing.T, entries map[string]string) string {
	t.Helper()

	zipPath := filepath.Join(t.TempDir(), "entries.zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("Create(zip) error = %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	for name, body := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("Create(entry) error = %v", err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatalf("Write(entry) error = %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close(zip) error = %v", err)
	}
	return zipPath
}

func valueOrDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
