package intent

import (
	"testing"

	"local-agent/internal/agent/planner/normalize"
)

func TestIntentClassifierCoreCases(t *testing.T) {
	cases := []struct {
		input  string
		domain IntentDomain
		intent string
	}{
		{"请获取这台本地机器的系统概况", DomainOps, "system_overview"},
		{"system overview for this machine", DomainOps, "system_overview"},
		{"请定位包含 `最小静态站点` 的文件，workspace: /www/wwwroot/test", DomainCode, "search_text"},
		{"find containing `TODO` workspace: .", DomainCode, "search_text"},
		{"请打开 workspace: /www/wwwroot/test 中的 `index.html`", DomainCode, "read_file"},
	}
	for _, tc := range cases {
		req := normalize.New().Normalize(tc.input)
		got := New().Classify(req)
		if got.Domain != tc.domain || got.Intent != tc.intent || !got.NeedTool {
			t.Fatalf("Classify(%q)=%+v, want %s/%s tool", tc.input, got, tc.domain, tc.intent)
		}
	}
}

func TestIntentClassifierAmbiguousLogsClarify(t *testing.T) {
	req := normalize.New().Normalize("帮我看一下日志")
	got := New().Classify(req)
	if !got.NeedClarify || got.Intent != "logs" {
		t.Fatalf("classification = %+v, want logs clarify", got)
	}
}
