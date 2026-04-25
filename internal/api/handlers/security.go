package handlers

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"local-agent/internal/config"
	"local-agent/internal/core"
	"local-agent/internal/security"
)

// SecurityHandler serves policy, secret guard, network policy and audit APIs.
type SecurityHandler struct {
	Base
}

// NewSecurityHandler creates a security handler.
func NewSecurityHandler(deps Dependencies) *SecurityHandler {
	return &SecurityHandler{Base{Deps: deps}}
}

// ListPolicyProfiles handles GET /v1/security/policy-profiles.
// @Tags Security
// @Summary List policy profiles
// @Produce application/json
// @Success 200 {object} PolicyProfileListResponse
// @Router /v1/security/policy-profiles [get]
func (h *SecurityHandler) ListPolicyProfiles(w http.ResponseWriter, r *http.Request) {
	profiles := make([]config.PolicyProfile, 0, len(h.Deps.Config.Policy.Profiles))
	for name, profile := range config.NormalizePolicy(h.Deps.Config.Policy).Profiles {
		if profile.Name == "" {
			profile.Name = name
		}
		profiles = append(profiles, profile)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	writeJSON(w, http.StatusOK, PolicyProfileListResponse{
		Active: h.Deps.Config.Policy.ActiveProfile,
		Items:  profiles,
	})
}

// GetPolicyProfile handles GET /v1/security/policy-profiles/{name}.
// @Tags Security
// @Summary Get policy profile
// @Produce application/json
// @Param name path string true "Policy profile name"
// @Success 200 {object} config.PolicyProfile
// @Failure 404 {object} LegacyErrorResponse
// @Router /v1/security/policy-profiles/{name} [get]
func (h *SecurityHandler) GetPolicyProfile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	policy := config.NormalizePolicy(h.Deps.Config.Policy)
	profile, ok := policy.Profiles[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "policy profile not found"})
		return
	}
	if profile.Name == "" {
		profile.Name = name
	}
	writeJSON(w, http.StatusOK, profile)
}

// ValidatePolicyProfile handles POST /v1/security/policy-profiles/validate.
// @Tags Security
// @Summary Validate a policy profile
// @Accept application/json
// @Produce application/json
// @Param body body config.PolicyProfile true "Policy profile payload"
// @Success 200 {object} PolicyProfileValidateResponse
// @Failure 400 {object} PolicyProfileValidateResponse
// @Router /v1/security/policy-profiles/validate [post]
func (h *SecurityHandler) ValidatePolicyProfile(w http.ResponseWriter, r *http.Request) {
	var profile config.PolicyProfile
	if err := decodeJSON(r, &profile); err != nil {
		writeJSON(w, http.StatusBadRequest, PolicyProfileValidateResponse{Valid: false, Error: err.Error()})
		return
	}
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		name = "candidate"
	}
	policy := config.PolicyConfig{
		MinConfidenceForAutoExecute: h.Deps.Config.Policy.MinConfidenceForAutoExecute,
		ActiveProfile:               name,
		Profiles:                    map[string]config.PolicyProfile{name: profile},
		Network:                     h.Deps.Config.Policy.Network,
	}
	if err := config.ValidatePolicyConfig(policy); err != nil {
		writeJSON(w, http.StatusBadRequest, PolicyProfileValidateResponse{Valid: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, PolicyProfileValidateResponse{Valid: true, Profile: &profile})
}

// SecretScan handles POST /v1/security/secret-scan.
// @Tags Security
// @Summary Scan text or payload for secrets
// @Accept application/json
// @Produce application/json
// @Param body body SecretScanRequest true "Secret scan payload"
// @Success 200 {object} security.SecretScanResult
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/security/secret-scan [post]
func (h *SecurityHandler) SecretScan(w http.ResponseWriter, r *http.Request) {
	var body SecretScanRequest
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if len(body.Payload) > 0 {
		writeJSON(w, http.StatusOK, security.ScanMap(body.Payload))
		return
	}
	writeJSON(w, http.StatusOK, security.ScanText(body.Text))
}

// NetworkPolicy handles GET /v1/security/network-policy.
// @Tags Security
// @Summary Get network policy
// @Produce application/json
// @Success 200 {object} config.NetworkPolicy
// @Router /v1/security/network-policy [get]
func (h *SecurityHandler) NetworkPolicy(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.Deps.Config.Policy.Network)
}

// ValidateURL handles POST /v1/security/network-policy/validate-url.
// @Tags Security
// @Summary Validate URL against network policy
// @Accept application/json
// @Produce application/json
// @Param body body NetworkValidateURLRequest true "URL validation payload"
// @Success 200 {object} security.NetworkPolicyDecision
// @Failure 400 {object} LegacyErrorResponse
// @Router /v1/security/network-policy/validate-url [post]
func (h *SecurityHandler) ValidateURL(w http.ResponseWriter, r *http.Request) {
	var body NetworkValidateURLRequest
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	decision := security.ValidateNetworkURL(h.Deps.Config.Policy.Network, body.URL, body.Method, body.MaxDownloadBytes)
	writeJSON(w, http.StatusOK, decision)
}

// Audit handles GET /v1/security/audit.
// @Tags Security
// @Summary Read security audit report
// @Produce application/json
// @Param type query string false "Event type filter"
// @Param limit query int false "Max events"
// @Success 200 {object} SecurityAuditReport
// @Failure 500 {object} LegacyErrorResponse
// @Router /v1/security/audit [get]
func (h *SecurityHandler) Audit(w http.ResponseWriter, r *http.Request) {
	report, err := h.auditReport(r, "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// AuditRun handles GET /v1/security/audit/runs/{run_id}.
// @Tags Security
// @Summary Read security audit report for a run
// @Produce application/json
// @Param run_id path string true "Run ID"
// @Param type query string false "Event type filter"
// @Param limit query int false "Max events"
// @Success 200 {object} SecurityAuditReport
// @Failure 500 {object} LegacyErrorResponse
// @Router /v1/security/audit/runs/{run_id} [get]
func (h *SecurityHandler) AuditRun(w http.ResponseWriter, r *http.Request) {
	report, err := h.auditReport(r, chi.URLParam(r, "run_id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *SecurityHandler) auditReport(r *http.Request, runID string) (SecurityAuditReport, error) {
	typeFilter := r.URL.Query().Get("type")
	limit := parseSecurityLimit(r.URL.Query().Get("limit"))
	items, err := readAuditEvents(h.Deps.Config.Events.AuditRoot, runID, typeFilter, limit)
	if err != nil {
		return SecurityAuditReport{}, err
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Type]++
	}
	return SecurityAuditReport{
		Total:  len(items),
		Counts: counts,
		Items:  items,
	}, nil
}

func readAuditEvents(root, runID, typeFilter string, limit int) ([]core.Event, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil
	}
	items := []core.Event{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
		for scanner.Scan() {
			var event core.Event
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				continue
			}
			if runID != "" && event.RunID != runID {
				continue
			}
			if typeFilter != "" && event.Type != typeFilter {
				continue
			}
			event.Payload = security.RedactMap(event.Payload)
			event.Content = security.RedactString(event.Content)
			items = append(items, event)
		}
		return scanner.Err()
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func parseSecurityLimit(raw string) int {
	if raw == "" {
		return 100
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 100
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}
