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
	"local-agent/internal/tools/skills"
)

func TestAPISmoke(t *testing.T) {
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

	server := httptest.NewServer(api.NewRouter(api.Dependencies{
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
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/health")
	if err != nil {
		t.Fatalf("GET health: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", resp.StatusCode)
	}
	var health map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	knowledgeBase, ok := health["knowledge_base"].(map[string]any)
	if !ok {
		t.Fatalf("expected knowledge_base health payload")
	}
	if knowledgeBase["status"] != "disabled" {
		t.Fatalf("knowledge_base status = %v, want disabled", knowledgeBase["status"])
	}

	var conversation struct {
		ID string `json:"id"`
	}
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/conversations", map[string]any{"title": "smoke"}, &conversation)
	if conversation.ID == "" {
		t.Fatalf("expected conversation id")
	}

	var runResp map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/conversations/"+conversation.ID+"/messages", map[string]any{
		"content": "你好，介绍一下你自己",
	}, &runResp)
	if runResp["run_id"] == "" {
		t.Fatalf("expected run_id")
	}

	var messages map[string][]map[string]any
	mustRequestJSON(t, http.MethodGet, server.URL+"/v1/conversations/"+conversation.ID+"/messages", nil, &messages)
	if len(messages["items"]) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(messages["items"]))
	}

	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/conversations/"+conversation.ID+"/messages", map[string]any{
		"content": "tool_id: shell.exec 请帮我安装 axios 依赖",
	}, &runResp)

	var approvals map[string][]map[string]any
	mustRequestJSON(t, http.MethodGet, server.URL+"/v1/approvals/pending", nil, &approvals)
	if len(approvals["items"]) == 0 {
		t.Fatalf("expected pending approvals")
	}

	var kbHealth map[string]any
	mustRequestJSON(t, http.MethodGet, server.URL+"/v1/kbs/health", nil, &kbHealth)
	if kbHealth["status"] != "disabled" {
		t.Fatalf("kb health status = %v, want disabled", kbHealth["status"])
	}

	skillRoot := createSkillFixture(t, skillFixtureOptions{
		ID:             "api_skill",
		Effects:        []string{"process.read"},
		SandboxProfile: skills.SandboxProfileBestEffortLocal,
		Script:         "#!/bin/sh\nprintf '{\"ok\":true}'\n",
	})

	var skill map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/skills/upload", map[string]any{
		"path": skillRoot,
	}, &skill)
	if skill["id"] != "api_skill" {
		t.Fatalf("skill id = %v, want api_skill", skill["id"])
	}

	var skillDetail map[string]any
	mustRequestJSON(t, http.MethodGet, server.URL+"/v1/skills/api_skill", nil, &skillDetail)
	if _, ok := skillDetail["manifest"].(map[string]any); !ok {
		t.Fatalf("expected skill manifest in detail response")
	}

	var runSkill map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/skills/api_skill/run", map[string]any{}, &runSkill)
	result, ok := runSkill["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected run result")
	}
	if result["status"] != nil {
		t.Fatalf("unexpected top-level result status payload: %v", result["status"])
	}
	output, ok := result["output"].(map[string]any)
	if !ok {
		t.Fatalf("expected tool output payload")
	}
	if output["status"] != "ok" {
		t.Fatalf("skill run status = %v, want ok", output["status"])
	}
}

func mustRequestJSON(t *testing.T, method, url string, body any, target any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body = %s", resp.StatusCode, string(data))
	}
	if target == nil {
		return
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
}

func containsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}
