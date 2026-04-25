package security

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// SecretFinding describes one detected secret-like value.
type SecretFinding struct {
	Type        string  `json:"type"`
	Redacted    string  `json:"redacted"`
	Location    string  `json:"location,omitempty"`
	Confidence  float64 `json:"confidence"`
	Severity    string  `json:"severity"`
	Description string  `json:"description,omitempty"`
}

// SecretScanResult is returned by text or map scans.
type SecretScanResult struct {
	HasSecret    bool            `json:"has_secret"`
	Findings     []SecretFinding `json:"findings"`
	RedactedText string          `json:"redacted_text,omitempty"`
}

// SecretGuard detects and redacts high-risk secret-like values.
type SecretGuard struct {
	patterns  []secretPattern
	allowlist []*regexp.Regexp
}

type secretPattern struct {
	typ         string
	re          *regexp.Regexp
	valueGroup  int
	confidence  float64
	severity    string
	description string
}

var defaultSecretPatterns = []secretPattern{
	{typ: "private_key", re: regexp.MustCompile(`(?is)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`), valueGroup: 0, confidence: 0.99, severity: "critical", description: "private key block"},
	{typ: "ssh_private_key", re: regexp.MustCompile(`(?is)-----BEGIN OPENSSH PRIVATE KEY-----.*?-----END OPENSSH PRIVATE KEY-----`), valueGroup: 0, confidence: 0.99, severity: "critical", description: "OpenSSH private key block"},
	{typ: "openai_key", re: regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{16,}\b`), valueGroup: 0, confidence: 0.95, severity: "high", description: "OpenAI-style API key"},
	{typ: "github_token", re: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`), valueGroup: 0, confidence: 0.95, severity: "high", description: "GitHub token"},
	{typ: "aws_access_key", re: regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`), valueGroup: 0, confidence: 0.95, severity: "high", description: "AWS access key id"},
	{typ: "aws_secret_key", re: regexp.MustCompile(`(?i)(aws[_-]?secret[_-]?access[_-]?key\s*[:=]\s*["']?)([A-Za-z0-9/+=]{32,})(["']?)`), valueGroup: 2, confidence: 0.95, severity: "high", description: "AWS secret access key assignment"},
	{typ: "bearer_token", re: regexp.MustCompile(`(?i)(bearer\s+)([A-Za-z0-9\-._~+/=]{12,})`), valueGroup: 2, confidence: 0.85, severity: "high", description: "Bearer token"},
	{typ: "api_key", re: regexp.MustCompile(`(?i)(api[_-]?key\s*[:=]\s*["']?)([^\s"',;]+)(["']?)`), valueGroup: 2, confidence: 0.82, severity: "high", description: "API key assignment"},
	{typ: "password_assignment", re: regexp.MustCompile(`(?i)(password|passwd|pwd)(\s*[:=]\s*["']?)([^\s"',;]+)(["']?)`), valueGroup: 3, confidence: 0.82, severity: "high", description: "password assignment"},
	{typ: "secret_assignment", re: regexp.MustCompile(`(?i)(secret)(\s*[:=]\s*["']?)([^\s"',;]+)(["']?)`), valueGroup: 3, confidence: 0.82, severity: "high", description: "secret assignment"},
	{typ: "token_assignment", re: regexp.MustCompile(`(?i)(token)(\s*[:=]\s*["']?)([^\s"',;]+)(["']?)`), valueGroup: 3, confidence: 0.8, severity: "high", description: "token assignment"},
	{typ: "cookie", re: regexp.MustCompile(`(?i)(cookie\s*[:=]\s*)([^\r\n]+)`), valueGroup: 2, confidence: 0.82, severity: "high", description: "cookie header or assignment"},
	{typ: "session", re: regexp.MustCompile(`(?i)(session(?:id|_id)?\s*[:=]\s*["']?)([^\s"',;]+)(["']?)`), valueGroup: 2, confidence: 0.82, severity: "high", description: "session token assignment"},
	{typ: "database_dsn_password", re: regexp.MustCompile(`(?i)((?:postgres(?:ql)?|mysql|mongodb|redis)://[^:\s/@]+:)([^@\s]+)(@)`), valueGroup: 2, confidence: 0.95, severity: "high", description: "database DSN password"},
	{typ: "cloud_credential", re: regexp.MustCompile(`(?i)(google_application_credentials|azure_client_secret|client_secret)\s*[:=]\s*["']?([^\s"',;]+)(["']?)`), valueGroup: 2, confidence: 0.85, severity: "high", description: "cloud credential assignment"},
}

// NewSecretGuard constructs a guard with optional allowlist regular expressions.
func NewSecretGuard(allowlistPatterns []string) (*SecretGuard, error) {
	compiled := make([]*regexp.Regexp, 0, len(allowlistPatterns))
	for _, pattern := range allowlistPatterns {
		if strings.TrimSpace(pattern) == "" {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile secret allowlist pattern %q: %w", pattern, err)
		}
		compiled = append(compiled, re)
	}
	return &SecretGuard{
		patterns:  append([]secretPattern(nil), defaultSecretPatterns...),
		allowlist: compiled,
	}, nil
}

// DefaultSecretGuard returns the built-in guard.
func DefaultSecretGuard() *SecretGuard {
	guard, _ := NewSecretGuard(nil)
	return guard
}

// ScanText detects secrets in free text.
func ScanText(text string) SecretScanResult {
	return DefaultSecretGuard().ScanText(text)
}

// ScanMap detects secrets in structured maps.
func ScanMap(input map[string]any) SecretScanResult {
	return DefaultSecretGuard().ScanMap(input)
}

// RedactText masks secrets in free text.
func RedactText(text string) string {
	return DefaultSecretGuard().RedactText(text)
}

// MustBlockLongTermStorage reports whether a scan result must not be persisted into long-term stores.
func MustBlockLongTermStorage(result SecretScanResult) bool {
	if !result.HasSecret {
		return false
	}
	for _, finding := range result.Findings {
		switch strings.ToLower(finding.Severity) {
		case "critical", "high":
			return true
		}
	}
	return false
}

// ScanText detects secrets in free text.
func (g *SecretGuard) ScanText(text string) SecretScanResult {
	redacted := g.RedactText(text)
	findings := []SecretFinding{}
	for _, pattern := range g.patterns {
		matches := pattern.re.FindAllStringSubmatchIndex(text, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			full := text[match[0]:match[1]]
			if g.allowlisted(full) {
				continue
			}
			localMatch := pattern.re.FindStringSubmatchIndex(full)
			findings = append(findings, SecretFinding{
				Type:        pattern.typ,
				Redacted:    redactMatch(full, pattern, localMatch),
				Location:    fmt.Sprintf("text:%d-%d", match[0], match[1]),
				Confidence:  pattern.confidence,
				Severity:    pattern.severity,
				Description: pattern.description,
			})
		}
	}
	if looksLikeEnvFile(text) {
		findings = append(findings, SecretFinding{
			Type:        "env_file_content",
			Redacted:    "[REDACTED:env_file_content]",
			Location:    "text",
			Confidence:  0.8,
			Severity:    "high",
			Description: "multiple .env-style secret assignments",
		})
	}
	sort.SliceStable(findings, func(i, j int) bool {
		return findings[i].Location < findings[j].Location
	})
	return SecretScanResult{
		HasSecret:    len(findings) > 0,
		Findings:     findings,
		RedactedText: redacted,
	}
}

// ScanMap detects secrets in structured maps.
func (g *SecretGuard) ScanMap(input map[string]any) SecretScanResult {
	if input == nil {
		return SecretScanResult{}
	}
	findings := []SecretFinding{}
	redacted := redactMapWithGuard(input, g, "$", &findings)
	raw, _ := json.Marshal(redacted)
	return SecretScanResult{
		HasSecret:    len(findings) > 0,
		Findings:     findings,
		RedactedText: string(raw),
	}
}

// RedactText masks secrets in free text.
func (g *SecretGuard) RedactText(text string) string {
	if g == nil || text == "" {
		return text
	}
	result := text
	for _, pattern := range g.patterns {
		result = pattern.re.ReplaceAllStringFunc(result, func(match string) string {
			if g.allowlisted(match) {
				return match
			}
			indices := pattern.re.FindStringSubmatchIndex(match)
			return redactMatch(match, pattern, indices)
		})
	}
	return result
}

func redactMapWithGuard(input map[string]any, guard *SecretGuard, path string, findings *[]SecretFinding) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		location := path + "." + key
		if IsSensitiveKey(key) {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				*findings = append(*findings, SecretFinding{
					Type:        secretTypeFromKey(key),
					Redacted:    "[REDACTED]",
					Location:    location,
					Confidence:  0.9,
					Severity:    "high",
					Description: "sensitive field name",
				})
			}
			out[key] = "[REDACTED]"
			continue
		}
		out[key] = redactAnyWithGuard(value, guard, location, findings)
	}
	return out
}

func redactAnyWithGuard(value any, guard *SecretGuard, path string, findings *[]SecretFinding) any {
	switch typed := value.(type) {
	case map[string]any:
		return redactMapWithGuard(typed, guard, path, findings)
	case map[string]string:
		converted := make(map[string]any, len(typed))
		for key, value := range typed {
			converted[key] = value
		}
		return redactMapWithGuard(converted, guard, path, findings)
	case []any:
		out := make([]any, len(typed))
		for idx, item := range typed {
			out[idx] = redactAnyWithGuard(item, guard, fmt.Sprintf("%s[%d]", path, idx), findings)
		}
		return out
	case []string:
		out := make([]any, len(typed))
		for idx, item := range typed {
			out[idx] = redactAnyWithGuard(item, guard, fmt.Sprintf("%s[%d]", path, idx), findings)
		}
		return out
	case string:
		scan := guard.ScanText(typed)
		for _, finding := range scan.Findings {
			finding.Location = path + ":" + finding.Location
			*findings = append(*findings, finding)
		}
		return scan.RedactedText
	default:
		return typed
	}
}

func redactMatch(match string, pattern secretPattern, indices []int) string {
	if pattern.valueGroup <= 0 || len(indices) <= pattern.valueGroup*2+1 {
		return "[REDACTED:" + pattern.typ + "]"
	}
	start := indices[pattern.valueGroup*2]
	end := indices[pattern.valueGroup*2+1]
	if start < 0 || end < start || end > len(match) {
		return "[REDACTED:" + pattern.typ + "]"
	}
	return match[:start] + "[REDACTED]" + match[end:]
}

func (g *SecretGuard) allowlisted(match string) bool {
	for _, re := range g.allowlist {
		if re.MatchString(match) {
			return true
		}
	}
	return false
}

func looksLikeEnvFile(text string) bool {
	secretAssignments := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		key := strings.TrimSpace(strings.SplitN(line, "=", 2)[0])
		if IsSensitiveKey(key) {
			secretAssignments++
		}
	}
	return secretAssignments >= 2
}

// IsSensitiveKey reports whether a field name is secret-bearing.
func IsSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "has_secret", "deny_effects", "expected_effects", "effects", "input_tokens", "output_tokens", "total_tokens", "tool_call_count", "redact_secrets", "secret_patterns", "secret_findings", "forbidden_secret_patterns":
		return false
	}
	return sensitiveKeyPattern.MatchString(normalized)
}

func secretTypeFromKey(key string) string {
	lower := strings.ToLower(key)
	switch {
	case strings.Contains(lower, "cookie"):
		return "cookie"
	case strings.Contains(lower, "session"):
		return "session"
	case strings.Contains(lower, "password"), strings.Contains(lower, "passwd"):
		return "password_assignment"
	case strings.Contains(lower, "token"):
		return "token_assignment"
	case strings.Contains(lower, "secret"):
		return "secret_assignment"
	case strings.Contains(lower, "api"):
		return "api_key"
	default:
		return "cloud_credential"
	}
}
