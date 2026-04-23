package code

// ReadFileInput is the input snapshot for code.read_file.
type ReadFileInput struct {
	Path string `json:"path"`
}

// SearchInput is the input snapshot for code.search.
type SearchInput struct {
	Path  string `json:"path"`
	Query string `json:"query"`
}

// PatchInput is the input snapshot for code.apply_patch.
type PatchInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
