package openapi

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Spec returns the OpenAPI document served by /swagger/doc.json and generated
// by cmd/openapi.
func Spec() map[string]any {
	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "Local Agent API",
			"description": "Single-user local Codex-like Agent API.",
			"version":     "0.11.0",
		},
		"servers": []map[string]any{
			{"url": "http://127.0.0.1:8765"},
		},
		"paths":      paths(),
		"components": components(),
	}
}

// WriteFile writes the generated OpenAPI document to path.
func WriteFile(path string) error {
	raw, err := json.MarshalIndent(Spec(), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func paths() map[string]any {
	return map[string]any{
		"/v1/health": get("Get service health", "HealthResponse"),

		"/v1/conversations": map[string]any{
			"get":  operation("List conversations", nil, "", "ConversationListResponse", httpStatusOK()),
			"post": operation("Create conversation", nil, "CreateConversationRequest", "Conversation", httpStatusCreated()),
		},
		"/v1/conversations/{conversation_id}": map[string]any{
			"get": operation("Get conversation", []map[string]any{pathParam("conversation_id")}, "", "Conversation", httpStatusOK()),
		},
		"/v1/conversations/{conversation_id}/messages": map[string]any{
			"get":  operation("List conversation messages", []map[string]any{pathParam("conversation_id")}, "", "MessageListResponse", httpStatusOK()),
			"post": operation("Post user message", []map[string]any{pathParam("conversation_id")}, "PostMessageRequest", "RunResponse", httpStatusOK()),
		},

		"/v1/approvals/pending": get("List pending approvals", "ApprovalListResponse"),
		"/v1/approvals/{approval_id}/approve": map[string]any{
			"post": operation("Approve pending action", []map[string]any{pathParam("approval_id")}, "EmptyObject", "ApprovalResolutionResponse", httpStatusOK()),
		},
		"/v1/approvals/{approval_id}/reject": map[string]any{
			"post": operation("Reject pending action", []map[string]any{pathParam("approval_id")}, "RejectApprovalRequest", "ApprovalResolutionResponse", httpStatusOK()),
		},
		"/v1/runs": map[string]any{
			"get": operation("List workflow runs", []map[string]any{queryParam("status"), queryIntParam("limit")}, "", "RunListResponse", httpStatusOK()),
		},
		"/v1/runs/{run_id}": map[string]any{
			"get": operation("Get workflow run", []map[string]any{pathParam("run_id")}, "", "RunState", httpStatusOK()),
		},
		"/v1/runs/{run_id}/steps": map[string]any{
			"get": operation("List workflow run steps", []map[string]any{pathParam("run_id")}, "", "RunStepListResponse", httpStatusOK()),
		},
		"/v1/runs/{run_id}/resume": map[string]any{
			"post": operation("Resume paused workflow run", []map[string]any{pathParam("run_id")}, "ResumeRunRequest", "RunResponse", httpStatusOK()),
		},
		"/v1/runs/{run_id}/cancel": map[string]any{
			"post": operation("Cancel workflow run", []map[string]any{pathParam("run_id")}, "EmptyObject", "RunState", httpStatusOK()),
		},

		"/v1/memory/files": get("List memory files", "MemoryFileListResponse"),
		"/v1/memory/files/{path}": map[string]any{
			"get": operation("Read memory file", []map[string]any{pathParam("path")}, "", "MemoryFile", httpStatusOK()),
		},
		"/v1/memory/search": map[string]any{
			"post": operation("Search memory", nil, "SearchRequest", "MemoryFileListResponse", httpStatusOK()),
		},
		"/v1/memory/patches": map[string]any{
			"post": operation("Create memory patch", nil, "CreateMemoryPatchRequest", "MemoryPatch", httpStatusCreated()),
		},
		"/v1/memory/reindex": map[string]any{
			"post": operation("Reindex memory", nil, "EmptyObject", "StatusResponse", httpStatusOK()),
		},

		"/v1/kbs/health": get("Get knowledge base health", "KnowledgeBaseHealthResponse"),
		"/v1/kbs": map[string]any{
			"get":  operation("List knowledge bases", nil, "", "KnowledgeBaseListResponse", httpStatusOK(), featureDisabledResponse()),
			"post": operation("Create knowledge base", nil, "CreateKnowledgeBaseRequest", "KnowledgeBase", httpStatusCreated(), featureDisabledResponse()),
		},
		"/v1/kbs/{kb_id}/documents/upload": map[string]any{
			"post": operation("Upload KB document", []map[string]any{pathParam("kb_id")}, "UploadDocumentRequest", "KBChunkListResponse", httpStatusCreated(), featureDisabledResponse()),
		},
		"/v1/kbs/{kb_id}/search": map[string]any{
			"post": operation("Search knowledge base", []map[string]any{pathParam("kb_id")}, "SearchRequest", "KBChunkListResponse", httpStatusOK(), featureDisabledResponse()),
		},

		"/v1/skills/upload": map[string]any{
			"post": operation("Upload local skill", nil, "UploadSkillRequest", "SkillRegistration", httpStatusCreated()),
		},
		"/v1/skills": get("List skills", "SkillListResponse"),
		"/v1/skills/{id}": map[string]any{
			"get": operation("Get skill", []map[string]any{pathParam("id")}, "", "SkillDetailResponse", httpStatusOK()),
		},
		"/v1/skills/{id}/enable": map[string]any{
			"post": operation("Enable skill", []map[string]any{pathParam("id")}, "EmptyObject", "SkillRegistration", httpStatusOK()),
		},
		"/v1/skills/{id}/disable": map[string]any{
			"post": operation("Disable skill", []map[string]any{pathParam("id")}, "EmptyObject", "SkillRegistration", httpStatusOK()),
		},
		"/v1/skills/{id}/test": map[string]any{
			"post": operation("Validate skill input", []map[string]any{pathParam("id")}, "SkillRunRequest", "StatusResponse", httpStatusOK()),
		},
		"/v1/skills/{id}/run": map[string]any{
			"post": operation("Run skill through approval chain", []map[string]any{pathParam("id")}, "SkillRunRequest", "ToolRouteResponse", httpStatusOK()),
		},

		"/v1/mcp/servers": map[string]any{
			"get":  operation("List MCP servers", nil, "", "MCPServerListResponse", httpStatusOK()),
			"post": operation("Create MCP server", nil, "MCPServerInput", "MCPServer", httpStatusCreated()),
		},
		"/v1/mcp/servers/{id}": map[string]any{
			"get":   operation("Get MCP server", []map[string]any{pathParam("id")}, "", "MCPServerDetailResponse", httpStatusOK()),
			"patch": operation("Update MCP server", []map[string]any{pathParam("id")}, "MCPServerInput", "MCPServer", httpStatusOK()),
		},
		"/v1/mcp/servers/{id}/refresh": map[string]any{
			"post": operation("Refresh MCP tools", []map[string]any{pathParam("id")}, "EmptyObject", "MCPRefreshResponse", httpStatusOK()),
		},
		"/v1/mcp/servers/{id}/test": map[string]any{
			"post": operation("Test MCP server", []map[string]any{pathParam("id")}, "EmptyObject", "StatusResponse", httpStatusOK()),
		},
		"/v1/mcp/servers/{id}/tools/{tool_name}/call": map[string]any{
			"post": operation("Call MCP tool through approval chain", []map[string]any{pathParam("id"), pathParam("tool_name")}, "MCPCallToolRequest", "ToolRouteResponse", httpStatusOK()),
		},
		"/v1/mcp/tools": get("List MCP tool policies", "MCPToolPolicyListResponse"),
		"/v1/mcp/tools/{id}/policy": map[string]any{
			"patch": operation("Update MCP tool policy", []map[string]any{pathParam("id")}, "MCPToolPolicyInput", "MCPToolPolicy", httpStatusOK()),
		},
	}
}

func components() map[string]any {
	return map[string]any{
		"schemas": map[string]any{
			"ErrorResponse": object(map[string]any{
				"code":    stringSchema("feature_disabled"),
				"message": stringSchema("knowledge base is disabled"),
				"details": map[string]any{"type": "object", "additionalProperties": true},
			}, "code", "message"),
			"EmptyObject":    object(map[string]any{}),
			"StatusResponse": object(map[string]any{"status": stringSchema("ok")}, "status"),
			"HealthResponse": object(map[string]any{
				"status":         stringSchema("ok"),
				"service":        stringSchema("local-agent"),
				"database":       map[string]any{"type": "object", "additionalProperties": true},
				"qdrant":         map[string]any{"type": "object", "additionalProperties": true},
				"knowledge_base": map[string]any{"type": "object", "additionalProperties": true},
				"workflow":       map[string]any{"type": "object", "additionalProperties": true},
				"docs":           map[string]any{"type": "object", "additionalProperties": true},
			}, "status", "service"),
			"Conversation": object(map[string]any{"id": stringSchema("conv_x"), "title": stringSchema("smoke"), "project_key": stringSchema("default"), "created_at": timeSchema(), "updated_at": timeSchema(), "archived": boolSchema()}, "id", "title"),
			"Message":      object(map[string]any{"id": stringSchema("msg_x"), "conversation_id": stringSchema("conv_x"), "role": stringSchema("assistant"), "content": stringSchema("hello"), "created_at": timeSchema()}, "id", "conversation_id", "role"),
			"ToolResult":   object(map[string]any{"tool_call_id": stringSchema("tool_x"), "output": map[string]any{"type": "object", "additionalProperties": true}, "error": stringSchema("")}),
			"ApprovalRecord": object(map[string]any{
				"id":             stringSchema("apr_x"),
				"status":         stringSchema("pending"),
				"input_snapshot": map[string]any{"type": "object", "additionalProperties": true},
				"snapshot_hash":  stringSchema("sha256:..."),
				"summary":        stringSchema("requires approval"),
			}, "id", "status", "input_snapshot"),
			"RunState": object(map[string]any{
				"run_id":             stringSchema("run_x"),
				"conversation_id":    stringSchema("conv_x"),
				"status":             stringSchema("paused_for_approval"),
				"current_step":       stringSchema("request_approval"),
				"current_step_index": integerSchema(4),
				"step_count":         integerSchema(2),
				"max_steps":          integerSchema(6),
				"user_message":       stringSchema("install axios"),
				"approval_id":        stringSchema("apr_x"),
				"error":              stringSchema(""),
				"created_at":         timeSchema(),
				"updated_at":         timeSchema(),
			}, "run_id", "status", "created_at", "updated_at"),
			"RunStep": object(map[string]any{
				"step_id":     stringSchema("step_x"),
				"run_id":      stringSchema("run_x"),
				"index":       integerSchema(4),
				"type":        stringSchema("request_approval"),
				"status":      stringSchema("paused"),
				"proposal":    map[string]any{"type": "object", "additionalProperties": true},
				"inference":   map[string]any{"type": "object", "additionalProperties": true},
				"policy":      map[string]any{"type": "object", "additionalProperties": true},
				"approval":    ref("ApprovalRecord"),
				"tool_result": ref("ToolResult"),
				"summary":     stringSchema("requires approval"),
				"error":       stringSchema(""),
				"created_at":  timeSchema(),
				"updated_at":  timeSchema(),
			}, "step_id", "run_id", "index", "type", "status", "created_at", "updated_at"),
			"MemoryFile":        object(map[string]any{"path": stringSchema("preferences.md"), "body": stringSchema("# Preferences")}, "path", "body"),
			"MemoryPatch":       object(map[string]any{"id": stringSchema("mempatch_x"), "path": stringSchema("preferences.md"), "body": stringSchema("content"), "summary": stringSchema("update")}, "id", "path", "body"),
			"KnowledgeBase":     object(map[string]any{"id": stringSchema("kb_x"), "name": stringSchema("docs"), "description": stringSchema("local docs"), "created_at": timeSchema()}, "id", "name"),
			"KBChunk":           object(map[string]any{"id": stringSchema("kbch_x"), "kb_id": stringSchema("kb_x"), "document": stringSchema("intro.md"), "content": stringSchema("hello"), "score": numberSchema()}, "id", "kb_id", "document", "content"),
			"SkillRegistration": object(map[string]any{"id": stringSchema("skill_x"), "name": stringSchema("demo"), "enabled": boolSchema(), "effects": arrayOfString()}, "id", "name", "enabled"),
			"MCPServer":         object(map[string]any{"id": stringSchema("filesystem"), "name": stringSchema("filesystem"), "transport": stringSchema("http"), "url": stringSchema("http://127.0.0.1:3000"), "enabled": boolSchema()}, "id", "name", "transport", "enabled"),
			"MCPToolPolicy":     object(map[string]any{"id": stringSchema("mcp.filesystem.read_file"), "effects": arrayOfString(), "approval": stringSchema("auto"), "risk_level": stringSchema("read")}, "id"),

			"CreateConversationRequest": object(map[string]any{"title": stringSchema("smoke"), "project_key": stringSchema("local")}),
			"PostMessageRequest":        object(map[string]any{"content": stringSchema("hello")}, "content"),
			"RejectApprovalRequest":     object(map[string]any{"reason": stringSchema("not needed")}),
			"ResumeRunRequest":          object(map[string]any{"approval_id": stringSchema("apr_x"), "approved": boolSchema()}, "approved"),
			"SearchRequest":             object(map[string]any{"query": stringSchema("hello"), "limit": integerSchema(5)}, "query"),
			"CreateMemoryPatchRequest":  object(map[string]any{"path": stringSchema("preferences.md"), "summary": stringSchema("update"), "body": stringSchema("content"), "frontmatter": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}}, "path", "body"),
			"CreateKnowledgeBaseRequest": object(map[string]any{
				"name":        stringSchema("docs"),
				"description": stringSchema("local docs"),
			}, "name"),
			"UploadDocumentRequest": object(map[string]any{"filename": stringSchema("intro.md"), "content": stringSchema("# Intro")}, "filename", "content"),
			"UploadSkillRequest":    object(map[string]any{"path": stringSchema("./skills/demo"), "name": stringSchema("demo"), "description": stringSchema("demo skill")}, "path"),
			"SkillRunRequest":       object(map[string]any{"args": map[string]any{"type": "object", "additionalProperties": true}}),
			"MCPServerInput":        object(map[string]any{"id": stringSchema("filesystem"), "name": stringSchema("filesystem"), "transport": stringSchema("http"), "url": stringSchema("http://127.0.0.1:3000"), "enabled": boolSchema(), "headers": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}}, "name", "transport"),
			"MCPCallToolRequest":    object(map[string]any{"arguments": map[string]any{"type": "object", "additionalProperties": true}, "purpose": stringSchema("read a local file")}),
			"MCPToolPolicyInput":    object(map[string]any{"effects": arrayOfString(), "approval": stringSchema("require"), "risk_level": stringSchema("write"), "reason": stringSchema("mutating tool")}),

			"ConversationListResponse":    listResponse("Conversation"),
			"MessageListResponse":         listResponse("Message"),
			"ApprovalListResponse":        listResponse("ApprovalRecord"),
			"RunListResponse":             listResponse("RunState"),
			"RunStepListResponse":         listResponse("RunStep"),
			"MemoryFileListResponse":      listResponse("MemoryFile"),
			"KnowledgeBaseListResponse":   listResponse("KnowledgeBase"),
			"KBChunkListResponse":         listResponse("KBChunk"),
			"SkillListResponse":           listResponse("SkillRegistration"),
			"MCPServerListResponse":       listResponse("MCPServer"),
			"MCPToolPolicyListResponse":   listResponse("MCPToolPolicy"),
			"KnowledgeBaseHealthResponse": object(map[string]any{"enabled": boolSchema(), "provider": stringSchema("qdrant"), "status": stringSchema("ok")}),
			"RunResponse":                 object(map[string]any{"run_id": stringSchema("run_x"), "assistant_message": ref("Message"), "tool_result": ref("ToolResult"), "approval": ref("ApprovalRecord"), "state": ref("RunState")}, "run_id"),
			"ApprovalResolutionResponse":  object(map[string]any{"approval": ref("ApprovalRecord"), "result": ref("ToolResult"), "run": ref("RunResponse")}),
			"ToolRouteResponse":           object(map[string]any{"decision": map[string]any{"type": "object", "additionalProperties": true}, "inference": map[string]any{"type": "object", "additionalProperties": true}, "approval": ref("ApprovalRecord"), "result": ref("ToolResult")}),
			"SkillDetailResponse":         object(map[string]any{"skill": ref("SkillRegistration"), "manifest": map[string]any{"type": "object", "additionalProperties": true}}),
			"MCPServerDetailResponse":     object(map[string]any{"server": ref("MCPServer"), "state": map[string]any{"type": "object", "additionalProperties": true}}),
			"MCPRefreshResponse":          object(map[string]any{"status": stringSchema("ok"), "tools": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}}, "state": map[string]any{"type": "object", "additionalProperties": true}}, "status"),
		},
	}
}

func get(summary, responseSchema string) map[string]any {
	return map[string]any{"get": operation(summary, nil, "", responseSchema, httpStatusOK())}
}

func operation(summary string, params []map[string]any, requestSchema, responseSchema string, success map[string]any, extraResponses ...map[string]any) map[string]any {
	responses := map[string]any{
		success["status"].(string): response(success["description"].(string), responseSchema),
		"400":                      response("Bad request", "ErrorResponse"),
		"500":                      response("Internal server error", "ErrorResponse"),
	}
	for _, item := range extraResponses {
		for key, value := range item {
			responses[key] = value
		}
	}
	op := map[string]any{
		"summary":   summary,
		"responses": responses,
	}
	if len(params) > 0 {
		op["parameters"] = params
	}
	if requestSchema != "" {
		op["requestBody"] = map[string]any{
			"required": true,
			"content": map[string]any{
				"application/json": map[string]any{"schema": ref(requestSchema)},
			},
		}
	}
	return op
}

func response(description, schema string) map[string]any {
	item := map[string]any{"description": description}
	if schema != "" {
		item["content"] = map[string]any{
			"application/json": map[string]any{"schema": ref(schema)},
		}
	}
	return item
}

func featureDisabledResponse() map[string]any {
	return map[string]any{"503": response("Feature disabled", "ErrorResponse")}
}

func httpStatusOK() map[string]any {
	return map[string]any{"status": "200", "description": "OK"}
}

func httpStatusCreated() map[string]any {
	return map[string]any{"status": "201", "description": "Created"}
}

func pathParam(name string) map[string]any {
	return map[string]any{
		"name":     name,
		"in":       "path",
		"required": true,
		"schema":   map[string]any{"type": "string"},
	}
}

func queryParam(name string) map[string]any {
	return map[string]any{
		"name":     name,
		"in":       "query",
		"required": false,
		"schema":   map[string]any{"type": "string"},
	}
}

func queryIntParam(name string) map[string]any {
	return map[string]any{
		"name":     name,
		"in":       "query",
		"required": false,
		"schema":   map[string]any{"type": "integer"},
	}
}

func ref(schema string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + schema}
}

func listResponse(schema string) map[string]any {
	return object(map[string]any{
		"items": map[string]any{
			"type":  "array",
			"items": ref(schema),
		},
	}, "items")
}

func object(properties map[string]any, required ...string) map[string]any {
	out := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func stringSchema(example string) map[string]any {
	out := map[string]any{"type": "string"}
	if example != "" {
		out["example"] = example
	}
	return out
}

func integerSchema(example int) map[string]any {
	return map[string]any{"type": "integer", "example": example}
}

func numberSchema() map[string]any {
	return map[string]any{"type": "number", "format": "double"}
}

func boolSchema() map[string]any {
	return map[string]any{"type": "boolean"}
}

func timeSchema() map[string]any {
	return map[string]any{"type": "string", "format": "date-time"}
}

func arrayOfString() map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
}
