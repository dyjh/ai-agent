package code

// ReadFileInput is the input snapshot for code.read_file.
type ReadFileInput struct {
	Path     string `json:"path"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

// ListFilesInput is the input snapshot for code.list_files.
type ListFilesInput struct {
	Path             string `json:"path,omitempty"`
	MaxDepth         int    `json:"max_depth,omitempty"`
	Limit            int    `json:"limit,omitempty"`
	IncludeSensitive bool   `json:"include_sensitive,omitempty"`
}

// SearchInput is the input snapshot for code.search and code.search_text.
type SearchInput struct {
	Path             string `json:"path,omitempty"`
	Query            string `json:"query"`
	Limit            int    `json:"limit,omitempty"`
	IncludeSensitive bool   `json:"include_sensitive,omitempty"`
}

// SearchSymbolInput is the input snapshot for code.search_symbol.
type SearchSymbolInput struct {
	Path             string `json:"path,omitempty"`
	Symbol           string `json:"symbol"`
	Limit            int    `json:"limit,omitempty"`
	IncludeSensitive bool   `json:"include_sensitive,omitempty"`
}

// DetectInput is the input snapshot for project detection tools.
type DetectInput struct {
	Path string `json:"path,omitempty"`
}

// PatchFileInput describes a full-file replacement in a patch proposal.
type PatchFileInput struct {
	Path           string `json:"path"`
	Content        string `json:"content"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
}

// PatchInput is the input snapshot for code.propose_patch and code.apply_patch.
type PatchInput struct {
	Path           string           `json:"path,omitempty"`
	Content        string           `json:"content,omitempty"`
	ExpectedSHA256 string           `json:"expected_sha256,omitempty"`
	Files          []PatchFileInput `json:"files,omitempty"`
	Diff           string           `json:"diff,omitempty"`
	Summary        string           `json:"summary,omitempty"`
}

// ExplainDiffInput is the input snapshot for code.explain_diff.
type ExplainDiffInput struct {
	Diff  string           `json:"diff,omitempty"`
	Files []PatchFileInput `json:"files,omitempty"`
}

// RunTestsInput is the input snapshot for code.run_tests.
type RunTestsInput struct {
	Workspace       string   `json:"workspace,omitempty"`
	Command         string   `json:"command,omitempty"`
	Args            []string `json:"args,omitempty"`
	TimeoutSeconds  int      `json:"timeout_seconds,omitempty"`
	MaxOutputBytes  int64    `json:"max_output_bytes,omitempty"`
	UseDetected     bool     `json:"use_detected,omitempty"`
	TestNamePattern string   `json:"test_name_pattern,omitempty"`
}

// ParseTestFailureInput is the input snapshot for code.parse_test_failure.
type ParseTestFailureInput struct {
	Workspace string `json:"workspace,omitempty"`
	Command   string `json:"command"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exit_code"`
	Language  string `json:"language,omitempty"`
}

// FixLoopInput is the input snapshot for code.fix_test_failure_loop.
type FixLoopInput struct {
	Workspace      string `json:"workspace,omitempty"`
	TestCommand    string `json:"test_command,omitempty"`
	MaxIterations  int    `json:"max_iterations,omitempty"`
	StopOnApproval bool   `json:"stop_on_approval,omitempty"`
}
