package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"local-agent/internal/core"
)

var rpcID uint64

type MCPErrorCode string

const (
	MCPErrorInvalidResponse   MCPErrorCode = "invalid_response"
	MCPErrorTimeout           MCPErrorCode = "timeout"
	MCPErrorTransportFailure  MCPErrorCode = "transport_failure"
	MCPErrorProtocolViolation MCPErrorCode = "protocol_violation"
	MCPErrorServerError       MCPErrorCode = "server_error"
	MCPErrorUnknownTool       MCPErrorCode = "unknown_tool"
)

// MCPError is the normalized transport/protocol error shape exposed above MCP transports.
type MCPError struct {
	Code    MCPErrorCode `json:"code"`
	Message string       `json:"message"`
	Cause   error        `json:"-"`
}

func (e *MCPError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Message
}

func (e *MCPError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func newMCPError(code MCPErrorCode, message string, cause error) *MCPError {
	if message == "" && cause != nil {
		message = cause.Error()
	}
	return &MCPError{Code: code, Message: message, Cause: cause}
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
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

func newRPCNotification(method string, params any) rpcNotification {
	return rpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
}

func newInitializeRequest() rpcRequest {
	return newRPCRequest("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "local-agent",
			"version": "0.1.0",
		},
	})
}

func newInitializedNotification() rpcNotification {
	return newRPCNotification("notifications/initialized", map[string]any{})
}

func decodeRPCResponse(data []byte, expectedID string, profile core.MCPCompatibilityProfile) (json.RawMessage, error) {
	frame, err := selectResponseFrame(data, profile)
	if err != nil {
		return nil, err
	}

	var probe map[string]json.RawMessage
	if err := json.Unmarshal(frame, &probe); err != nil {
		return nil, newMCPError(MCPErrorInvalidResponse, "decode MCP response JSON", err)
	}
	if !rawLooksLikeRPC(probe) {
		if profile.Dialect == DialectEnvelopeWrapped {
			if payload, ok := unwrapEnvelopeRaw(probe); ok {
				if payloadLooksLikeRPC(payload) {
					return decodeRPCResponse(payload, expectedID, profile)
				}
				return payload, nil
			}
		}
		return nil, newMCPError(MCPErrorProtocolViolation, "MCP response is not JSON-RPC", nil)
	}

	var response rpcResponse
	if err := json.Unmarshal(frame, &response); err != nil {
		return nil, newMCPError(MCPErrorInvalidResponse, "decode MCP JSON-RPC response", err)
	}
	if response.Error != nil {
		if response.Error.Message == "" {
			response.Error.Message = "unknown MCP JSON-RPC error"
		}
		return nil, newMCPError(MCPErrorServerError, fmt.Sprintf("MCP JSON-RPC error %d: %s", response.Error.Code, response.Error.Message), nil)
	}
	if profile.StrictIDMatching && expectedID != "" && response.ID != "" && response.ID != expectedID {
		return nil, newMCPError(MCPErrorProtocolViolation, fmt.Sprintf("MCP response id mismatch: got %s want %s", response.ID, expectedID), nil)
	}
	if len(response.Result) == 0 {
		return []byte("{}"), nil
	}
	return response.Result, nil
}

func selectResponseFrame(data []byte, profile core.MCPCompatibilityProfile) ([]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, newMCPError(MCPErrorInvalidResponse, "empty MCP response", nil)
	}
	if profile.Dialect != DialectLineDelimitedJSONRPC {
		return trimmed, nil
	}
	lines := bytes.Split(trimmed, []byte{'\n'})
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if json.Valid(line) {
			return line, nil
		}
	}
	return nil, newMCPError(MCPErrorInvalidResponse, "line-delimited MCP response contains no JSON frame", nil)
}

func rawLooksLikeRPC(probe map[string]json.RawMessage) bool {
	if probe == nil {
		return false
	}
	_, hasResult := probe["result"]
	_, hasError := probe["error"]
	_, hasJSONRPC := probe["jsonrpc"]
	return hasResult || hasError || hasJSONRPC
}

func payloadLooksLikeRPC(data []byte) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return rawLooksLikeRPC(probe)
}

func unwrapEnvelopeRaw(probe map[string]json.RawMessage) (json.RawMessage, bool) {
	for _, key := range []string{"data", "payload", "response"} {
		if raw, ok := probe[key]; ok && len(raw) > 0 && string(raw) != "null" {
			return raw, true
		}
	}
	return nil, false
}

func parseToolsResult(data []byte, profile core.MCPCompatibilityProfile) ([]MCPToolSchema, error) {
	data = unwrapResultEnvelope(data, profile)
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}

	var direct []map[string]any
	if err := json.Unmarshal(data, &direct); err == nil && direct != nil {
		return normalizeToolSchemas(direct, profile)
	}

	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, newMCPError(MCPErrorInvalidResponse, "decode MCP tools response", err)
	}
	if raw, ok := wrapped["tools"]; ok {
		var items []map[string]any
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, newMCPError(MCPErrorInvalidResponse, "decode MCP tools list", err)
		}
		return normalizeToolSchemas(items, profile)
	}
	if raw, ok := wrapped["data"]; ok && profile.Dialect == DialectEnvelopeWrapped {
		return parseToolsResult(raw, profile)
	}
	return nil, newMCPError(MCPErrorProtocolViolation, "MCP tools response does not contain tools", nil)
}

func normalizeToolSchemas(items []map[string]any, profile core.MCPCompatibilityProfile) ([]MCPToolSchema, error) {
	out := make([]MCPToolSchema, 0, len(items))
	for index, item := range items {
		schema, err := normalizeToolSchema(item, profile)
		if err != nil {
			return nil, newMCPError(MCPErrorInvalidResponse, fmt.Sprintf("invalid MCP tool schema at index %d: %s", index, err.Error()), err)
		}
		out = append(out, schema)
	}
	return out, nil
}

func normalizeToolSchema(item map[string]any, profile core.MCPCompatibilityProfile) (MCPToolSchema, error) {
	name := strings.TrimSpace(stringValue(item["name"]))
	if name == "" {
		return MCPToolSchema{}, fmt.Errorf("name is required")
	}
	description := stringValue(item["description"])
	inputSchema, err := schemaMap(item)
	if err != nil {
		return MCPToolSchema{}, err
	}
	if inputSchema == nil {
		if !profile.AcceptMissingSchema {
			return MCPToolSchema{}, fmt.Errorf("input schema is required by compatibility profile")
		}
		inputSchema = map[string]any{}
	}

	metadata := mapValue(item["metadata"])
	if metadata == nil {
		metadata = map[string]any{}
	}
	for _, key := range []string{"effects", "effect", "approval", "requires_approval", "confidence", "risk_level"} {
		if value, ok := item[key]; ok {
			metadata[key] = value
		}
	}
	if annotations := mapValue(item["annotations"]); annotations != nil {
		metadata["annotations"] = annotations
	}
	if profile.AcceptExtraMetadata {
		for key, value := range item {
			if isKnownToolSchemaField(key) {
				continue
			}
			metadata[key] = value
		}
	}

	return MCPToolSchema{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Metadata:    metadata,
	}, nil
}

func parseToolResult(data []byte, profile core.MCPCompatibilityProfile) (*MCPToolResult, error) {
	data = unwrapResultEnvelope(data, profile)
	if len(bytes.TrimSpace(data)) == 0 {
		return &MCPToolResult{}, nil
	}

	var object map[string]any
	if err := json.Unmarshal(data, &object); err == nil && object != nil {
		if isError, _ := object["isError"].(bool); isError {
			return nil, newMCPError(MCPErrorServerError, "MCP tool returned isError result", nil)
		}
		result := &MCPToolResult{Raw: rawMap(data)}
		if structured := mapValue(object["structured"]); structured != nil {
			result.Structured = structured
		}
		if structured := mapValue(object["structuredContent"]); structured != nil && result.Structured == nil {
			result.Structured = structured
		}
		if content, ok := object["content"]; ok {
			result.Content = normalizeContent(content, profile)
		}
		if result.Content != nil || result.Structured != nil || len(result.Raw) > 0 {
			return result, nil
		}
	}

	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, newMCPError(MCPErrorInvalidResponse, "decode MCP tool result", err)
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

func unwrapResultEnvelope(data []byte, profile core.MCPCompatibilityProfile) []byte {
	if profile.Dialect != DialectEnvelopeWrapped {
		return data
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return data
	}
	if raw, ok := unwrapEnvelopeRaw(probe); ok {
		return raw
	}
	return data
}

func normalizeContent(content any, profile core.MCPCompatibilityProfile) any {
	if !profile.AcceptTextOnlyResult {
		return content
	}
	items, ok := content.([]any)
	if !ok {
		return content
	}
	textParts := make([]string, 0, len(items))
	allText := true
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok || strings.ToLower(stringValue(object["type"])) != "text" {
			allText = false
			break
		}
		textParts = append(textParts, stringValue(object["text"]))
	}
	if allText {
		return strings.Join(textParts, "\n")
	}
	return content
}

func schemaMap(item map[string]any) (map[string]any, error) {
	for _, key := range []string{"input_schema", "inputSchema"} {
		value, ok := item[key]
		if !ok || value == nil {
			continue
		}
		out := mapValue(value)
		if out == nil {
			return nil, fmt.Errorf("%s must be an object", key)
		}
		return out, nil
	}
	return nil, nil
}

func isKnownToolSchemaField(key string) bool {
	switch key {
	case "name", "description", "input_schema", "inputSchema", "metadata", "annotations", "effects", "effect", "approval", "requires_approval", "confidence", "risk_level":
		return true
	default:
		return false
	}
}

func stringValue(value any) string {
	switch item := value.(type) {
	case string:
		return item
	default:
		return ""
	}
}

func mapValue(value any) map[string]any {
	switch item := value.(type) {
	case map[string]any:
		return item
	default:
		return nil
	}
}

func rawMap(data []byte) map[string]any {
	var out map[string]any
	if err := json.Unmarshal(data, &out); err == nil && out != nil {
		return out
	}
	return map[string]any{}
}
