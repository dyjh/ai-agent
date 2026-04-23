package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// CloneMap performs a deep copy for map[string]any values.
func CloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}

	raw, err := json.Marshal(input)
	if err != nil {
		cp := make(map[string]any, len(input))
		for k, v := range input {
			cp[k] = v
		}
		return cp
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		cp := make(map[string]any, len(input))
		for k, v := range input {
			cp[k] = v
		}
		return cp
	}

	return out
}

// MapsEqual compares two input snapshots through canonical JSON.
func MapsEqual(a, b map[string]any) bool {
	aj, errA := json.Marshal(a)
	bj, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(aj) == string(bj)
}

// HashMap returns a stable SHA-256 hash for an input snapshot.
func HashMap(input map[string]any) (string, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
