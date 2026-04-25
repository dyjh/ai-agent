package security

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"local-agent/internal/config"
)

// NetworkPolicyDecision explains URL validation under the configured policy.
type NetworkPolicyDecision struct {
	Allowed          bool   `json:"allowed"`
	RequiresApproval bool   `json:"requires_approval"`
	Reason           string `json:"reason"`
	NormalizedURL    string `json:"normalized_url,omitempty"`
	Method           string `json:"method"`
	Host             string `json:"host,omitempty"`
	RiskLevel        string `json:"risk_level"`
}

// ValidateNetworkURL checks one outbound URL and method against a network policy.
func ValidateNetworkURL(policy config.NetworkPolicy, rawURL, method string, maxDownloadBytes int64) NetworkPolicyDecision {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return networkDenied(method, "", "valid absolute URL is required")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return networkDenied(method, parsed.Hostname(), "only http and https URLs are allowed")
	}
	host := strings.ToLower(parsed.Hostname())
	if policy.DenyMetadataIP && isMetadataHost(host) {
		return networkDenied(method, host, "metadata endpoints are denied")
	}
	if policy.DenyPrivateIP && isPrivateHost(host) {
		return networkDenied(method, host, "private and loopback IP ranges are denied")
	}
	if domainDenied(host, policy.DeniedDomains) {
		return networkDenied(method, host, "domain is denied by network policy")
	}
	if len(policy.AllowedDomains) > 0 && !domainAllowed(host, policy.AllowedDomains) {
		return networkDenied(method, host, "domain is not in the allowlist")
	}
	if policy.MaxDownloadBytes > 0 && maxDownloadBytes > policy.MaxDownloadBytes {
		return networkDenied(method, host, fmt.Sprintf("requested download limit exceeds %d bytes", policy.MaxDownloadBytes))
	}
	decision := NetworkPolicyDecision{
		Allowed:       true,
		Method:        method,
		Host:          host,
		NormalizedURL: parsed.String(),
		RiskLevel:     "read",
		Reason:        "network URL is allowed",
	}
	if methodRequiresApproval(method, policy.RequireApprovalForMethods) {
		decision.RequiresApproval = true
		decision.RiskLevel = "write"
		decision.Reason = "network write method requires approval"
	}
	return decision
}

func networkDenied(method, host, reason string) NetworkPolicyDecision {
	return NetworkPolicyDecision{
		Allowed:   false,
		Method:    method,
		Host:      host,
		RiskLevel: "danger",
		Reason:    reason,
	}
}

func methodRequiresApproval(method string, configured []string) bool {
	if len(configured) == 0 {
		configured = []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	}
	for _, item := range configured {
		if strings.EqualFold(strings.TrimSpace(item), method) {
			return true
		}
	}
	return false
}

func isMetadataHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "169.254.169.254" || host == "metadata.google.internal" {
		return true
	}
	return strings.Contains(host, "metadata") && strings.Contains(host, "internal")
}

func isPrivateHost(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	return false
}

func domainAllowed(host string, patterns []string) bool {
	for _, pattern := range patterns {
		if domainPatternMatches(host, pattern) {
			return true
		}
	}
	return false
}

func domainDenied(host string, patterns []string) bool {
	for _, pattern := range patterns {
		if domainPatternMatches(host, pattern) {
			return true
		}
	}
	return false
}

func domainPatternMatches(host, pattern string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if host == "" || pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(host, suffix)
	}
	return host == pattern || strings.HasSuffix(host, "."+pattern)
}
