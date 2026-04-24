package gittools

// ToolInput is the input snapshot for git workflow tools.
type ToolInput struct {
	Workspace string   `json:"workspace,omitempty"`
	Paths     []string `json:"paths,omitempty"`
	Message   string   `json:"message,omitempty"`
	Args      []string `json:"args,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Staged    bool     `json:"staged,omitempty"`
}
