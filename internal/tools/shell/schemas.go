package shell

// ExecInput is the approved shell input snapshot.
type ExecInput struct {
	Shell           string   `json:"shell"`
	Command         string   `json:"command"`
	CWD             string   `json:"cwd"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
	Purpose         string   `json:"purpose,omitempty"`
	ExpectedEffects []string `json:"expected_effects,omitempty"`
}

// CommandStructure is a lossy structural analysis of a shell command.
type CommandStructure struct {
	Command             string           `json:"command"`
	Segments            []CommandSegment `json:"segments"`
	HasPipeline         bool             `json:"has_pipeline"`
	HasWriteRedirect    bool             `json:"has_write_redirect"`
	RedirectTargets     []string         `json:"redirect_targets,omitempty"`
	PossibleFileTargets []string         `json:"possible_file_targets,omitempty"`
}

// CommandSegment represents one pipe segment.
type CommandSegment struct {
	Raw       string   `json:"raw"`
	Name      string   `json:"name"`
	Args      []string `json:"args,omitempty"`
	Redirects []string `json:"redirects,omitempty"`
}
