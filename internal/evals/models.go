package evals

import (
	"time"

	"local-agent/internal/core"
	"local-agent/internal/security"
)

// EvalCategory identifies the capability surface under evaluation.
type EvalCategory string

const (
	EvalCategoryChat   EvalCategory = "chat"
	EvalCategoryRAG    EvalCategory = "rag"
	EvalCategoryCode   EvalCategory = "code"
	EvalCategoryOps    EvalCategory = "ops"
	EvalCategorySafety EvalCategory = "safety"
)

// Valid reports whether the category is one of the built-in eval categories.
func (c EvalCategory) Valid() bool {
	switch c {
	case EvalCategoryChat, EvalCategoryRAG, EvalCategoryCode, EvalCategoryOps, EvalCategorySafety:
		return true
	default:
		return false
	}
}

// EvalCase is the portable golden-task definition for chat, RAG, code, ops and safety checks.
type EvalCase struct {
	ID               string         `json:"id" yaml:"id"`
	Title            string         `json:"title" yaml:"title"`
	Category         EvalCategory   `json:"category" yaml:"category"`
	Tags             []string       `json:"tags,omitempty" yaml:"tags,omitempty"`
	Input            string         `json:"input" yaml:"input"`
	Conversation     []EvalMessage  `json:"conversation,omitempty" yaml:"conversation,omitempty"`
	WorkspaceFixture string         `json:"workspace_fixture,omitempty" yaml:"workspace_fixture,omitempty"`
	KBFixture        string         `json:"kb_fixture,omitempty" yaml:"kb_fixture,omitempty"`
	MemoryFixture    string         `json:"memory_fixture,omitempty" yaml:"memory_fixture,omitempty"`
	OpsFixture       string         `json:"ops_fixture,omitempty" yaml:"ops_fixture,omitempty"`
	Expected         EvalExpected   `json:"expected" yaml:"expected"`
	Forbidden        EvalForbidden  `json:"forbidden,omitempty" yaml:"forbidden,omitempty"`
	Replay           ReplayOptions  `json:"replay,omitempty" yaml:"replay,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	SourcePath       string         `json:"-" yaml:"-"`
}

// EvalMessage stores one turn of an eval conversation fixture.
type EvalMessage struct {
	Role    string `json:"role" yaml:"role"`
	Content string `json:"content" yaml:"content"`
}

// EvalExpected describes assertions that should hold for a case.
type EvalExpected struct {
	Tools            []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	ToolSequence     []string `json:"tool_sequence,omitempty" yaml:"tool_sequence,omitempty"`
	ApprovalRequired *bool    `json:"approval_required,omitempty" yaml:"approval_required,omitempty"`
	RefusalExpected  *bool    `json:"refusal_expected,omitempty" yaml:"refusal_expected,omitempty"`
	CitationRequired *bool    `json:"citation_required,omitempty" yaml:"citation_required,omitempty"`
	ExpectedSources  []string `json:"expected_sources,omitempty" yaml:"expected_sources,omitempty"`
	AnswerHints      []string `json:"answer_hints,omitempty" yaml:"answer_hints,omitempty"`
	RiskLevel        string   `json:"risk_level,omitempty" yaml:"risk_level,omitempty"`
	PolicyProfile    string   `json:"policy_profile,omitempty" yaml:"policy_profile,omitempty"`
}

// EvalForbidden describes behavior that must not happen.
type EvalForbidden struct {
	Tools          []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	Effects        []string `json:"effects,omitempty" yaml:"effects,omitempty"`
	SecretPatterns []string `json:"secret_patterns,omitempty" yaml:"secret_patterns,omitempty"`
}

// ReplayMode selects historical event playback or safe behavior replay.
type ReplayMode string

const (
	ReplayModeEvent    ReplayMode = "event"
	ReplayModeBehavior ReplayMode = "behavior"
)

// ReplayOptions configures event or behavior replay.
type ReplayOptions struct {
	Mode             ReplayMode `json:"mode,omitempty" yaml:"mode,omitempty"`
	UseMockTools     bool       `json:"use_mock_tools" yaml:"use_mock_tools"`
	RedactSecrets    bool       `json:"redact_secrets" yaml:"redact_secrets"`
	CompareToolCalls bool       `json:"compare_tool_calls" yaml:"compare_tool_calls"`
	CompareApprovals bool       `json:"compare_approvals" yaml:"compare_approvals"`
}

// EvalRunStatus captures suite-level run status.
type EvalRunStatus string

const (
	EvalRunPending EvalRunStatus = "pending"
	EvalRunRunning EvalRunStatus = "running"
	EvalRunPassed  EvalRunStatus = "passed"
	EvalRunFailed  EvalRunStatus = "failed"
	EvalRunError   EvalRunStatus = "error"
)

// EvalApprovalMode controls how safe-mode eval handles approval requests.
type EvalApprovalMode string

const (
	EvalApprovalRejectAllWrites     EvalApprovalMode = "reject_all_writes"
	EvalApprovalApproveExpectedOnly EvalApprovalMode = "approve_expected_writes"
	EvalApprovalAutoApproveReadonly EvalApprovalMode = "auto_approve_readonly"
)

// EvalRunRequest selects cases to run.
type EvalRunRequest struct {
	CaseIDs      []string         `json:"case_ids,omitempty"`
	Category     EvalCategory     `json:"category,omitempty"`
	Tag          string           `json:"tag,omitempty"`
	Tags         []string         `json:"tags,omitempty"`
	ApprovalMode EvalApprovalMode `json:"approval_mode,omitempty"`
	MaxSteps     int              `json:"max_steps,omitempty"`
}

// EvalRun stores one suite execution.
type EvalRun struct {
	RunID      string         `json:"run_id"`
	Status     EvalRunStatus  `json:"status"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`
	Request    EvalRunRequest `json:"request"`
	Total      int            `json:"total"`
	Passed     int            `json:"passed"`
	Failed     int            `json:"failed"`
	Errors     int            `json:"errors"`
	Results    []EvalResult   `json:"results"`
	ReportPath string         `json:"report_path,omitempty"`
}

// EvalResult stores assertions and observed behavior for one case.
type EvalResult struct {
	CaseID          string                   `json:"case_id"`
	Title           string                   `json:"title,omitempty"`
	Category        EvalCategory             `json:"category"`
	Status          EvalRunStatus            `json:"status"`
	Passed          bool                     `json:"passed"`
	Score           float64                  `json:"score"`
	Assertions      []EvalAssertion          `json:"assertions"`
	ToolCalls       []string                 `json:"tool_calls,omitempty"`
	Approvals       []string                 `json:"approvals,omitempty"`
	Citations       []EvalCitation           `json:"citations,omitempty"`
	Refused         bool                     `json:"refused"`
	SecretFindings  []security.SecretFinding `json:"secret_findings,omitempty"`
	RiskTraces      []core.RiskTrace         `json:"risk_traces,omitempty"`
	PolicyDecisions []core.PolicyDecision    `json:"policy_decisions,omitempty"`
	Events          []core.Event             `json:"events,omitempty"`
	Summary         string                   `json:"summary"`
	Error           string                   `json:"error,omitempty"`
}

// EvalCitation is a minimal citation shape used by generic eval reports.
type EvalCitation struct {
	Source     string  `json:"source,omitempty"`
	SourceFile string  `json:"source_file,omitempty"`
	SourceURI  string  `json:"source_uri,omitempty"`
	DocumentID string  `json:"document_id,omitempty"`
	Score      float64 `json:"score,omitempty"`
}

// EvalAssertion stores one check outcome.
type EvalAssertion struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Expected any    `json:"expected,omitempty"`
	Actual   any    `json:"actual,omitempty"`
	Message  string `json:"message,omitempty"`
}

// EvalCaseListResponse is used by the HTTP API.
type EvalCaseListResponse struct {
	Items []EvalCase `json:"items"`
}

// EvalRunListResponse is used by the HTTP API.
type EvalRunListResponse struct {
	Items []EvalRun `json:"items"`
}

// EvalReport is the persisted JSON report format.
type EvalReport struct {
	RunID      string                         `json:"run_id"`
	Summary    EvalReportSummary              `json:"summary"`
	ByCategory map[EvalCategory]CategoryStats `json:"by_category"`
	Failures   []EvalReportFailure            `json:"failures,omitempty"`
	Security   EvalReportSecurity             `json:"security"`
	CreatedAt  time.Time                      `json:"created_at"`
}

// EvalReportSummary stores aggregate counts.
type EvalReportSummary struct {
	Total    int     `json:"total"`
	Passed   int     `json:"passed"`
	Failed   int     `json:"failed"`
	Errors   int     `json:"errors"`
	PassRate float64 `json:"pass_rate"`
}

// CategoryStats stores per-category report counts.
type CategoryStats struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
	Errors int `json:"errors"`
}

// EvalReportFailure stores a failed assertion in report-friendly form.
type EvalReportFailure struct {
	CaseID    string `json:"case_id"`
	Category  string `json:"category"`
	Assertion string `json:"assertion"`
	Expected  any    `json:"expected,omitempty"`
	Actual    any    `json:"actual,omitempty"`
	Message   string `json:"message,omitempty"`
}

// EvalReportSecurity summarizes security regressions.
type EvalReportSecurity struct {
	SecretFindings          []security.SecretFinding `json:"secret_findings,omitempty"`
	ForbiddenToolCalls      []string                 `json:"forbidden_tool_calls,omitempty"`
	UnexpectedAutoApprovals []string                 `json:"unexpected_auto_approvals,omitempty"`
}

// ReplayResult stores event or behavior replay output.
type ReplayResult struct {
	ReplayID    string        `json:"replay_id"`
	SourceRunID string        `json:"source_run_id"`
	Mode        ReplayMode    `json:"mode"`
	Status      EvalRunStatus `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
	Options     ReplayOptions `json:"options"`
	Events      []core.Event  `json:"events,omitempty"`
	Behavior    *EvalResult   `json:"behavior,omitempty"`
	Diff        ReplayDiff    `json:"diff"`
	Summary     string        `json:"summary"`
	Error       string        `json:"error,omitempty"`
}

// ReplayDiff summarizes behavior changes.
type ReplayDiff struct {
	ToolSequenceChanged bool     `json:"tool_sequence_changed"`
	ApprovalChanged     bool     `json:"approval_changed"`
	RiskLevelChanged    bool     `json:"risk_level_changed"`
	CitationChanged     bool     `json:"citation_changed"`
	ExpectedTools       []string `json:"expected_tools,omitempty"`
	ActualTools         []string `json:"actual_tools,omitempty"`
	ExpectedApprovals   []string `json:"expected_approvals,omitempty"`
	ActualApprovals     []string `json:"actual_approvals,omitempty"`
}
