package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/security"
)

// HTTPTransport calls an MCP endpoint using JSON-RPC over HTTP.
type HTTPTransport struct {
	url     string
	headers map[string]string
	client  *http.Client
	profile core.MCPCompatibilityProfile
}

// NewHTTPTransport creates an HTTP MCP transport.
func NewHTTPTransport(cfg TransportConfig) *HTTPTransport {
	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	profile := cfg.Compatibility
	if profile.Dialect == "" {
		profile = defaultCompatibilityProfile(timeout)
	}
	if profile.TimeoutSeconds <= 0 {
		profile.TimeoutSeconds = timeout
	}
	if profile.MaxPayloadBytes <= 0 {
		profile.MaxPayloadBytes = DefaultMaxPayloadBytes
	}
	return &HTTPTransport{
		url:     strings.TrimSpace(cfg.URL),
		headers: copyStringMap(cfg.Headers),
		client: &http.Client{
			Timeout: time.Duration(profile.TimeoutSeconds) * time.Second,
		},
		profile: profile,
	}
}

// Start validates the transport endpoint. HTTP connections are created per request.
func (t *HTTPTransport) Start(_ context.Context) error {
	if t.url == "" {
		return fmt.Errorf("mcp http url is required")
	}
	return nil
}

// Health checks whether the endpoint is reachable.
func (t *HTTPTransport) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
	if err != nil {
		return err
	}
	t.applyHeaders(req)
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("mcp http health failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := readLimited(resp.Body, t.profile.MaxPayloadBytes)
		return fmt.Errorf("mcp http health status %d: %s", resp.StatusCode, truncateForError(data))
	}
	return nil
}

// ListTools lists tools via the MCP tools/list JSON-RPC method.
func (t *HTTPTransport) ListTools(ctx context.Context) ([]MCPToolSchema, error) {
	payload, err := t.postRPC(ctx, newRPCRequest("tools/list", map[string]any{}))
	if err != nil {
		return nil, err
	}
	return parseToolsResult(payload, t.profile)
}

// CallTool invokes a tool via the MCP tools/call JSON-RPC method.
func (t *HTTPTransport) CallTool(ctx context.Context, name string, input map[string]any) (*MCPToolResult, error) {
	payload, err := t.postRPC(ctx, newRPCRequest("tools/call", map[string]any{
		"name":      name,
		"arguments": cloneMap(input),
	}))
	if err != nil {
		return nil, err
	}
	return parseToolResult(payload, t.profile)
}

// Close is a no-op for the HTTP transport.
func (t *HTTPTransport) Close(_ context.Context) error {
	return nil
}

func (t *HTTPTransport) postRPC(ctx context.Context, payload rpcRequest) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(t.profile.TimeoutSeconds)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	t.applyHeaders(req)

	resp, err := t.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, newMCPError(MCPErrorTimeout, "mcp http request timed out", ctx.Err())
		}
		return nil, newMCPError(MCPErrorTransportFailure, "mcp http request failed", err)
	}
	defer resp.Body.Close()

	body, err := readLimited(resp.Body, t.profile.MaxPayloadBytes)
	if err != nil {
		return nil, newMCPError(MCPErrorInvalidResponse, "read mcp http response", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newMCPError(MCPErrorTransportFailure, fmt.Sprintf("mcp http status %d: %s", resp.StatusCode, truncateForError(body)), nil)
	}
	return decodeRPCResponse(body, payload.ID, t.profile)
}

func (t *HTTPTransport) applyHeaders(req *http.Request) {
	for key, value := range t.headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
}

func readLimited(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxPayloadBytes
	}
	limited := io.LimitReader(reader, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("mcp response exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func truncateForError(data []byte) string {
	text := strings.TrimSpace(string(data))
	if len(text) > 512 {
		text = text[:512] + "...[truncated]"
	}
	return security.RedactString(text)
}
