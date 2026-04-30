package semantic

import "strings"

// ChatGateConfig controls the lightweight no-tool gate. It may decide that a
// request is direct chat, but it never selects a concrete tool.
type ChatGateConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Mode    string `json:"mode" yaml:"mode"`
}

// ToolPlannerConfig controls whether local planner components may produce
// final ToolProposals or only provide context for the semantic planner.
type ToolPlannerConfig struct {
	RequireLLMForToolChoice    bool `json:"require_llm_for_tool_choice" yaml:"require_llm_for_tool_choice"`
	EnableFastPath             bool `json:"enable_fastpath" yaml:"enable_fastpath"`
	AllowCandidateFallback     bool `json:"allow_candidate_fallback" yaml:"allow_candidate_fallback"`
	CandidateSelectorAsContext bool `json:"candidate_selector_as_context" yaml:"candidate_selector_as_context"`
	AllowCrossCandidate        bool `json:"allow_cross_candidate" yaml:"allow_cross_candidate"`
}

// ShellPlannerConfig prevents shell.exec from becoming an implicit fallback.
type ShellPlannerConfig struct {
	AllowAutoFallback bool `json:"allow_auto_fallback" yaml:"allow_auto_fallback"`
}

// DebugConfig controls planner observability.
type DebugConfig struct {
	ExposePlannerSource bool `json:"expose_planner_source" yaml:"expose_planner_source"`
}

// NormalizeConfig fills mode-specific planner defaults.
func NormalizeConfig(cfg Config) Config {
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	if cfg.Mode == "" {
		cfg.Mode = "hybrid"
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 2
	}
	if cfg.ChatGate.Mode == "" {
		cfg.ChatGate.Mode = "lightweight"
	}
	if IsSemanticStrictMode(cfg.Mode) {
		cfg.ChatGate.Enabled = true
		cfg.ToolPlanner.RequireLLMForToolChoice = true
		cfg.ToolPlanner.EnableFastPath = false
		cfg.ToolPlanner.AllowCandidateFallback = false
		cfg.Shell.AllowAutoFallback = false
		cfg.Debug.ExposePlannerSource = true
	} else {
		cfg.ToolPlanner.EnableFastPath = true
		cfg.ToolPlanner.AllowCandidateFallback = true
	}
	return cfg
}

// IsSemanticStrictMode reports whether mode requires the LLM to make tool
// choices and forbids local candidate fallback.
func IsSemanticStrictMode(mode string) bool {
	return strings.EqualFold(strings.TrimSpace(mode), "semantic_strict")
}
