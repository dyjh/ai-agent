package handlers

import (
	"encoding/json"
	"net/http"

	"local-agent/internal/security"
)

// Base wraps shared dependencies and JSON helpers.
type Base struct {
	Deps Dependencies
}

// ErrorResponse is the stable API error envelope.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(security.RedactAny(payload))
}

func writeError(w http.ResponseWriter, status int, code, message string, details any) {
	writeJSON(w, status, ErrorResponse{
		Code:    code,
		Message: message,
		Details: details,
	})
}
