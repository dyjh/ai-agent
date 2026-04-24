package tests

import (
	"os"
	"strings"
	"testing"

	"local-agent/internal/tools/skills"
)

func TestSkillZipInstallSuccess(t *testing.T) {
	root := createSkillFixture(t, skillFixtureOptions{
		ID:      "zip_skill",
		Effects: []string{"process.read"},
		Script:  "#!/bin/sh\nprintf '{\"ok\":true}'\n",
	})
	zipPath := createSkillZipFromDir(t, root)

	manager := skills.NewManager(t.TempDir())
	entry, err := manager.InstallZip(zipPath, false)
	if err != nil {
		t.Fatalf("InstallZip() error = %v", err)
	}
	if entry.Registration.ID != "zip_skill" {
		t.Fatalf("skill id = %s, want zip_skill", entry.Registration.ID)
	}
	if entry.Package.SourceType != "zip" {
		t.Fatalf("source_type = %s, want zip", entry.Package.SourceType)
	}
	if !strings.HasPrefix(entry.Package.Checksum, "sha256:") {
		t.Fatalf("checksum = %q, want sha256:*", entry.Package.Checksum)
	}
	if _, err := os.Stat(entry.Package.PackagePath); err != nil {
		t.Fatalf("installed package path missing: %v", err)
	}
}

func TestSkillZipInstallRejectsMissingManifest(t *testing.T) {
	zipPath := createZipWithEntries(t, map[string]string{
		"bin/skill.sh": "#!/bin/sh\nprintf ok\n",
	})

	manager := skills.NewManager(t.TempDir())
	_, err := manager.InstallZip(zipPath, false)
	if err == nil {
		t.Fatalf("expected missing manifest error")
	}
	if !strings.Contains(err.Error(), "skill.yaml") {
		t.Fatalf("error = %v, want missing skill.yaml", err)
	}
}

func TestSkillZipInstallRejectsZipSlip(t *testing.T) {
	zipPath := createZipWithEntries(t, map[string]string{
		"../escape.txt": "oops",
		"skill.yaml":    "id: bad\nversion: 1.0.0\nruntime:\n  type: executable\n  command: ./bin/skill.sh\n",
	})

	manager := skills.NewManager(t.TempDir())
	_, err := manager.InstallZip(zipPath, false)
	if err == nil {
		t.Fatalf("expected zip slip error")
	}
	if !strings.Contains(err.Error(), "escapes the target directory") {
		t.Fatalf("error = %v, want zip slip rejection", err)
	}
}

func TestSkillZipInstallRejectsDuplicateVersionWithoutForce(t *testing.T) {
	root := createSkillFixture(t, skillFixtureOptions{
		ID:      "dup_skill",
		Effects: []string{"process.read"},
	})
	zipPath := createSkillZipFromDir(t, root)

	manager := skills.NewManager(t.TempDir())
	if _, err := manager.InstallZip(zipPath, false); err != nil {
		t.Fatalf("first InstallZip() error = %v", err)
	}
	if _, err := manager.InstallZip(zipPath, false); err == nil {
		t.Fatalf("expected duplicate version error")
	}
	if _, err := manager.InstallZip(zipPath, true); err != nil {
		t.Fatalf("force InstallZip() error = %v", err)
	}
}
