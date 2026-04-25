package intent

import (
	"testing"

	"local-agent/internal/agent/planner/normalize"
)

func TestIntentClassifierUsesStructuralSlotsOnly(t *testing.T) {
	cases := []struct {
		input  string
		domain IntentDomain
		intent string
	}{
		{"请定位包含 `最小静态站点` 的文件，workspace: /www/wwwroot/test", DomainCode, "workspace_scoped"},
		{"find containing `TODO` workspace: .", DomainCode, "workspace_scoped"},
		{"请打开 workspace: /www/wwwroot/test 中的 `index.html`", DomainCode, "workspace_scoped"},
		{"kb_id: kb_1 question", DomainRAG, "knowledge"},
		{"host_id: local status", DomainOps, "host_scoped"},
		{"approval_id: apr_1", DomainApproval, "approval"},
		{"run_id: run_1", DomainRun, "run"},
	}
	for _, tc := range cases {
		req := normalize.New().Normalize(tc.input)
		got := New().Classify(req)
		if got.Domain != tc.domain || got.Intent != tc.intent || !got.NeedTool {
			t.Fatalf("Classify(%q)=%+v, want %s/%s tool", tc.input, got, tc.domain, tc.intent)
		}
	}
}

func TestIntentClassifierDoesNotClassifyNaturalLanguagePhrases(t *testing.T) {
	for _, input := range []string{
		"请获取这台本地机器的系统概况",
		"system overview for this machine",
		"帮我看一下日志",
	} {
		req := normalize.New().Normalize(input)
		got := New().Classify(req)
		if got.NeedTool || got.NeedClarify || got.Intent != "answer" {
			t.Fatalf("Classify(%q)=%+v, want no natural-language intent decision", input, got)
		}
	}
}
