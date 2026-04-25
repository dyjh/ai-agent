package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"local-agent/internal/core"
)

// SSETransport connects to the legacy MCP SSE endpoint and sends JSON-RPC to the message endpoint.
type SSETransport struct {
	sseURL     string
	messageURL string
	headers    map[string]string
	client     *http.Client
	profile    core.MCPCompatibilityProfile

	mu          sync.RWMutex
	cancel      context.CancelFunc
	endpoint    string
	pending     map[string]chan sseResponse
	connected   bool
	lastReadErr error
}

type sseResponse struct {
	data []byte
	err  error
}

type sseEvent struct {
	name string
	data string
}

// NewSSETransport creates an MCP transport for SSE + message endpoints.
func NewSSETransport(cfg TransportConfig) *SSETransport {
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
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = time.Duration(profile.TimeoutSeconds) * time.Second
	return &SSETransport{
		sseURL:     strings.TrimSpace(cfg.URL),
		messageURL: strings.TrimSpace(cfg.MessageURL),
		headers:    copyStringMap(cfg.Headers),
		client: &http.Client{
			Transport: transport,
			Timeout:   0,
		},
		profile: profile,
		pending: map[string]chan sseResponse{},
	}
}

// Start opens the SSE stream and performs the MCP initialize handshake.
func (t *SSETransport) Start(ctx context.Context) error {
	if t.sseURL == "" {
		return fmt.Errorf("mcp sse url is required")
	}
	streamCtx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, t.sseURL, nil)
	if err != nil {
		cancel()
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	t.applyHeaders(req)

	resp, err := t.client.Do(req)
	if err != nil {
		cancel()
		return fmt.Errorf("mcp sse connect failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		cancel()
		data, _ := readLimited(resp.Body, t.profile.MaxPayloadBytes)
		return fmt.Errorf("mcp sse status %d: %s", resp.StatusCode, truncateForError(data))
	}

	t.mu.Lock()
	t.cancel = cancel
	t.connected = true
	t.lastReadErr = nil
	t.mu.Unlock()
	go t.readLoop(resp.Body)

	if t.messageURL == "" {
		if err := t.waitForEndpoint(ctx, true, time.Duration(t.profile.TimeoutSeconds)*time.Second); err != nil {
			_ = t.Close(context.Background())
			return err
		}
	} else {
		_ = t.waitForEndpoint(ctx, false, time.Second)
	}
	if err := t.initialize(ctx); err != nil {
		_ = t.Close(context.Background())
		return err
	}
	return nil
}

// Health checks whether the SSE stream was opened and has not reported a read failure.
func (t *SSETransport) Health(_ context.Context) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.connected {
		return fmt.Errorf("mcp sse transport is not connected")
	}
	if t.lastReadErr != nil {
		return t.lastReadErr
	}
	return nil
}

// ListTools lists tools via the MCP tools/list JSON-RPC method.
func (t *SSETransport) ListTools(ctx context.Context) ([]MCPToolSchema, error) {
	payload, err := t.request(ctx, newRPCRequest("tools/list", map[string]any{}))
	if err != nil {
		return nil, err
	}
	return parseToolsResult(payload, t.profile)
}

// CallTool invokes a tool via the MCP tools/call JSON-RPC method.
func (t *SSETransport) CallTool(ctx context.Context, name string, input map[string]any) (*MCPToolResult, error) {
	payload, err := t.request(ctx, newRPCRequest("tools/call", map[string]any{
		"name":      name,
		"arguments": cloneMap(input),
	}))
	if err != nil {
		return nil, err
	}
	return parseToolResult(payload, t.profile)
}

// Close terminates the SSE stream and unblocks pending calls.
func (t *SSETransport) Close(_ context.Context) error {
	t.mu.Lock()
	if t.cancel != nil {
		t.cancel()
	}
	t.connected = false
	for id, ch := range t.pending {
		delete(t.pending, id)
		ch <- sseResponse{err: newMCPError(MCPErrorTransportFailure, "mcp sse transport closed", nil)}
	}
	t.mu.Unlock()
	return nil
}

func (t *SSETransport) initialize(ctx context.Context) error {
	_, err := t.request(ctx, newInitializeRequest())
	if err != nil {
		var mcpErr *MCPError
		if errors.As(err, &mcpErr) && mcpErr.Code == MCPErrorServerError {
			return nil
		}
		return fmt.Errorf("initialize mcp sse server: %w", err)
	}
	return t.notify(ctx, newInitializedNotification())
}

func (t *SSETransport) request(ctx context.Context, payload rpcRequest) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	ch := make(chan sseResponse, 1)
	t.mu.Lock()
	t.pending[payload.ID] = ch
	t.mu.Unlock()
	defer t.forget(payload.ID)

	response, err := t.post(ctx, raw)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(response)) > 0 && json.Valid(response) {
		return decodeRPCResponse(response, payload.ID, t.profile)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(t.profile.TimeoutSeconds)*time.Second)
	defer cancel()
	select {
	case <-ctx.Done():
		return nil, newMCPError(MCPErrorTimeout, "mcp sse request timed out", ctx.Err())
	case item := <-ch:
		if item.err != nil {
			return nil, item.err
		}
		return decodeRPCResponse(item.data, payload.ID, t.profile)
	}
}

func (t *SSETransport) notify(ctx context.Context, notification rpcNotification) error {
	raw, err := json.Marshal(notification)
	if err != nil {
		return err
	}
	_, err = t.post(ctx, raw)
	return err
}

func (t *SSETransport) post(ctx context.Context, raw []byte) ([]byte, error) {
	endpoint, err := t.resolvedMessageURL()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(t.profile.TimeoutSeconds)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	t.applyHeaders(req)

	client := &http.Client{Timeout: time.Duration(t.profile.TimeoutSeconds) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, newMCPError(MCPErrorTimeout, "mcp sse message request timed out", ctx.Err())
		}
		return nil, newMCPError(MCPErrorTransportFailure, "mcp sse message request failed", err)
	}
	defer resp.Body.Close()
	body, err := readLimited(resp.Body, t.profile.MaxPayloadBytes)
	if err != nil {
		return nil, newMCPError(MCPErrorInvalidResponse, "read mcp sse message response", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newMCPError(MCPErrorTransportFailure, fmt.Sprintf("mcp sse message status %d: %s", resp.StatusCode, truncateForError(body)), nil)
	}
	return body, nil
}

func (t *SSETransport) readLoop(body io.ReadCloser) {
	defer body.Close()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), int(t.profile.MaxPayloadBytes))
	event := sseEvent{name: "message"}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			t.dispatch(event)
			event = sseEvent{name: "message"}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch key {
		case "event":
			event.name = value
		case "data":
			if event.data != "" {
				event.data += "\n"
			}
			event.data += value
		}
	}
	if event.data != "" {
		t.dispatch(event)
	}
	err := scanner.Err()
	t.mu.Lock()
	t.connected = false
	if err != nil {
		t.lastReadErr = newMCPError(MCPErrorTransportFailure, "read mcp sse stream", err)
	}
	for id, ch := range t.pending {
		delete(t.pending, id)
		ch <- sseResponse{err: newMCPError(MCPErrorTransportFailure, "mcp sse stream closed", err)}
	}
	t.mu.Unlock()
}

func (t *SSETransport) dispatch(event sseEvent) {
	data := strings.TrimSpace(event.data)
	if data == "" {
		return
	}
	if event.name == "endpoint" {
		t.setEndpoint(data)
		return
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &probe); err != nil {
		return
	}
	id := rawIDString(probe["id"])
	if id == "" {
		return
	}
	t.mu.RLock()
	ch := t.pending[id]
	t.mu.RUnlock()
	if ch == nil {
		return
	}
	select {
	case ch <- sseResponse{data: []byte(data)}:
	default:
	}
}

func (t *SSETransport) setEndpoint(value string) {
	endpoint := strings.TrimSpace(value)
	if endpoint == "" {
		return
	}
	if parsed, err := url.Parse(endpoint); err == nil && !parsed.IsAbs() {
		base, baseErr := url.Parse(t.sseURL)
		if baseErr == nil {
			endpoint = base.ResolveReference(parsed).String()
		}
	}
	t.mu.Lock()
	t.endpoint = endpoint
	t.mu.Unlock()
}

func (t *SSETransport) waitForEndpoint(ctx context.Context, required bool, maxWait time.Duration) error {
	if maxWait <= 0 {
		maxWait = time.Duration(t.profile.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		t.mu.RLock()
		endpoint := t.endpoint
		connected := t.connected
		readErr := t.lastReadErr
		t.mu.RUnlock()
		if endpoint != "" {
			return nil
		}
		if readErr != nil {
			return readErr
		}
		if !connected {
			if required {
				return fmt.Errorf("mcp sse stream closed before endpoint event")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			if required {
				return newMCPError(MCPErrorTimeout, "mcp sse endpoint event timed out", ctx.Err())
			}
			return nil
		case <-ticker.C:
		}
	}
}

func (t *SSETransport) resolvedMessageURL() (string, error) {
	t.mu.RLock()
	endpoint := t.endpoint
	t.mu.RUnlock()
	if endpoint != "" {
		return endpoint, nil
	}
	if t.messageURL != "" {
		return t.messageURL, nil
	}
	return "", fmt.Errorf("mcp sse message endpoint is not available")
}

func (t *SSETransport) forget(id string) {
	t.mu.Lock()
	delete(t.pending, id)
	t.mu.Unlock()
}

func (t *SSETransport) applyHeaders(req *http.Request) {
	for key, value := range t.headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
}

func rawIDString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil {
		return number.String()
	}
	return ""
}
