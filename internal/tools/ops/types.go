package ops

import "time"

const (
	HostTypeLocal  = "local"
	HostTypeSSH    = "ssh"
	HostTypeDocker = "docker"
	HostTypeK8s    = "k8s"
)

// HostProfile describes a local or remote operations target.
type HostProfile struct {
	HostID           string            `json:"host_id"`
	Name             string            `json:"name"`
	Type             string            `json:"type"`
	DefaultShell     string            `json:"default_shell,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	PolicyProfile    string            `json:"policy_profile,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	SSH              *SSHHostConfig    `json:"ssh,omitempty"`
	K8s              *K8sHostConfig    `json:"k8s,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// HostProfileInput is the create/update payload for host profiles.
type HostProfileInput struct {
	Name             string            `json:"name"`
	Type             string            `json:"type"`
	DefaultShell     string            `json:"default_shell,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	PolicyProfile    string            `json:"policy_profile,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	SSH              *SSHHostConfig    `json:"ssh,omitempty"`
	K8s              *K8sHostConfig    `json:"k8s,omitempty"`
}

// SSHHostConfig stores non-secret SSH connection metadata.
type SSHHostConfig struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	AuthType    string `json:"auth_type"`
	KeyPath     string `json:"key_path,omitempty"`
	PasswordRef string `json:"password_ref,omitempty"`
}

// K8sHostConfig stores Kubernetes client selection metadata.
type K8sHostConfig struct {
	KubeconfigPath string `json:"kubeconfig_path,omitempty"`
	Context        string `json:"context,omitempty"`
	Namespace      string `json:"namespace,omitempty"`
}

// HostTestResult is returned by host connectivity checks.
type HostTestResult struct {
	HostID string `json:"host_id"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// RollbackPlan describes the best-effort recovery path for a write operation.
type RollbackPlan struct {
	CanRollback          bool     `json:"can_rollback"`
	PreviousStateSummary string   `json:"previous_state_summary,omitempty"`
	RollbackSteps        []string `json:"rollback_steps,omitempty"`
	BackupPaths          []string `json:"backup_paths,omitempty"`
	RiskNote             string   `json:"risk_note,omitempty"`
}

// Runbook is a parsed Markdown runbook.
type Runbook struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Scope      string            `json:"scope,omitempty"`
	HostType   string            `json:"host_type,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Body       string            `json:"body"`
	SourcePath string            `json:"source_path"`
	StepTexts  []string          `json:"step_texts,omitempty"`
	LoadedAt   time.Time         `json:"loaded_at"`
}

// RunbookPlan is a dry-run plan derived from a runbook.
type RunbookPlan struct {
	RunbookID string            `json:"runbook_id"`
	Title     string            `json:"title"`
	HostID    string            `json:"host_id,omitempty"`
	DryRun    bool              `json:"dry_run"`
	Steps     []RunbookPlanStep `json:"steps"`
}

// RunbookPlanStep is one executable or explanatory runbook step.
type RunbookPlanStep struct {
	Index            int            `json:"index"`
	Text             string         `json:"text"`
	Tool             string         `json:"tool,omitempty"`
	Input            map[string]any `json:"input,omitempty"`
	RequiresApproval bool           `json:"requires_approval"`
	Reason           string         `json:"reason,omitempty"`
}

// RunbookExecuteRequest controls runbook execution.
type RunbookExecuteRequest struct {
	HostID   string `json:"host_id,omitempty"`
	DryRun   bool   `json:"dry_run,omitempty"`
	MaxSteps int    `json:"max_steps,omitempty"`
}
