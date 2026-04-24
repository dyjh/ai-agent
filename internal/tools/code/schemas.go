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
	Summary        string           `json:"summary,omitempty"`
}

// ExplainDiffInput is the input snapshot for code.explain_diff.
type ExplainDiffInput struct {
	Diff  string           `json:"diff,omitempty"`
	Files []PatchFileInput `json:"files,omitempty"`
}
