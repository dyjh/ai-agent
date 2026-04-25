package normalize

import "testing"

func TestNormalizerExtractsWorkspaceQuotesAndSignals(t *testing.T) {
	req := New().Normalize("请定位包含 `最小静态站点` 的文件，workspace: /www/wwwroot/test")
	if req.Workspace != "/www/wwwroot/test" {
		t.Fatalf("workspace = %q", req.Workspace)
	}
	if len(req.QuotedTexts) != 1 || req.QuotedTexts[0] != "最小静态站点" {
		t.Fatalf("quoted = %#v", req.QuotedTexts)
	}
	if !hasSignal(req.Signals, "search_text") {
		t.Fatalf("signals = %#v, want search_text", req.Signals)
	}
}

func TestNormalizerExtractsWorkspaceFilePath(t *testing.T) {
	req := New().Normalize("请打开 workspace: /www/wwwroot/test 中的 `index.html` 并确认页面标题")
	if len(req.PossibleFiles) != 1 || req.PossibleFiles[0] != "/www/wwwroot/test/index.html" {
		t.Fatalf("possible files = %#v", req.PossibleFiles)
	}
	if !hasSignal(req.Signals, "read_file") {
		t.Fatalf("signals = %#v, want read_file", req.Signals)
	}
}

func TestNormalizerSynonymSignalsChineseEnglishMixed(t *testing.T) {
	cases := []struct {
		input  string
		signal string
	}{
		{"请获取这台本地机器的系统概况", "system_overview"},
		{"system overview for this machine", "system_overview"},
		{"find containing `TODO` workspace: .", "search_text"},
		{"read `main.go` workspace: cmd/agent", "read_file"},
	}
	for _, tc := range cases {
		req := New().Normalize(tc.input)
		if !hasSignal(req.Signals, tc.signal) {
			t.Fatalf("Normalize(%q) signals=%#v, want %s", tc.input, req.Signals, tc.signal)
		}
	}
}

func TestNormalizerExtractsIDs(t *testing.T) {
	req := New().Normalize("host_id: local kb_id: kb_1 run_id: run_1 approval_id: apr_1")
	if req.HostID != "local" || req.KBID != "kb_1" || req.RunID != "run_1" || req.ApprovalID != "apr_1" {
		t.Fatalf("ids = %+v", req)
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
