package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"local-agent/internal/core"
)

// StdioTransport starts a local MCP server process and exchanges line-delimited JSON-RPC.
type StdioTransport struct {
	command string
	args    []string
	cwd     string
	env     map[string]string
	profile core.MCPCompatibilityProfile

	stateMu sync.RWMutex
	rpcMu   sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
}

// NewStdioTransport creates a stdio MCP transport.
func NewStdioTransport(cfg TransportConfig) *StdioTransport {
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
	return &StdioTransport{
		command: strings.TrimSpace(cfg.Command),
		args:    append([]string(nil), cfg.Args...),
		cwd:     strings.TrimSpace(cfg.Cwd),
		env:     copyStringMap(cfg.Env),
		profile: profile,
	}
}

// Start launches the configured MCP child process.
func (t *StdioTransport) Start(ctx context.Context) error {
	t.stateMu.Lock()
	if t.cmd != nil {
		t.stateMu.Unlock()
		return nil
	}
	if t.command == "" {
		t.stateMu.Unlock()
		return fmt.Errorf("mcp stdio command is required")
	}
	t.stateMu.Unlock()

	cmd := exec.CommandContext(ctx, t.command, t.args...)
	if t.cwd != "" {
		cmd.Dir = t.cwd
	}
	cmd.Env = append(os.Environ(), envPairs(t.env)...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open mcp stdio stdin: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open mcp stdio stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("open mcp stdio stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start mcp stdio server: %w", err)
	}
	go io.Copy(io.Discard, stderr)

	t.stateMu.Lock()
	t.cmd = cmd
	t.stdin = stdin
	t.stdout = bufio.NewReader(stdoutPipe)
	t.stateMu.Unlock()
	if err := t.initialize(ctx); err != nil {
		t.stateMu.Lock()
		_ = t.closeLocked()
		t.stateMu.Unlock()
		return err
	}
	return nil
}

// ListTools lists tools via the MCP tools/list JSON-RPC method.
func (t *StdioTransport) ListTools(ctx context.Context) ([]MCPToolSchema, error) {
	payload, err := t.request(ctx, newRPCRequest("tools/list", map[string]any{}))
	if err != nil {
		return nil, err
	}
	return parseToolsResult(payload, t.profile)
}

// CallTool invokes a tool via the MCP tools/call JSON-RPC method.
func (t *StdioTransport) CallTool(ctx context.Context, name string, input map[string]any) (*MCPToolResult, error) {
	payload, err := t.request(ctx, newRPCRequest("tools/call", map[string]any{
		"name":      name,
		"arguments": cloneMap(input),
	}))
	if err != nil {
		return nil, err
	}
	return parseToolResult(payload, t.profile)
}

// Close terminates the child process and closes pipes.
func (t *StdioTransport) Close(_ context.Context) error {
	t.stateMu.Lock()
	defer t.stateMu.Unlock()
	return t.closeLocked()
}

func (t *StdioTransport) closeLocked() error {
	if t.stdin != nil {
		_ = t.stdin.Close()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
	}
	t.cmd = nil
	t.stdin = nil
	t.stdout = nil
	return nil
}

func (t *StdioTransport) initialize(ctx context.Context) error {
	_, err := t.request(ctx, newInitializeRequest())
	if err != nil {
		var mcpErr *MCPError
		if errors.As(err, &mcpErr) && mcpErr.Code == MCPErrorServerError {
			return nil
		}
		return fmt.Errorf("initialize mcp stdio server: %w", err)
	}
	return t.notify(ctx, newInitializedNotification())
}

func (t *StdioTransport) notify(ctx context.Context, notification rpcNotification) error {
	t.rpcMu.Lock()
	defer t.rpcMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(t.profile.TimeoutSeconds)*time.Second)
	defer cancel()

	t.stateMu.RLock()
	stdin := t.stdin
	cmd := t.cmd
	t.stateMu.RUnlock()
	if stdin == nil || cmd == nil {
		return fmt.Errorf("mcp stdio transport is not started")
	}

	raw, err := json.Marshal(notification)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		_, err := stdin.Write(append(raw, '\n'))
		done <- err
	}()
	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return newMCPError(MCPErrorTimeout, "mcp stdio notification timed out", ctx.Err())
	case err := <-done:
		if err != nil {
			return fmt.Errorf("write mcp stdio notification: %w", err)
		}
		return nil
	}
}

func (t *StdioTransport) request(ctx context.Context, request rpcRequest) ([]byte, error) {
	t.rpcMu.Lock()
	defer t.rpcMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(t.profile.TimeoutSeconds)*time.Second)
	defer cancel()

	t.stateMu.RLock()
	stdin := t.stdin
	stdout := t.stdout
	cmd := t.cmd
	t.stateMu.RUnlock()
	if stdin == nil || stdout == nil || cmd == nil {
		return nil, fmt.Errorf("mcp stdio transport is not started")
	}

	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if _, err := stdin.Write(append(raw, '\n')); err != nil {
		return nil, fmt.Errorf("write mcp stdio request: %w", err)
	}

	for {
		type response struct {
			line []byte
			err  error
		}
		done := make(chan response, 1)
		go func() {
			line, err := stdout.ReadBytes('\n')
			done <- response{line: bytes.TrimSpace(line), err: err}
		}()

		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return nil, newMCPError(MCPErrorTimeout, "mcp stdio request timed out", ctx.Err())
		case out := <-done:
			if out.err != nil {
				return nil, newMCPError(MCPErrorTransportFailure, "read mcp stdio response", out.err)
			}
			if int64(len(out.line)) > t.profile.MaxPayloadBytes {
				return nil, newMCPError(MCPErrorInvalidResponse, fmt.Sprintf("mcp stdio response exceeds %d bytes", t.profile.MaxPayloadBytes), nil)
			}
			if len(out.line) == 0 {
				continue
			}
			if !stdioFrameMatchesRequest(out.line, request.ID, t.profile) {
				continue
			}
			return decodeRPCResponse(out.line, request.ID, t.profile)
		}
	}
}

func stdioFrameMatchesRequest(line []byte, expectedID string, profile core.MCPCompatibilityProfile) bool {
	frame, err := selectResponseFrame(line, profile)
	if err != nil {
		return true
	}
	var response rpcResponse
	if err := json.Unmarshal(frame, &response); err != nil {
		return true
	}
	if response.ID == "" && len(response.Result) == 0 && response.Error == nil {
		return false
	}
	if profile.StrictIDMatching && expectedID != "" && response.ID != "" && response.ID != expectedID {
		return false
	}
	return true
}

func envPairs(input map[string]string) []string {
	if len(input) == 0 {
		return nil
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]string, 0, len(keys))
	for _, key := range keys {
		items = append(items, key+"="+input[key])
	}
	return items
}
