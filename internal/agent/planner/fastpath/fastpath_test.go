package fastpath

import (
	"testing"

	"local-agent/internal/agent/planner/intent"
	"local-agent/internal/agent/planner/normalize"
)

func TestFastPathDoesNotRouteNaturalLanguage(t *testing.T) {
	cases := []string{
		"请获取这台本地机器的系统概况",
		"请定位包含 `最小静态站点` 的文件，workspace: /www/wwwroot/test",
		"请打开 workspace: /www/wwwroot/test 中的 `index.html` 并确认页面标题",
		"find containing `TODO` workspace: .",
	}
	for _, input := range cases {
		req := normalize.New().Normalize(input)
		cls := intent.New().Classify(req)
		if plan, ok := New().Plan(Input{Request: req, Classification: cls}); ok {
			t.Fatalf("Plan(%q)=%+v ok=true, fastpath must not route natural language", input, plan)
		}
	}
}
