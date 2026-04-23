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

	payload := map[string]any{
		"status":    "ok",
		"service":   "local-agent",
		"timestamp": time.Now().UTC(),
		"server": map[string]any{
			"host": h.Deps.Config.Server.Host,
			"port": h.Deps.Config.Server.Port,
		},
	}
	if h.Deps.Knowledge != nil {
		payload["vector"] = h.Deps.Knowledge.Health(r.Context())
	}

	_ = json.NewEncoder(w).Encode(payload)
}
