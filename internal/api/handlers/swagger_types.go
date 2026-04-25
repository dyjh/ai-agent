package handlers

import (
	"time"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
	"local-agent/internal/security"
	kbtools "local-agent/internal/tools/kb"
	"local-agent/internal/tools/mcp"
	memstore "local-agent/internal/tools/memory"
	"local-agent/internal/tools/ops"
	"local-agent/internal/tools/skills"
)

// LegacyErrorResponse matches older handlers that still emit {"error": "..."}.
type LegacyErrorResponse struct {
	Error string `json:"error"`
}

// StatusResponse is a minimal success envelope.
type StatusResponse struct {
	Status string `json:"status"`
}

// HealthResponse documents GET /v1/health.
type HealthResponse struct {
	Status        string         `json:"status"`
	Service       string         `json:"service"`
	Version       string         `json:"version"`
	Timestamp     time.Time      `json:"timestamp"`
	Server        map[string]any `json:"server"`
	Database      map[string]any `json:"database"`
	LLM           map[string]any `json:"llm"`
	Embeddings    map[string]any `json:"embeddings"`
	Qdrant        map[string]any `json:"qdrant"`
	KnowledgeBase map[string]any `json:"knowledge_base"`
	Workflow      map[string]any `json:"workflow"`
	Docs          map[string]any `json:"docs"`
	Vector        map[string]any `json:"vector"`
}

// CreateConversationRequest is the input for creating a conversation.
type CreateConversationRequest struct {
	Title      string `json:"title"`
	ProjectKey string `json:"project_key"`
}

// ConversationListResponse wraps a conversation list.
type ConversationListResponse struct {
	Items []core.Conversation `json:"items"`
}

// PostMessageRequest is the user message payload.
type PostMessageRequest struct {
	Content string `json:"content"`
}

// MessageListResponse wraps a message list.
type MessageListResponse struct {
	Items []core.Message `json:"items"`
}

// ApprovalListResponse wraps pending approvals.
type ApprovalListResponse struct {
	Items []core.ApprovalRecord `json:"items"`
}

// RejectApprovalRequest is the approval rejection payload.
type RejectApprovalRequest struct {
	Reason string `json:"reason"`
}

// ApprovalResolutionResponse documents approval resolution results.
type ApprovalResolutionResponse struct {
	Approval *core.ApprovalRecord `json:"approval,omitempty"`
	Result   *core.ToolResult     `json:"result,omitempty"`
	Run      *agent.RunResponse   `json:"run,omitempty"`
}

// RunListResponse wraps run list responses.
type RunListResponse struct {
	Items []agent.RunState `json:"items"`
}

// RunStepListResponse wraps run step list responses.
type RunStepListResponse struct {
	Items []agent.RunStep `json:"items"`
}

// ResumeRunRequest is the resume payload for paused runs.
type ResumeRunRequest struct {
	ApprovalID string `json:"approval_id"`
	Approved   bool   `json:"approved"`
}

// MemoryFileListResponse wraps memory file lists.
type MemoryFileListResponse struct {
	Items []core.MemoryFile `json:"items"`
}

// MemoryFilePathListResponse wraps memory file path lists.
type MemoryFilePathListResponse struct {
	Items []string `json:"items"`
}

// SearchRequest is a generic query+limit payload.
type SearchRequest struct {
	Query   string         `json:"query"`
	Limit   int            `json:"limit"`
	TopK    int            `json:"top_k,omitempty"`
	Mode    string         `json:"mode,omitempty"`
	Rerank  bool           `json:"rerank,omitempty"`
	Filters map[string]any `json:"filters,omitempty"`
}

// CreateMemoryPatchRequest creates a markdown memory patch.
type CreateMemoryPatchRequest struct {
	Path        string            `json:"path"`
	Summary     string            `json:"summary"`
	Body        string            `json:"body"`
	Frontmatter map[string]string `json:"frontmatter"`
}

// MemoryItemListResponse wraps memory item lists.
type MemoryItemListResponse struct {
	Items []memstore.MemoryItem `json:"items"`
}

// MemoryReviewListResponse wraps memory review queue items.
type MemoryReviewListResponse struct {
	Items []memstore.MemoryReviewItem `json:"items"`
}

// MemoryExtractReviewRequest extracts candidate memories into review queue.
type MemoryExtractReviewRequest struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id,omitempty"`
	Text           string `json:"text"`
	ProjectKey     string `json:"project_key,omitempty"`
}

// MemoryReviewDecisionRequest records a review decision note.
type MemoryReviewDecisionRequest struct {
	Note string `json:"note"`
}

// MemoryItemCreateRequest creates one memory item through ToolRouter.
type MemoryItemCreateRequest = memstore.MemoryItemCreateInput

// MemoryItemUpdateRequest updates one memory item through ToolRouter.
type MemoryItemUpdateRequest = memstore.MemoryItemUpdateInput

// KnowledgeBaseListResponse wraps knowledge base lists.
type KnowledgeBaseListResponse struct {
	Items []core.KnowledgeBase `json:"items"`
}

// KnowledgeBaseItem documents one knowledge base record.
type KnowledgeBaseItem struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateKnowledgeBaseRequest creates a new KB.
type CreateKnowledgeBaseRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// KnowledgeBaseDocumentUploadRequest uploads raw document content to a KB.
type KnowledgeBaseDocumentUploadRequest struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// KBChunkListResponse wraps KB chunk search/upload results.
type KBChunkListResponse struct {
	Items []core.KBChunk `json:"items"`
}

// KnowledgeSourceListResponse wraps KB source lists.
type KnowledgeSourceListResponse struct {
	Items []kbtools.KnowledgeSource `json:"items"`
}

// KnowledgeIndexJobListResponse wraps KB index job lists.
type KnowledgeIndexJobListResponse struct {
	Items []kbtools.KnowledgeIndexJob `json:"items"`
}

// RetrievalResultListResponse wraps citation-aware retrieval results.
type RetrievalResultListResponse struct {
	Items []kbtools.RetrievalResult `json:"items"`
}

// KBAnswerResponse documents grounded KB answers.
type KBAnswerResponse struct {
	Answer kbtools.AnswerResult `json:"answer"`
}

// RAGEvalCaseListResponse wraps RAG eval cases.
type RAGEvalCaseListResponse struct {
	Items []kbtools.RAGEvalCase `json:"items"`
}

// RAGEvalRunRequest starts selected RAG eval cases.
type RAGEvalRunRequest struct {
	CaseIDs []string `json:"case_ids,omitempty"`
}

// KnowledgeBaseHealthResponse documents KB runtime health.
type KnowledgeBaseHealthResponse struct {
	Enabled        bool              `json:"enabled,omitempty"`
	Provider       string            `json:"provider,omitempty"`
	Status         string            `json:"status,omitempty"`
	Error          string            `json:"error,omitempty"`
	VectorBackend  string            `json:"vector_backend,omitempty"`
	FallbackReason string            `json:"fallback_reason,omitempty"`
	Qdrant         string            `json:"qdrant,omitempty"`
	Collections    map[string]string `json:"collections,omitempty"`
}

// SkillListResponse wraps skill registry lists.
type SkillListResponse struct {
	Items []core.SkillRegistration `json:"items"`
}

// SkillDetailResponse documents the combined skill detail payload.
type SkillDetailResponse struct {
	Skill    core.SkillRegistration `json:"skill"`
	Manifest skills.Manifest        `json:"manifest"`
	Package  core.SkillPackageInfo  `json:"package"`
}

// SkillManifestResponse documents the skill manifest payload.
type SkillManifestResponse struct {
	Skill    core.SkillRegistration `json:"skill"`
	Manifest skills.Manifest        `json:"manifest"`
}

// SkillRunRequestBody is the request payload for validate/test/run.
type SkillRunRequestBody struct {
	Args map[string]any `json:"args"`
}

// ToolRouteResponse documents ToolRouter-mediated execution responses.
type ToolRouteResponse struct {
	Approval  *core.ApprovalRecord        `json:"approval,omitempty"`
	Decision  *core.PolicyDecision        `json:"decision,omitempty"`
	Inference *core.EffectInferenceResult `json:"inference,omitempty"`
	Result    *core.ToolResult            `json:"result,omitempty"`
}

// MCPServerListResponse wraps MCP server lists.
type MCPServerListResponse struct {
	Items []core.MCPServer `json:"items"`
}

// MCPServerDetailResponse documents one MCP server plus runtime cache state.
type MCPServerDetailResponse struct {
	Server core.MCPServer   `json:"server"`
	State  mcp.RuntimeState `json:"state"`
}

// MCPRefreshResponse documents tool refresh results.
type MCPRefreshResponse struct {
	Status string              `json:"status"`
	Tools  []mcp.MCPToolSchema `json:"tools"`
	State  mcp.RuntimeState    `json:"state"`
}

// MCPTestServerResponse documents MCP health test results.
type MCPTestServerResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// MCPToolPolicyListResponse wraps tool policy lists.
type MCPToolPolicyListResponse struct {
	Items []core.MCPToolPolicy `json:"items"`
}

// MCPCallToolRequest is the payload for MCP tool invocation.
type MCPCallToolRequest struct {
	Arguments map[string]any `json:"arguments"`
	Purpose   string         `json:"purpose"`
}

// OpsHostListResponse wraps host profile lists.
type OpsHostListResponse struct {
	Items []ops.HostProfile `json:"items"`
}

// OpsRunbookListResponse wraps runbook lists.
type OpsRunbookListResponse struct {
	Items []ops.Runbook `json:"items"`
}

// OpsRunbookPlanRequest is the runbook plan payload.
type OpsRunbookPlanRequest struct {
	HostID string `json:"host_id,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
}

// PolicyProfileListResponse wraps configured policy profiles.
type PolicyProfileListResponse struct {
	Active string                 `json:"active"`
	Items  []config.PolicyProfile `json:"items"`
}

// PolicyProfileValidateResponse reports profile validation status.
type PolicyProfileValidateResponse struct {
	Valid   bool                  `json:"valid"`
	Error   string                `json:"error,omitempty"`
	Profile *config.PolicyProfile `json:"profile,omitempty"`
}

// SecretScanRequest scans text or a structured payload for secret-like values.
type SecretScanRequest struct {
	Text    string         `json:"text,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

// NetworkValidateURLRequest validates one outbound URL.
type NetworkValidateURLRequest struct {
	URL              string `json:"url"`
	Method           string `json:"method,omitempty"`
	MaxDownloadBytes int64  `json:"max_download_bytes,omitempty"`
}

// SecurityAuditReport summarizes redacted security-relevant events.
type SecurityAuditReport struct {
	Total  int                        `json:"total"`
	Counts map[string]int             `json:"counts"`
	Items  []core.Event               `json:"items"`
	Scan   *security.SecretScanResult `json:"scan,omitempty"`
}
