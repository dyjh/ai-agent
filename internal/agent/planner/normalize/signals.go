package normalize

// This file extracts structural slots only.
// Do not add natural-language phrase dictionaries here.
// Tool semantics live in Tool Cards and Semantic Planner.
func signalsFor(req NormalizedRequest) ([]string, []string, []string) {
	signals := []string{}
	add := func(signal string) {
		signals = append(signals, signal)
	}

	if req.Workspace != "" {
		add("has_workspace")
	}
	if len(req.QuotedTexts) > 0 {
		add("has_quoted_text")
	}
	if len(req.PossibleFiles) > 0 {
		add("has_possible_file")
		add("has_file_path")
	}
	if len(req.URLs) > 0 {
		add("has_url")
	}
	if req.HostID != "" {
		add("has_host_id")
	}
	if req.KBID != "" {
		add("has_kb_id")
	}
	if req.RunID != "" {
		add("has_run_id")
	}
	if req.ApprovalID != "" {
		add("has_approval_id")
	}
	if req.ExplicitToolID != "" {
		add("has_explicit_tool_id")
	}
	if len(req.Numbers) > 0 {
		add("has_number")
	}
	return uniq(signals), nil, nil
}
