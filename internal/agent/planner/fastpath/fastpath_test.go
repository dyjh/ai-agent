package fastpath

import (
	"testing"

	"local-agent/internal/agent/planner/intent"
	"local-agent/internal/agent/planner/normalize"
)

func TestFastPathBugRegressionCases(t *testing.T) {
	cases := []struct {
		input string
		tool  string
		key   string
		value any
	}{
		{"请获取这台本地机器的系统概况", "ops.local.system_info", "", nil},
		{"请定位包含 `最小静态站点` 的文件，workspace: /www/wwwroot/test", "code.search_text", "query", "最小静态站点"},
		{"请打开 workspace: /www/wwwroot/test 中的 `index.html` 并确认页面标题", "code.read_file", "path", "/www/wwwroot/test/index.html"},
	}
	for _, tc := range cases {
		req := normalize.New().Normalize(tc.input)
		cls := intent.New().Classify(req)
		plan, ok := New().Plan(Input{Request: req, Classification: cls})
		if !ok || len(plan.Steps) == 0 || plan.Steps[0].Tool != tc.tool {
			t.Fatalf("Plan(%q)=%+v ok=%v, want %s", tc.input, plan, ok, tc.tool)
		}
		if tc.key != "" && plan.Steps[0].Input[tc.key] != tc.value {
			t.Fatalf("input[%s]=%v, want %v", tc.key, plan.Steps[0].Input[tc.key], tc.value)
		}
	}
}

func TestFastPathCommonTools(t *testing.T) {
	cases := map[string]string{
		"请读取文件 `main.go`，workspace: cmd/agent":     "code.read_file",
		"find containing `TODO` workspace: .":      "code.search_text",
		"请查看 git status":                           "git.status",
		"请查看 git diff":                             "git.diff",
		"请梳理 workspace: /www/wwwroot/test 的文件语言构成": "code.inspect_project",
		"只根据知识库回答这个文档里有没有 alpha，并引用来源":             "kb.answer",
		"记住我喜欢中文回答":                                "memory.extract_candidates",
	}
	for input, want := range cases {
		req := normalize.New().Normalize(input)
		cls := intent.New().Classify(req)
		plan, ok := New().Plan(Input{ConversationID: "conv_1", Request: req, Classification: cls})
		if !ok || len(plan.Steps) == 0 || plan.Steps[0].Tool != want {
			t.Fatalf("Plan(%q)=%+v ok=%v, want %s", input, plan, ok, want)
		}
	}
}
