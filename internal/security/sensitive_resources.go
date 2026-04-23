package security

import "strings"

// IsSensitivePath reports whether a path should default to approval.
func IsSensitivePath(path string, extra []string) bool {
	normalized := strings.ToLower(path)
	defaults := []string{
		".env",
		".env.local",
		".npmrc",
		".pypirc",
		"id_rsa",
		"id_ed25519",
		"credentials",
		"cookie",
		"session",
		"token",
		"secret",
		"private",
		"aws",
		"gcloud",
		"kubeconfig",
	}
	for _, item := range defaults {
		if strings.Contains(normalized, strings.ToLower(item)) {
			return true
		}
	}
	for _, item := range extra {
		if strings.Contains(normalized, strings.ToLower(item)) {
			return true
		}
	}
	return false
}
