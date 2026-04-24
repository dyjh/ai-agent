package core

import "time"

// ToolSpec describes a tool exposed to the planner/runtime.
type ToolSpec struct {
	ID             string         `json:"id"`
	Provider       string         `json:"provider"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	InputSchema    map[string]any `json:"input_schema"`
	DefaultEffects []string       `json:"default_effects"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// ToolProposal is the only structure the model may emit for side effects.
type ToolProposal struct {
	ID              string         `json:"id"`
	Tool            string         `json:"tool"`
	Input           map[string]any `json:"input"`
	Purpose         string         `json:"purpose"`
	ExpectedEffects []string       `json:"expected_effects"`
	CreatedAt       time.Time      `json:"created_at"`
}

// ToolResult captures executor output.
type ToolResult struct {
	ToolCallID string         `json:"tool_call_id"`
	Output     map[string]any `json:"output"`
	Error      string         `json:"error,omitempty"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
}

// EffectInferenceResult is derived from structure-aware proposal analysis.
type EffectInferenceResult struct {
	Effects          []string `json:"effects"`
	RiskLevel        string   `json:"risk_level"`
	Sensitive        bool     `json:"sensitive"`
	ApprovalRequired bool     `json:"approval_required"`
	Confidence       float64  `json:"confidence"`
	ReasonSummary    string   `json:"reason_summary"`
}

// PolicyDecision controls whether execution is automatic or gated by approval.
type PolicyDecision struct {
	Allowed          bool           `json:"allowed"`
	RequiresApproval bool           `json:"requires_approval"`
	RiskLevel        string         `json:"risk_level"`
	Reason           string         `json:"reason"`
	ApprovalPayload  map[string]any `json:"approval_payload,omitempty"`
}

// ApprovalStatus tracks approval lifecycle.
type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
)

// ApprovalRecord stores an immutable input snapshot awaiting resolution.
type ApprovalRecord struct {
	ID             string                `json:"id"`
	RunID          string                `json:"run_id,omitempty"`
	ConversationID string                `json:"conversation_id,omitempty"`
	Proposal       ToolProposal          `json:"proposal"`
	Inference      EffectInferenceResult `json:"inference"`
	Decision       PolicyDecision        `json:"decision"`
	InputSnapshot  map[string]any        `json:"input_snapshot"`
	SnapshotHash   string                `json:"snapshot_hash"`
	Summary        string                `json:"summary"`
	Status         ApprovalStatus        `json:"status"`
	Reason         string                `json:"reason,omitempty"`
	CreatedAt      time.Time             `json:"created_at"`
	ResolvedAt     *time.Time            `json:"resolved_at,omitempty"`
}

// Conversation represents a chat session.
type Conversation struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	ProjectKey string    `json:"project_key,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Archived   bool      `json:"archived"`
}

// Message represents a chat message.
type Message struct {
	ID             string         `json:"id"`
	ConversationID string         `json:"conversation_id"`
	Role           string         `json:"role"`
	Content        string         `json:"content,omitempty"`
	ContentJSON    map[string]any `json:"content_json,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

// MessageUsage stores token accounting.
type MessageUsage struct {
	ID             string    `json:"id"`
	MessageID      string    `json:"message_id"`
	ConversationID string    `json:"conversation_id"`
	Model          string    `json:"model,omitempty"`
	InputTokens    int       `json:"input_tokens"`
	OutputTokens   int       `json:"output_tokens"`
	TotalTokens    int       `json:"total_tokens"`
	ToolCallCount  int       `json:"tool_call_count"`
	CreatedAt      time.Time `json:"created_at"`
}

// ConversationUsageRollup stores aggregated usage.
type ConversationUsageRollup struct {
	ConversationID    string    `json:"conversation_id"`
	TotalInputTokens  int64     `json:"total_input_tokens"`
	TotalOutputTokens int64     `json:"total_output_tokens"`
	TotalTokens       int64     `json:"total_tokens"`
	TotalMessages     int64     `json:"total_messages"`
	TotalRuns         int64     `json:"total_runs"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// AgentEvent stores an event row.
type AgentEvent struct {
	ID             string         `json:"id"`
	ConversationID string         `json:"conversation_id,omitempty"`
	RunID          string         `json:"run_id,omitempty"`
	EventType      string         `json:"event_type"`
	Payload        map[string]any `json:"payload,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

// Event is the JSONL/audit event shape.
type Event struct {
	Type           string         `json:"type"`
	RunID          string         `json:"run_id,omitempty"`
	StepID         string         `json:"step_id,omitempty"`
	StepIndex      int            `json:"step_index,omitempty"`
	ConversationID string         `json:"conversation_id,omitempty"`
	ApprovalID     string         `json:"approval_id,omitempty"`
	Tool           string         `json:"tool,omitempty"`
	ToolCallID     string         `json:"tool_call_id,omitempty"`
	RiskLevel      string         `json:"risk_level,omitempty"`
	Content        string         `json:"content,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

// AgentRunRecord is the durable repository shape for a workflow run.
type AgentRunRecord struct {
	RunID            string         `json:"run_id"`
	ConversationID   string         `json:"conversation_id"`
	Status           string         `json:"status"`
	CurrentStep      string         `json:"current_step,omitempty"`
	CurrentStepIndex int            `json:"current_step_index,omitempty"`
	StepCount        int            `json:"step_count,omitempty"`
	MaxSteps         int            `json:"max_steps,omitempty"`
	UserMessage      string         `json:"user_message,omitempty"`
	ApprovalID       string         `json:"approval_id,omitempty"`
	Error            string         `json:"error,omitempty"`
	StateJSON        map[string]any `json:"state_json,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// AgentRunStepRecord is the durable repository shape for one workflow step.
type AgentRunStepRecord struct {
	StepID         string         `json:"step_id"`
	RunID          string         `json:"run_id"`
	StepIndex      int            `json:"step_index"`
	StepType       string         `json:"step_type"`
	Status         string         `json:"status"`
	ProposalJSON   map[string]any `json:"proposal_json,omitempty"`
	InferenceJSON  map[string]any `json:"inference_json,omitempty"`
	PolicyJSON     map[string]any `json:"policy_json,omitempty"`
	ApprovalJSON   map[string]any `json:"approval_json,omitempty"`
	ToolResultJSON map[string]any `json:"tool_result_json,omitempty"`
	Summary        string         `json:"summary,omitempty"`
	Error          string         `json:"error,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// MemoryFile is a Markdown-backed memory document.
type MemoryFile struct {
	Path        string            `json:"path"`
	Frontmatter map[string]string `json:"frontmatter,omitempty"`
	Body        string            `json:"body"`
}

// MemoryPatch represents a proposed markdown mutation.
type MemoryPatch struct {
	ID          string            `json:"id"`
	Path        string            `json:"path"`
	Frontmatter map[string]string `json:"frontmatter,omitempty"`
	Body        string            `json:"body"`
	Summary     string            `json:"summary"`
	Sensitive   bool              `json:"sensitive"`
	CreatedAt   time.Time         `json:"created_at"`
}

// KnowledgeBase stores metadata for an indexed KB.
type KnowledgeBase struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// KBChunk represents a searchable chunk.
type KBChunk struct {
	ID       string            `json:"id"`
	KBID     string            `json:"kb_id"`
	Document string            `json:"document"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Score    float64           `json:"score,omitempty"`
}

// SkillRegistration stores uploaded skill metadata.
type SkillRegistration struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Version         string    `json:"version,omitempty"`
	Description     string    `json:"description,omitempty"`
	ArchivePath     string    `json:"archive_path,omitempty"`
	RuntimeType     string    `json:"runtime_type,omitempty"`
	Effects         []string  `json:"effects,omitempty"`
	ApprovalDefault string    `json:"approval_default,omitempty"`
	SourceType      string    `json:"source_type,omitempty"`
	Checksum        string    `json:"checksum,omitempty"`
	InstalledAt     time.Time `json:"installed_at,omitempty"`
	SandboxProfile  string    `json:"sandbox_profile,omitempty"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
}

// SkillPackageInfo stores package/install metadata for a skill.
type SkillPackageInfo struct {
	SkillID     string    `json:"skill_id"`
	Version     string    `json:"version"`
	SourceType  string    `json:"source_type"`
	PackagePath string    `json:"package_path"`
	Checksum    string    `json:"checksum,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
}

// SkillPolicyProfile describes the effect/approval model for a registered skill.
type SkillPolicyProfile struct {
	ID              string   `json:"id"`
	Effects         []string `json:"effects"`
	ApprovalDefault string   `json:"approval_default,omitempty"`
	Enabled         bool     `json:"enabled"`
}

// MCPServer stores an MCP server config.
type MCPServer struct {
	ID             string            `json:"id" yaml:"id"`
	Name           string            `json:"name" yaml:"name"`
	Transport      string            `json:"transport" yaml:"transport"`
	Command        string            `json:"command,omitempty" yaml:"command,omitempty"`
	Args           []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Cwd            string            `json:"cwd,omitempty" yaml:"cwd,omitempty"`
	URL            string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers        map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Enabled        bool              `json:"enabled" yaml:"enabled"`
	Environment    map[string]string `json:"environment,omitempty" yaml:"env,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
	CreatedAt      time.Time         `json:"created_at" yaml:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at" yaml:"updated_at"`
}

// MCPToolPolicy stores local policy overrides for MCP tools.
type MCPToolPolicy struct {
	ID               string    `json:"id" yaml:"id"`
	ToolName         string    `json:"tool_name" yaml:"tool_name"`
	Effects          []string  `json:"effects,omitempty" yaml:"effects,omitempty"`
	Approval         string    `json:"approval,omitempty" yaml:"approval,omitempty"`
	RequiresApproval bool      `json:"requires_approval" yaml:"requires_approval"`
	RiskLevel        string    `json:"risk_level" yaml:"risk_level"`
	Reason           string    `json:"reason,omitempty" yaml:"reason,omitempty"`
	UpdatedAt        time.Time `json:"updated_at" yaml:"updated_at"`
}

// MCPPolicyProfile is the normalized effect model for a concrete MCP tool.
type MCPPolicyProfile struct {
	ServerID         string   `json:"server_id"`
	ToolName         string   `json:"tool_name"`
	Effects          []string `json:"effects"`
	Approval         string   `json:"approval,omitempty"`
	RequiresApproval bool     `json:"requires_approval"`
	RiskLevel        string   `json:"risk_level"`
	Reason           string   `json:"reason,omitempty"`
	Known            bool     `json:"known"`
	Enabled          bool     `json:"enabled"`
	Confidence       float64  `json:"confidence"`
}
