package normalize

import "strings"

func signalsFor(req NormalizedRequest) ([]string, []string, []string) {
	text := req.NormalizedText
	signals := []string{}
	domains := []string{}
	intents := []string{}
	add := func(signal, domain, intent string) {
		signals = append(signals, signal)
		if domain != "" {
			domains = append(domains, domain)
		}
		if intent != "" {
			intents = append(intents, intent)
		}
	}

	if hasAny(text, "系统概况", "系统信息", "机器概况", "本机信息", "本地机器", "system overview", "system info", "machine overview") {
		add("system_overview", "ops", "system_overview")
	}
	if hasAny(text, "定位包含", "查找包含", "搜索包含", "find containing", "search containing", "grep") {
		add("search_text", "code", "search_text")
	}
	if hasAny(text, "搜索代码", "查找代码", "search code", "search_text") {
		add("search_text", "code", "search_text")
	}
	if hasAny(text, "打开", "读取", "查看", "open", "read") && (len(req.PossibleFiles) > 0 || strings.Contains(text, "文件") || strings.Contains(text, "file")) {
		add("read_file", "code", "read_file")
	}
	if hasAny(text, "磁盘", "disk usage", "disk") {
		add("disk_usage", "ops", "disk_usage")
	}
	if hasAny(text, "内存", "memory usage") {
		add("memory_usage", "ops", "memory_usage")
	}
	if hasAny(text, "cpu 占用", "cpu占用", "进程", "process") {
		add("processes", "ops", "processes")
	}
	if hasAny(text, "日志", "logs", "log") {
		add("logs", "ops", "logs")
	}
	if hasAny(text, "docker") {
		add("docker_ops", "ops", "docker")
	}
	if hasAny(text, "k8s", "kubernetes", "kubectl") {
		add("k8s_ops", "ops", "k8s")
	}
	if hasAny(text, "ssh") {
		add("ssh_ops", "ops", "ssh")
	}
	if hasAny(text, "重启", "restart") && hasAny(text, "服务", "service") {
		add("local_restart", "ops", "service_restart")
	}
	if hasAny(text, "runbook", "运行手册", "按 runbook") {
		add("runbook_ops", "ops", "runbook")
	}
	if hasAny(text, "git status", "工作区状态") {
		add("git_status", "git", "status")
	}
	if hasAny(text, "git diff summary", "diff summary", "diff 摘要", "总结 diff") {
		add("git_diff_summary", "git", "diff_summary")
	} else if hasAny(text, "git diff", "查看 diff", "代码 diff") {
		add("git_diff", "git", "diff")
	}
	if hasAny(text, "commit message", "提交信息", "commit-message") {
		add("git_commit_message", "git", "commit_message")
	}
	if hasAny(text, "git log", "提交记录") {
		add("git_log", "git", "log")
	}
	if hasAny(text, "修复测试", "修测试", "fix tests", "fix failing test", "fix test failure", "测试失败", "failed tests") {
		add("fix_tests", "code", "fix_tests")
	}
	if hasAny(text, "跑测试", "运行测试", "run tests", "go test", "npm test", "pytest", "cargo test", "make test") || (strings.Contains(text, "测试") && !hasAny(text, "测试失败")) {
		add("run_tests", "code", "run_tests")
	}
	if hasAny(text, "修 bug", "修复", "实现", "改代码", "修改代码", "代码任务", "看代码", "检查项目", "梳理", "语言构成", "文件语言构成", "inspect project", "language composition", "fix bug", "implement", "code") {
		add("inspect_project", "code", "inspect_project")
	}
	if req.Workspace != "" {
		add("workspace_scope", "code", "")
	}
	if hasAny(text, "只根据知识库", "知识库回答", "基于知识库", "引用来源", "给出引用", "citation", "citations", "kb_only", "no_citation_no_answer", "无引用不回答", "没有引用不要回答") {
		add("kb_answer", "rag", "answer")
	}
	if hasAny(text, "检索知识库", "搜索知识库", "knowledge base search", "kb.retrieve", "hybrid retrieval") {
		add("kb_retrieve", "rag", "retrieve")
	}
	if hasAny(text, "记住", "记一下", "以后", "remember", "always", "never") && !hasAny(text, "忘记", "forget") {
		add("memory_extract", "memory", "extract_candidates")
	}
	if hasAny(text, "忘记", "删除这条记忆", "归档记忆", "archive memory", "forget this memory") {
		add("memory_archive", "memory", "archive")
	}
	if hasAny(text, "validate patch", "patch validate", "验证 patch", "校验 patch", "dry-run patch") {
		add("patch_validate", "code", "patch_validate")
	}
	if hasAny(text, "apply patch", "patch apply", "应用 patch") {
		add("patch_apply", "code", "patch_apply")
	}
	if hasAny(text, "安装", "依赖", "install dependency", "install package", "add dependency") {
		add("install_dependency", "code", "install_dependency")
	}
	return uniq(signals), uniq(domains), uniq(intents)
}

func hasAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
