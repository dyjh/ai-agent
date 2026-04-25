package semantic

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParsePlan parses a JSON SemanticPlan, accepting a response that wraps the
// JSON object with short prose.
func ParsePlan(value string) (SemanticPlan, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return SemanticPlan{}, fmt.Errorf("semantic plan response is empty")
	}
	var plan SemanticPlan
	if err := json.Unmarshal([]byte(value), &plan); err == nil {
		return plan, nil
	}
	start := strings.Index(value, "{")
	end := strings.LastIndex(value, "}")
	if start < 0 || end <= start {
		return SemanticPlan{}, fmt.Errorf("semantic plan response is not JSON")
	}
	if err := json.Unmarshal([]byte(value[start:end+1]), &plan); err != nil {
		return SemanticPlan{}, err
	}
	return plan, nil
}
