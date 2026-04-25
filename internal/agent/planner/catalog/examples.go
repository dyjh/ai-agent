package catalog

func examplesForTool(tool string) ([]ToolExample, []ToolExample) {
	switch tool {
	case "code.read_file":
		return []ToolExample{
			{User: "请打开 workspace: /repo 中的 `index.html`", Input: map[string]any{"path": "/repo/index.html", "max_bytes": 200000}},
			{User: "read `cmd/main.go` in workspace: app", Input: map[string]any{"path": "app/cmd/main.go", "max_bytes": 200000}},
		}, []ToolExample{{User: "请检查这个项目", Input: map[string]any{"path": "/repo"}}}
	case "code.search_text":
		return []ToolExample{
			{User: "请定位包含 `最小静态站点` 的文件，workspace: /repo", Input: map[string]any{"path": "/repo", "query": "最小静态站点", "limit": 50}},
			{User: "find containing `TODO` workspace: .", Input: map[string]any{"path": ".", "query": "TODO", "limit": 50}},
		}, []ToolExample{{User: "请打开 `index.html`", Input: map[string]any{"path": "index.html"}}}
	case "code.inspect_project":
		return []ToolExample{{User: "帮我实现这个功能，先检查项目", Input: map[string]any{"path": "."}}}, nil
	case "code.run_tests":
		return []ToolExample{{User: "请运行测试，workspace: services/api", Input: map[string]any{"workspace": "services/api", "use_detected": true}}}, nil
	case "git.status":
		return []ToolExample{{User: "请查看 git status，workspace: .", Input: map[string]any{"workspace": "."}}}, nil
	case "git.diff":
		return []ToolExample{{User: "请查看 git diff，workspace: .", Input: map[string]any{"workspace": "."}}}, nil
	case "ops.local.system_info":
		return []ToolExample{
			{User: "请获取这台本地机器的系统概况", Input: map[string]any{}},
			{User: "system overview", Input: map[string]any{}},
		}, nil
	case "ops.local.processes":
		return []ToolExample{{User: "看一下本机 CPU 占用", Input: map[string]any{}}}, nil
	case "ops.local.disk_usage":
		return []ToolExample{{User: "看一下磁盘空间", Input: map[string]any{}}}, nil
	case "kb.answer":
		return []ToolExample{{User: "只根据知识库回答并引用来源", Input: map[string]any{"query": "只根据知识库回答并引用来源", "mode": "kb_only", "top_k": 5, "require_citations": true, "rerank": true}}}, nil
	case "memory.extract_candidates":
		return []ToolExample{{User: "记住我喜欢中文回答", Input: map[string]any{"text": "记住我喜欢中文回答", "queue": true}}}, nil
	default:
		return nil, nil
	}
}
