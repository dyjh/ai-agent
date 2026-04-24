package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local-agent/internal/api"
	"local-agent/internal/app"
	"local-agent/internal/config"
	"local-agent/internal/tools/skills"
)

func TestSkillAPIUploadZipValidateRunAndRemove(t *testing.T) {
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

	skillID := "zip_api_skill_" + strings.ReplaceAll(filepath.Base(t.TempDir()), "-", "_")
	root := createSkillFixture(t, skillFixtureOptions{
		ID:             skillID,
		Effects:        []string{"process.read"},
		SandboxProfile: skills.SandboxProfileBestEffortLocal,
		Script:         "#!/bin/sh\nprintf '{\"ok\":true}'\n",
	})
	zipPath := createSkillZipFromDir(t, root)

	var uploadResp map[string]any
	mustRequestMultipart(t, server.URL+"/v1/skills/upload-zip", zipPath, &uploadResp)
	pkg := uploadResp["package"].(map[string]any)
	if pkg["skill_id"] != skillID {
		t.Fatalf("package.skill_id = %v, want %s", pkg["skill_id"], skillID)
	}
	if pkg["checksum"] == "" {
		t.Fatalf("expected checksum in upload response")
	}

	var manifestResp map[string]any
	mustRequestJSON(t, http.MethodGet, server.URL+"/v1/skills/"+skillID+"/manifest", nil, &manifestResp)
	manifest := manifestResp["manifest"].(map[string]any)
	if manifest["id"] != skillID {
		t.Fatalf("manifest.id = %v, want %s", manifest["id"], skillID)
	}

	var validateResp map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/skills/"+skillID+"/validate", map[string]any{"args": map[string]any{}}, &validateResp)
	if validateResp["status"] != "ok" && validateResp["status"] != "warning" {
		t.Fatalf("validate status = %v, want ok or warning", validateResp["status"])
	}
	validation := validateResp["validation"].(map[string]any)
	if _, ok := validation["execution_profile"].(map[string]any); !ok {
		t.Fatalf("expected execution_profile in validation response")
	}

	var packageResp map[string]any
	mustRequestJSON(t, http.MethodGet, server.URL+"/v1/skills/"+skillID+"/package", nil, &packageResp)
	if packageResp["skill_id"] != skillID {
		t.Fatalf("package skill_id = %v, want %s", packageResp["skill_id"], skillID)
	}

	var runResp map[string]any
	mustRequestJSON(t, http.MethodPost, server.URL+"/v1/skills/"+skillID+"/run", map[string]any{"args": map[string]any{}}, &runResp)
	result := runResp["result"].(map[string]any)
	output := result["output"].(map[string]any)
	if output["status"] != "ok" {
		t.Fatalf("run status = %v, want ok", output["status"])
	}

	var removeResp map[string]any
	mustRequestJSON(t, http.MethodDelete, server.URL+"/v1/skills/"+skillID, nil, &removeResp)
	if removed, _ := removeResp["removed"].(bool); !removed {
		t.Fatalf("expected removed=true, got %v", removeResp["removed"])
	}
}

func mustRequestMultipart(t *testing.T, url, filePath string, target any) {
	t.Helper()

	file, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("Open(file) error = %v", err)
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		t.Fatalf("Copy(file) error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close(writer) error = %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body = %s", resp.StatusCode, string(data))
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
}
