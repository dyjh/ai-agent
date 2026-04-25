package normalize

import "testing"

func TestNormalizerExtractsStructuralSlots(t *testing.T) {
	req := New().Normalize("请定位包含 `最小静态站点` 的文件，workspace: /www/wwwroot/test")
	if req.Workspace != "/www/wwwroot/test" {
		t.Fatalf("workspace = %q", req.Workspace)
	}
	if len(req.QuotedTexts) != 1 || req.QuotedTexts[0] != "最小静态站点" {
		t.Fatalf("quoted = %#v", req.QuotedTexts)
	}
	if !hasSignal(req.Signals, "has_workspace") || !hasSignal(req.Signals, "has_quoted_text") {
		t.Fatalf("signals = %#v, want structural workspace/quoted signals", req.Signals)
	}
	if hasSignal(req.Signals, "search_text") {
		t.Fatalf("signals = %#v, natural-language semantic signal must not be emitted", req.Signals)
	}
}

func TestNormalizerExtractsWorkspaceFilePath(t *testing.T) {
	req := New().Normalize("请打开 workspace: /www/wwwroot/test 中的 `index.html` 并确认页面标题")
	if len(req.PossibleFiles) != 1 || req.PossibleFiles[0] != "/www/wwwroot/test/index.html" {
		t.Fatalf("possible files = %#v", req.PossibleFiles)
	}
	if !hasSignal(req.Signals, "has_possible_file") || !hasSignal(req.Signals, "has_file_path") {
		t.Fatalf("signals = %#v, want file path structural signals", req.Signals)
	}
	if hasSignal(req.Signals, "read_file") {
		t.Fatalf("signals = %#v, natural-language semantic signal must not be emitted", req.Signals)
	}
}

func TestNormalizerExtractsWorkspaceWithoutColonURLNumbersAndExplicitTool(t *testing.T) {
	req := New().Normalize("tool_id: code.search_text workspace /tmp/demo url https://example.test/a 50")
	if req.ExplicitToolID != "code.search_text" {
		t.Fatalf("explicit tool id = %q", req.ExplicitToolID)
	}
	if req.Workspace != "/tmp/demo" {
		t.Fatalf("workspace = %q", req.Workspace)
	}
	if len(req.URLs) != 1 || req.URLs[0] != "https://example.test/a" {
		t.Fatalf("urls = %#v", req.URLs)
	}
	if len(req.Numbers) != 1 || req.Numbers[0] != "50" {
		t.Fatalf("numbers = %#v", req.Numbers)
	}
	for _, signal := range []string{"has_explicit_tool_id", "has_workspace", "has_url", "has_number"} {
		if !hasSignal(req.Signals, signal) {
			t.Fatalf("signals = %#v, want %s", req.Signals, signal)
		}
	}
}

func TestNormalizerExtractsIDs(t *testing.T) {
	req := New().Normalize("host_id: local kb_id: kb_1 run_id: run_1 approval_id: apr_1")
	if req.HostID != "local" || req.KBID != "kb_1" || req.RunID != "run_1" || req.ApprovalID != "apr_1" {
		t.Fatalf("ids = %+v", req)
	}
	for _, signal := range []string{"has_host_id", "has_kb_id", "has_run_id", "has_approval_id"} {
		if !hasSignal(req.Signals, signal) {
			t.Fatalf("signals = %#v, want %s", req.Signals, signal)
		}
	}
}

func TestNormalizerDoesNotEmitNaturalLanguageSemanticSignals(t *testing.T) {
	for _, input := range []string{
		"请获取这台本地机器的系统概况",
		"system overview for this machine",
		"find containing `TODO` workspace: .",
		"read `main.go` workspace: cmd/agent",
	} {
		req := New().Normalize(input)
		for _, forbidden := range []string{"system_overview", "search_text", "read_file", "run_tests", "kb_answer", "memory_extract"} {
			if hasSignal(req.Signals, forbidden) {
				t.Fatalf("Normalize(%q) signals=%#v, forbidden %s", input, req.Signals, forbidden)
			}
		}
	}
}

func hasSignal(signals []string, want string) bool {
	for _, signal := range signals {
		if signal == want {
			return true
		}
	}
	return false
}
