package memory

// SearchInput is the input snapshot for memory.search.
type SearchInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

// PatchInput is the input snapshot for memory.patch.
type PatchInput struct {
	Path        string            `json:"path"`
	Summary     string            `json:"summary"`
	Body        string            `json:"body"`
	Frontmatter map[string]string `json:"frontmatter,omitempty"`
}
