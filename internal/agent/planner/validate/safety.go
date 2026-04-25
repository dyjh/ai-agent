package validate

import (
	"fmt"
	"path/filepath"
	"strings"

	"local-agent/internal/agent/planner/semantic"
	"local-agent/internal/security"
)

func (v Validator) validateSafety(step semantic.SemanticPlanStep) stepValidation {
	result := stepValidation{}
	if scan := security.ScanMap(step.Input); scan.HasSecret {
		result.Errors = append(result.Errors, "tool input contains secret-like data")
		return result
	}
	for _, key := range []string{"path", "workspace", "manifest_path"} {
		raw, _ := step.Input[key].(string)
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if pathEscapes(raw) {
			result.Errors = append(result.Errors, fmt.Sprintf("%s escapes workspace: %s", key, raw))
		}
		if v.isSensitivePath(raw) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s touches sensitive path", key))
		}
	}
	return result
}

func pathEscapes(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../")
}

func (v Validator) isSensitivePath(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	for _, sensitive := range v.Options.SensitivePaths {
		sensitive = strings.ToLower(filepath.ToSlash(strings.TrimSpace(sensitive)))
		if sensitive == "" {
			continue
		}
		if lower == sensitive || strings.Contains(lower, "/"+sensitive) || strings.HasSuffix(lower, sensitive) {
			return true
		}
	}
	return false
}
