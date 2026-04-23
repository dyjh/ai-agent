package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"
)

var rpcID uint64

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      string          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type transportHealth interface {
	Health(ctx context.Context) error
}

func nextRPCID() string {
	return strconv.FormatUint(atomic.AddUint64(&rpcID, 1), 10)
}

func newRPCRequest(method string, params any) rpcRequest {
	return rpcRequest{
		JSONRPC: "2.0",
		ID:      nextRPCID(),
		Method:  method,
		Params:  params,
	}
}

func decodeRPCResponse(data []byte) (json.RawMessage, error) {
	var response rpcResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode MCP JSON-RPC response: %w", err)
	}
	if response.Error != nil {
		if response.Error.Message == "" {
			response.Error.Message = "unknown MCP JSON-RPC error"
		}
		return nil, fmt.Errorf("MCP JSON-RPC error %d: %s", response.Error.Code, response.Error.Message)
	}
	if len(response.Result) == 0 {
		return []byte("{}"), nil
	}
	return response.Result, nil
}

func parseToolsResult(data []byte) ([]MCPToolSchema, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var direct []MCPToolSchema
	if err := json.Unmarshal(data, &direct); err == nil && direct != nil {
		return direct, nil
	}
	var wrapped struct {
		Tools []MCPToolSchema `json:"tools"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, fmt.Errorf("decode MCP tools response: %w", err)
	}
	return wrapped.Tools, nil
}

func parseToolResult(data []byte) (*MCPToolResult, error) {
	if len(data) == 0 {
		return &MCPToolResult{}, nil
	}
	var result MCPToolResult
	if err := json.Unmarshal(data, &result); err == nil {
		if result.Content != nil || result.Structured != nil || result.Raw != nil {
			if result.Raw == nil {
				result.Raw = rawMap(data)
			}
			return &result, nil
		}
	}

	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decode MCP tool result: %w", err)
	}
	output := &MCPToolResult{
		Content: decoded,
		Raw:     rawMap(data),
	}
	if structured, ok := decoded.(map[string]any); ok {
		output.Structured = structured
	}
	return output, nil
}

func rawMap(data []byte) map[string]any {
	var out map[string]any
	if err := json.Unmarshal(data, &out); err == nil && out != nil {
		return out
	}
	return map[string]any{}
}
