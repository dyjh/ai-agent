package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

// HealthHandler serves readiness metadata.
type HealthHandler struct {
	Base
}

// NewHealthHandler creates a new health handler.
func NewHealthHandler(deps Dependencies) *HealthHandler {
	return &HealthHandler{Base{Deps: deps}}
}

// Get serves the health payload.
func (h *HealthHandler) Get(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	kbStatus := map[string]any{
		"enabled":  false,
		"provider": h.Deps.Config.KB.Provider,
		"status":   "disabled",
	}
	var vector any
	qdrantStatus := map[string]any{
		"configured": h.Deps.Config.Qdrant.URL != "",
		"status":     "not_checked",
	}
	if h.Deps.Config.KB.Enabled && h.Deps.Knowledge != nil {
		health := h.Deps.Knowledge.Health(r.Context())
		vector = health
		status := "ok"
		if health.Error != "" {
			status = "error"
		}
		kbStatus = map[string]any{
			"enabled":  true,
			"provider": h.Deps.Config.KB.Provider,
			"status":   status,
		}
		if health.Error != "" {
			kbStatus["error"] = health.Error
		}
		qdrantStatus["status"] = health.Qdrant
		if health.Error != "" {
			qdrantStatus["error"] = health.Error
		}
	}
	if vector == nil {
		vector = map[string]any{"vector_backend": h.Deps.Config.Vector.Backend}
	}

	payload := map[string]any{
		"status":    "ok",
		"service":   "local-agent",
		"version":   "dev",
		"timestamp": time.Now().UTC(),
		"server": map[string]any{
			"host": h.Deps.Config.Server.Host,
			"port": h.Deps.Config.Server.Port,
		},
		"database": map[string]any{
			"status": configuredStatus(h.Deps.Config.Database.URL != "", "configured", "memory_fallback"),
		},
		"qdrant":         qdrantStatus,
		"knowledge_base": kbStatus,
		"workflow": map[string]any{
			"status": configuredStatus(h.Deps.Runtime != nil, "configured", "unavailable"),
		},
		"docs": map[string]any{
			"swagger": "/swagger/index.html",
			"openapi": "/swagger/doc.json",
		},
		"vector": vector,
	}

	_ = json.NewEncoder(w).Encode(payload)
}

func configuredStatus(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}
