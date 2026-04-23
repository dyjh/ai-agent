package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"local-agent/internal/api"
	"local-agent/internal/app"
	"local-agent/internal/config"
)

func TestRunAPIListGetStepsResumeCancel(t *testing.T) {
	server := newRunAPIServer(t)
	defer server.Close()

	conversationID := createConversationForRunAPI(t, server.URL)
	runID, approvalID := createApprovalRun(t, server.URL, conversationID)

	var runs map[string][]map[string]any
	mustRequestJSON(t, http.MethodGet, server.URL+"/v1/runs?status=paused_for_approval", nil, &runs)
	if len(runs["items"]) == 0 {
		t.Fatalf("expected paused runs")
	}
	if !containsRun(runs["items"], runID) {
		t.Fatalf("expected run %s in list", runID)
	}

	var run map[string]any
	mustRequestJSON(t, http.MethodGet, server.URL+"/v1/runs/"+runID, nil, &run)
	if run["status"] != "paused_for_approval" {
		t.Fatalf("status = %v, want paused_for_approval", run["status"])
	}

	var steps map[string][]map[string]any
	mustRequestJSON(t, http.MethodGet, server.URL+"/v1/runs/"+runID+"/steps", nil, &steps)
	if len(steps["items"]) == 0 {
		t.Fatalf("expected run steps")
	}

	var resumed map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/runs/"+runID+"/resume", map[string]any{
		"approval_id": approvalID,
		"approved":    false,
	}, &resumed)
	runState, ok := resumed["state"].(map[string]any)
	if !ok {
		t.Fatalf("expected run state in resume response")
	}
	if runState["status"] != "approval_rejected" {
		t.Fatalf("status = %v, want approval_rejected", runState["status"])
	}

	status, _ := requestJSONStatus(http.MethodPost, server.URL+"/v1/runs/"+runID+"/resume", map[string]any{
		"approval_id": approvalID,
		"approved":    true,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("resume terminal run status = %d, want 400", status)
	}

	pausedRunID, _ := createApprovalRun(t, server.URL, conversationID)
	var cancelled map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/runs/"+pausedRunID+"/cancel", map[string]any{}, &cancelled)
	if cancelled["status"] != "cancelled" {
		t.Fatalf("cancelled status = %v, want cancelled", cancelled["status"])
	}
}

func newRunAPIServer(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := config.Default()
	cfg.Database.URL = ""
	cfg.Memory.RootDir = t.TempDir()
	cfg.Events.JSONLRoot = t.TempDir()
	cfg.Events.AuditRoot = t.TempDir()
	cfg.Vector.Backend = config.VectorBackendMemory
	cfg.Vector.EmbeddingDimension = 16
	cfg.KB.Enabled = false
	cfg.KB.Provider = ""

	bootstrap, err := app.NewBootstrap(context.Background(), cfg, slog.Default())
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	return httptest.NewServer(api.NewRouter(api.Dependencies{
		Logger:    bootstrap.Logger,
		Config:    bootstrap.Config,
		Store:     bootstrap.Store,
		Runtime:   bootstrap.Runtime,
		Approvals: bootstrap.Approvals,
		Router:    bootstrap.Router,
		Memory:    bootstrap.Memory,
		Knowledge: bootstrap.Knowledge,
		Skills:    bootstrap.Skills,
		MCP:       bootstrap.MCP,
	}))
}

func createConversationForRunAPI(t *testing.T, baseURL string) string {
	t.Helper()
	var conversation struct {
		ID string `json:"id"`
	}
	mustRequestJSON(t, http.MethodPost, baseURL+"/v1/conversations", map[string]any{"title": "runs"}, &conversation)
	return conversation.ID
}

func createApprovalRun(t *testing.T, baseURL, conversationID string) (string, string) {
	t.Helper()
	var response map[string]any
	mustRequestJSON(t, http.MethodPost, baseURL+"/v1/conversations/"+conversationID+"/messages", map[string]any{
		"content": "请帮我安装 axios 依赖",
	}, &response)
	approval, ok := response["approval"].(map[string]any)
	if !ok {
		t.Fatalf("expected approval in response: %#v", response)
	}
	return response["run_id"].(string), approval["id"].(string)
}

func requestJSONStatus(method, url string, body any) (int, []byte) {
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, url, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func containsRun(items []map[string]any, runID string) bool {
	for _, item := range items {
		if item["run_id"] == runID {
			return true
		}
	}
	return false
}
