package tests

import (
	"context"
	"strings"
	"testing"

	"local-agent/internal/tools/skills"
)

func TestSkillRunnerRejectsWriteArgOutsideDeclaredWritePaths(t *testing.T) {
	root := createSkillFixture(t, skillFixtureOptions{
		ID:      "bounded_writer",
		Effects: []string{"fs.write", "code.modify"},
		Script:  "#!/bin/sh\nprintf '{\"ok\":true}'\n",
		Permissions: skills.PermissionsConfig{
			Filesystem: skills.FilesystemPermissions{
				Read:  []string{"."},
				Write: []string{"./workspace"},
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
	_, err := runner.Execute(context.Background(), map[string]any{
		"skill_id": "bounded_writer",
		"args": map[string]any{
			"output_path": "../escape.txt",
		},
	})
	if err == nil {
		t.Fatalf("expected write path rejection")
	}
	if !strings.Contains(err.Error(), "outside allowed write paths") {
		t.Fatalf("error = %v, want write path rejection", err)
	}
}
