package tools

import "fmt"

// GetString extracts a string from an input snapshot.
func GetString(input map[string]any, key string) (string, error) {
	raw, ok := input[key]
	if !ok {
		return "", fmt.Errorf("missing key: %s", key)
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("key %s is not a string", key)
	}
	return value, nil
}

// GetInt extracts an int from an input snapshot.
func GetInt(input map[string]any, key string, fallback int) int {
	raw, ok := input[key]
	if !ok {
		return fallback
	}
	switch value := raw.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return fallback
	}
}

// GetMap extracts an object-like value from an input snapshot.
func GetMap(input map[string]any, key string) map[string]any {
	raw, ok := input[key]
	if !ok {
		return nil
	}
	value, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return value
}
