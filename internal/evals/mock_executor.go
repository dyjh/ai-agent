package evals

import (
	"context"
	"strings"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/security"
)

// MockExecutor returns deterministic, redacted tool outputs for eval safe mode.
type MockExecutor struct {
	Tool string
	Case EvalCase
}

// Execute returns a safe mock result and never touches the external world.
func (m MockExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	now := time.Now().UTC()
	output := map[string]any{
		"status": "mocked",
		"tool":   m.Tool,
		"input":  security.RedactMap(core.CloneMap(input)),
	}
	switch m.Tool {
	case "kb.answer":
		citations := m.mockCitations()
		refused := m.Case.Expected.RefusalExpected != nil && *m.Case.Expected.RefusalExpected
		if required := m.Case.Expected.CitationRequired; required != nil && *required && len(citations) == 0 {
			refused = true
		}
		output["answer"] = map[string]any{
			"text":      mockAnswerText(m.Case),
			"citations": citations,
			"refused":   refused,
		}
		output["citations"] = citations
		output["refused"] = refused
		output["summary"] = "mock KB answer completed"
	case "kb.retrieve", "kb.search":
		output["citations"] = m.mockCitations()
		output["items"] = m.mockCitations()
		output["summary"] = "mock KB retrieval completed"
	case "code.run_tests":
		output["command"] = "go test ./..."
		output["passed"] = true
		output["exit_code"] = 0
		output["stdout"] = "ok local-agent/eval-fixture"
		output["stderr"] = ""
		output["summary"] = "mock allowlisted tests passed"
	case "code.fix_test_failure_loop":
		output["final_passed"] = false
		output["stopped_reason"] = "mock_patch_required"
		output["next_proposal"] = map[string]any{"tool": "code.propose_patch"}
		output["summary"] = "mock fix loop stopped before writing; propose_patch is required"
	case "code.parse_test_failure":
		output["failures"] = []map[string]any{{"test": "TestFixture", "message": "mock failure parsed"}}
		output["summary"] = "mock test failure parsed"
	case "code.read_file":
		path, _ := input["path"].(string)
		if strings.HasSuffix(path, ".diff") || strings.HasSuffix(path, ".patch") {
			output["content"] = "--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n"
		} else if security.IsSensitivePath(path, nil) {
			output["content"] = "OPENAI_API_KEY=[REDACTED]"
		} else {
			output["content"] = "mock file content"
		}
		output["summary"] = "mock file read completed"
	case "code.propose_patch":
		output["diff_preview"] = "--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n"
		output["changed_files"] = []string{"main.go"}
		output["summary"] = "mock patch proposal generated"
	case "code.apply_patch":
		output["applied"] = true
		output["summary"] = "mock patch applied after approval"
	case "git.status", "git.diff", "git.log", "git.branch", "git.diff_summary", "git.commit_message_proposal":
		output["summary"] = "mock git read completed"
	case "memory.extract_candidates":
		output["queued"] = input["queue"]
		output["candidates"] = []map[string]any{{"text": security.RedactString(m.Case.Input), "status": "pending_review"}}
		output["summary"] = "mock memory candidates queued for review"
	case "ops.local.processes", "ops.local.system_info", "ops.local.disk_usage", "ops.local.memory_usage", "ops.local.network_info", "ops.local.service_status", "ops.local.logs_tail":
		output["summary"] = "mock local ops read completed"
	case "ops.docker.ps", "ops.docker.inspect", "ops.docker.logs", "ops.docker.stats":
		output["summary"] = "mock docker read completed"
	case "ops.k8s.get", "ops.k8s.describe", "ops.k8s.logs", "ops.k8s.events":
		output["summary"] = "mock kubernetes read completed"
	case "runbook.plan", "runbook.list", "runbook.read":
		output["summary"] = "mock runbook dry-run completed"
	default:
		output["summary"] = "mock tool completed"
	}
	output = security.RedactMap(output)
	return &core.ToolResult{
		Output:     output,
		StartedAt:  now,
		FinishedAt: now,
	}, nil
}

func (m MockExecutor) mockCitations() []EvalCitation {
	if len(m.Case.Expected.ExpectedSources) == 0 {
		return nil
	}
	citations := make([]EvalCitation, 0, len(m.Case.Expected.ExpectedSources))
	for _, source := range m.Case.Expected.ExpectedSources {
		citations = append(citations, EvalCitation{
			Source:     source,
			SourceFile: source,
			Score:      0.99,
		})
	}
	return citations
}

func mockAnswerText(c EvalCase) string {
	if len(c.Expected.AnswerHints) > 0 {
		return strings.Join(c.Expected.AnswerHints, " ")
	}
	return "mock grounded answer"
}
