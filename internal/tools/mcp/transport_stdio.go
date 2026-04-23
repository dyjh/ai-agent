package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

// StdioTransport starts a local MCP server process and exchanges line-delimited JSON-RPC.
type StdioTransport struct {
	command string
	args    []string
	cwd     string
	env     map[string]string

	stateMu sync.RWMutex
	rpcMu   sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
}

// NewStdioTransport creates a stdio MCP transport.
func NewStdioTransport(cfg TransportConfig) *StdioTransport {
	return &StdioTransport{
		command: strings.TrimSpace(cfg.Command),
		args:    append([]string(nil), cfg.Args...),
		cwd:     strings.TrimSpace(cfg.Cwd),
		env:     copyStringMap(cfg.Env),
	}
}

// Start launches the configured MCP child process.
func (t *StdioTransport) Start(ctx context.Context) error {
	t.stateMu.Lock()
	defer t.stateMu.Unlock()
	if t.cmd != nil {
		return nil
	}
	if t.command == "" {
		return fmt.Errorf("mcp stdio command is required")
	}

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

	t.cmd = cmd
	t.stdin = stdin
	t.stdout = bufio.NewReader(stdoutPipe)
	return nil
}

// ListTools lists tools via the MCP tools/list JSON-RPC method.
func (t *StdioTransport) ListTools(ctx context.Context) ([]MCPToolSchema, error) {
	payload, err := t.request(ctx, newRPCRequest("tools/list", map[string]any{}))
	if err != nil {
		return nil, err
	}
	return parseToolsResult(payload)
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
	return parseToolResult(payload)
}

// Close terminates the child process and closes pipes.
func (t *StdioTransport) Close(_ context.Context) error {
	t.stateMu.Lock()
	defer t.stateMu.Unlock()
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

func (t *StdioTransport) request(ctx context.Context, request rpcRequest) ([]byte, error) {
	t.rpcMu.Lock()
	defer t.rpcMu.Unlock()

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
		return nil, ctx.Err()
	case out := <-done:
		if out.err != nil {
			return nil, fmt.Errorf("read mcp stdio response: %w", out.err)
		}
		return decodeRPCResponse(out.line)
	}
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
