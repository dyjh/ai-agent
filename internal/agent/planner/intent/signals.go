package intent

// IntentDomain is the broad planning domain for a normalized request.
type IntentDomain string

const (
	DomainChat     IntentDomain = "chat"
	DomainCode     IntentDomain = "code"
	DomainGit      IntentDomain = "git"
	DomainOps      IntentDomain = "ops"
	DomainRAG      IntentDomain = "rag"
	DomainMemory   IntentDomain = "memory"
	DomainSkill    IntentDomain = "skill"
	DomainMCP      IntentDomain = "mcp"
	DomainSecurity IntentDomain = "security"
	DomainEval     IntentDomain = "eval"
	DomainRun      IntentDomain = "run"
	DomainApproval IntentDomain = "approval"
)

// IntentClassification captures intent without selecting an executor.
type IntentClassification struct {
	Domain      IntentDomain `json:"domain"`
	Intent      string       `json:"intent"`
	Confidence  float64      `json:"confidence"`
	Signals     []string     `json:"signals,omitempty"`
	NeedTool    bool         `json:"need_tool"`
	NeedClarify bool         `json:"need_clarify"`
	Reason      string       `json:"reason,omitempty"`
}
