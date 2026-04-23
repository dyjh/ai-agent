package tests

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"local-agent/internal/app"
	"local-agent/internal/config"
	"local-agent/internal/core"
)

func TestReadOnlySkillAutoExecutes(t *testing.T) {
	bootstrap := newSkillBootstrap(t)
	root := createSkillFixture(t, skillFixtureOptions{
		ID:      "read_skill",
		Effects: []string{"process.read", "system.metrics.read"},
		Script:  "#!/bin/sh\nprintf '{\"ok\":true}'\n",
	})
	if _, err := bootstrap.Skills.Upload(root, "", ""); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	outcome, err := bootstrap.Router.Propose(context.Background(), "run_skill_read", "conv_skill_read", core.ToolProposal{
		ID:   "tool_skill_read",
		Tool: "skill.run",
		Input: map[string]any{
			"skill_id": "read_skill",
		},
	})
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if outcome.Decision.RequiresApproval {
		t.Fatalf("read-only skill should auto execute")
	}
	if outcome.Result == nil {
		t.Fatalf("expected tool result")
	}
	if outcome.Result.Output["status"] != "ok" {
		t.Fatalf("status = %v, want ok", outcome.Result.Output["status"])
	}
}

func TestWriteSkillRequiresApprovalAndHonorsSnapshot(t *testing.T) {
	bootstrap := newSkillBootstrap(t)
	root := createSkillFixture(t, skillFixtureOptions{
		ID:      "write_skill",
		Effects: []string{"fs.write", "code.modify"},
		Script:  "#!/bin/sh\nprintf '{\"ok\":true}'\n",
	})
	if _, err := bootstrap.Skills.Upload(root, "", ""); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	proposal := core.ToolProposal{
		ID:   "tool_skill_write",
		Tool: "skill.run",
		Input: map[string]any{
			"skill_id": "write_skill",
			"args": map[string]any{
				"path": "alpha.txt",
			},
		},
	}

	outcome, err := bootstrap.Router.Propose(context.Background(), "run_skill_write", "conv_skill_write", proposal)
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if outcome.Approval == nil || !outcome.Decision.RequiresApproval {
		t.Fatalf("write skill should require approval")
	}

	changed := proposal
	changed.Input = map[string]any{
		"skill_id": "write_skill",
		"args": map[string]any{
			"path": "beta.txt",
		},
	}
	matches, err := bootstrap.Approvals.SnapshotMatches(outcome.Approval.ID, changed)
	if err != nil {
		t.Fatalf("SnapshotMatches() error = %v", err)
	}
	if matches {
		t.Fatalf("changed proposal should not match approval snapshot")
	}

	if _, err := bootstrap.Approvals.Approve(outcome.Approval.ID); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	result, err := bootstrap.Router.ExecuteApproved(context.Background(), outcome.Approval.ID)
	if err != nil {
		t.Fatalf("ExecuteApproved() error = %v", err)
	}
	if result.Output["status"] != "ok" {
		t.Fatalf("status = %v, want ok", result.Output["status"])
	}
}

func newSkillBootstrap(t *testing.T) *app.Bootstrap {
	t.Helper()

	cfg := config.Default()
	cfg.Database.URL = ""
	cfg.Memory.RootDir = t.TempDir()
	cfg.Events.JSONLRoot = t.TempDir()
	cfg.Events.AuditRoot = t.TempDir()
	cfg.Vector.Backend = config.VectorBackendMemory
	cfg.Vector.EmbeddingDimension = 16
	cfg.KB.Enabled = false
	cfg.KB.Provider = ""

	bootstrap, err := app.NewBootstrap(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	return bootstrap
}
