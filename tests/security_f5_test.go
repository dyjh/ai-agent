package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"local-agent/internal/agent"
	"local-agent/internal/api"
	"local-agent/internal/config"
	"local-agent/internal/core"
	"local-agent/internal/events"
	"local-agent/internal/security"
	memstore "local-agent/internal/tools/memory"
)

func TestPolicyProfilesAndRiskTrace(t *testing.T) {
	cfg := config.Default().Policy

	strict := agent.NewPolicyEngine(config.PolicyConfig{
		ActiveProfile: "strict",
		Profiles:      cfg.Profiles,
		Network:       cfg.Network,
	})
	readDecision, err := strict.Decide(context.Background(), core.ToolProposal{Tool: "code.read_file", Input: map[string]any{"path": "main.go"}}, core.EffectInferenceResult{
		Effects:       []string{"read", "code.read"},
		RiskLevel:     "read",
		Confidence:    0.99,
		ReasonSummary: "read-only",
	})
	if err != nil {
		t.Fatalf("strict Decide() error = %v", err)
	}
	if !readDecision.RequiresApproval || readDecision.PolicyProfile != "strict" {
		t.Fatalf("strict read decision = %+v, want approval under strict profile", readDecision)
	}
	if readDecision.RiskTrace == nil || readDecision.RiskTrace.PolicyProfile != "strict" || len(readDecision.RiskTrace.Signals) == 0 {
		t.Fatalf("risk trace missing profile/signals: %+v", readDecision.RiskTrace)
	}

	developer := agent.NewPolicyEngine(config.PolicyConfig{
		ActiveProfile: "developer",
		Profiles:      cfg.Profiles,
		Network:       cfg.Network,
	})
	codeRead, err := developer.Decide(context.Background(), core.ToolProposal{Tool: "code.read_file", Input: map[string]any{"path": "main.go"}}, core.EffectInferenceResult{
		Effects:       []string{"read", "code.read"},
		RiskLevel:     "read",
		Confidence:    0.99,
		ReasonSummary: "read-only",
	})
	if err != nil {
		t.Fatalf("developer read Decide() error = %v", err)
	}
	if codeRead.RequiresApproval {
		t.Fatalf("developer read decision = %+v, want auto", codeRead)
	}
	patch, err := developer.Decide(context.Background(), core.ToolProposal{Tool: "code.apply_patch", Input: map[string]any{"path": "main.go"}}, core.EffectInferenceResult{
		Effects:       []string{"fs.write", "code.modify"},
		RiskLevel:     "write",
		Confidence:    0.99,
		ReasonSummary: "patch modifies files",
	})
	if err != nil {
		t.Fatalf("developer patch Decide() error = %v", err)
	}
	if !patch.RequiresApproval {
		t.Fatalf("developer patch decision = %+v, want approval", patch)
	}

	offline := agent.NewPolicyEngine(config.PolicyConfig{
		ActiveProfile: "offline",
		Profiles:      cfg.Profiles,
		Network:       cfg.Network,
	})
	networkWrite, err := offline.Decide(context.Background(), core.ToolProposal{Tool: "mcp.call_tool", Input: map[string]any{"server_id": "remote"}}, core.EffectInferenceResult{
		Effects:       []string{"network.post"},
		RiskLevel:     "write",
		Confidence:    0.95,
		ReasonSummary: "remote MCP call",
	})
	if err != nil {
		t.Fatalf("offline Decide() error = %v", err)
	}
	if networkWrite.Allowed {
		t.Fatalf("offline network write decision = %+v, want deny", networkWrite)
	}
}

func TestSecretGuardAndSensitiveStorage(t *testing.T) {
	text := "OPENAI_API_KEY=sk-test_abcdefghijklmnop\nDATABASE_URL=postgres://user:passw0rd@example.com/db"
	result := security.ScanText(text)
	if !result.HasSecret || !strings.Contains(result.RedactedText, "[REDACTED]") {
		t.Fatalf("ScanText() = %+v, want redacted findings", result)
	}
	if !security.MustBlockLongTermStorage(result) {
		t.Fatalf("secret scan should block long-term storage")
	}

	guard, err := security.NewSecretGuard([]string{`sk-allowlisted-value`})
	if err != nil {
		t.Fatalf("NewSecretGuard() error = %v", err)
	}
	allowed := guard.ScanText("token=sk-allowlisted-value")
	if allowed.HasSecret {
		t.Fatalf("allowlisted fake token should not produce findings: %+v", allowed)
	}

	store := memstore.NewStore(t.TempDir(), nil)
	_, err = store.CreatePatch(core.MemoryPatch{
		Path: "profile.md",
		Body: "remember token=sk-test_abcdefghijklmnop",
	})
	if err == nil {
		t.Fatalf("expected memory patch with secret to be blocked")
	}
}

func TestNetworkPolicyValidation(t *testing.T) {
	policy := config.Default().Policy.Network
	policy.AllowedDomains = []string{"github.com", "api.openai.com"}
	policy.DeniedDomains = []string{"*.internal"}
	policy.MaxDownloadBytes = 1024

	metadata := security.ValidateNetworkURL(policy, "http://169.254.169.254/latest/meta-data", http.MethodGet, 0)
	if metadata.Allowed {
		t.Fatalf("metadata decision = %+v, want deny", metadata)
	}
	private := security.ValidateNetworkURL(policy, "http://127.0.0.1:8080", http.MethodGet, 0)
	if private.Allowed {
		t.Fatalf("private decision = %+v, want deny", private)
	}
	allowed := security.ValidateNetworkURL(policy, "https://github.com/dyjh/ai-agent", http.MethodGet, 512)
	if !allowed.Allowed || allowed.RequiresApproval {
		t.Fatalf("allowed decision = %+v, want allowed read", allowed)
	}
	post := security.ValidateNetworkURL(policy, "https://github.com/api", http.MethodPost, 512)
	if !post.Allowed || !post.RequiresApproval {
		t.Fatalf("POST decision = %+v, want approval", post)
	}
	tooLarge := security.ValidateNetworkURL(policy, "https://github.com/archive.zip", http.MethodGet, 2048)
	if tooLarge.Allowed {
		t.Fatalf("max bytes decision = %+v, want deny", tooLarge)
	}
}

func TestApprovalExplanationRedactsButSnapshotRemainsExact(t *testing.T) {
	approvals := agent.NewApprovalCenter()
	proposal := core.ToolProposal{
		ID:    "tool_apply",
		Tool:  "code.apply_patch",
		Input: map[string]any{"path": "main.go", "content": "token=sk-test_abcdefghijklmnop"},
	}
	inference := core.EffectInferenceResult{
		Effects:          []string{"fs.write", "code.modify"},
		RiskLevel:        "write",
		ApprovalRequired: true,
		Confidence:       0.99,
		ReasonSummary:    "patch modifies files",
	}
	decision := core.PolicyDecision{
		Allowed:          true,
		RequiresApproval: true,
		RiskLevel:        "write",
		Reason:           "patch modifies files",
		PolicyProfile:    "developer",
		ApprovalPayload:  map[string]any{"effects": inference.Effects},
	}
	record, err := approvals.Create("run1", "conv1", proposal, inference, decision)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if record.Explanation == nil || record.Explanation.Summary == "" || len(record.Explanation.ExpectedEffects) == 0 {
		t.Fatalf("approval explanation missing: %+v", record)
	}
	if strings.Contains(record.Explanation.Summary, "sk-test") || strings.Contains(record.Explanation.WhyNeeded, "sk-test") {
		t.Fatalf("approval explanation leaked fake secret: %+v", record.Explanation)
	}
	if got := record.InputSnapshot["content"]; got != "token=sk-test_abcdefghijklmnop" {
		t.Fatalf("input snapshot = %v, want exact approved snapshot", got)
	}
}

func TestSecurityAPIAndAuditRedaction(t *testing.T) {
	cfg := config.Default()
	cfg.Events.AuditRoot = t.TempDir()
	cfg.Events.JSONLRoot = t.TempDir()

	writer := events.NewJSONLWriter(cfg.Events.JSONLRoot, cfg.Events.AuditRoot)
	if err := writer.WriteRun(core.Event{
		Type:      "approval.requested",
		RunID:     "run_security",
		Tool:      "shell.exec",
		RiskLevel: "sensitive",
		Payload:   map[string]any{"summary": "token=sk-test_abcdefghijklmnop"},
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("WriteRun() error = %v", err)
	}
	rawAudit, err := os.ReadFile(filepath.Join(cfg.Events.AuditRoot, time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if err != nil {
		t.Fatalf("ReadFile(audit) error = %v", err)
	}
	if strings.Contains(string(rawAudit), "sk-test") {
		t.Fatalf("audit log leaked fake secret: %s", string(rawAudit))
	}

	server := httptest.NewServer(api.NewRouter(api.Dependencies{Config: cfg}))
	defer server.Close()

	var profiles map[string]any
	mustRequestJSON(t, http.MethodGet, server.URL+"/v1/security/policy-profiles", nil, &profiles)
	if len(profiles["items"].([]any)) == 0 {
		t.Fatalf("expected policy profiles")
	}

	var scan map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/security/secret-scan", map[string]any{
		"text": "Authorization: Bearer fake-token-value",
	}, &scan)
	if scan["has_secret"] != true {
		t.Fatalf("secret scan response = %+v, want has_secret", scan)
	}
	rawScan, _ := json.Marshal(scan)
	if strings.Contains(string(rawScan), "fake-token-value") {
		t.Fatalf("secret scan response leaked token: %s", string(rawScan))
	}

	var network map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/security/network-policy/validate-url", map[string]any{
		"url": "http://169.254.169.254/latest/meta-data",
	}, &network)
	if network["allowed"] != false {
		t.Fatalf("network validation = %+v, want denied", network)
	}

	var audit map[string]any
	mustRequestJSON(t, http.MethodGet, server.URL+"/v1/security/audit/runs/run_security", nil, &audit)
	if audit["total"].(float64) == 0 {
		t.Fatalf("expected audit events")
	}
	rawAuditAPI, _ := json.Marshal(audit)
	if strings.Contains(string(rawAuditAPI), "sk-test") {
		t.Fatalf("audit API leaked fake secret: %s", string(rawAuditAPI))
	}
}
