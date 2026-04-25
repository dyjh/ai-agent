package catalog

import (
	"sort"

	"local-agent/internal/core"
)

// PlanningToolSpec is the planner-safe projection of a ToolSpec.
type PlanningToolSpec struct {
	ToolID           string         `json:"tool_id"`
	Domain           string         `json:"domain"`
	Description      string         `json:"description"`
	InputSchema      map[string]any `json:"input_schema,omitempty"`
	DefaultEffects   []string       `json:"default_effects,omitempty"`
	AutoSelectable   bool           `json:"auto_selectable"`
	RequiresApproval bool           `json:"requires_approval"`
	Examples         []ToolExample  `json:"examples,omitempty"`
	NegativeExamples []ToolExample  `json:"negative_examples,omitempty"`
}

// ToolExample documents planner behavior without becoming a matching rule.
type ToolExample struct {
	User  string         `json:"user"`
	Input map[string]any `json:"input"`
}

// Registry is the ToolRegistry surface needed by the planning catalog.
type Registry interface {
	List() []core.ToolSpec
}

// PlanningCatalog contains tool specs that may be selected by planners.
type PlanningCatalog struct {
	tools map[string]PlanningToolSpec
}

// New builds a catalog from a ToolRegistry, adding fallback core tools when
// registry is nil or incomplete for planner-only tests.
func New(registry Registry) PlanningCatalog {
	specs := map[string]core.ToolSpec{}
	for _, spec := range coreToolSpecs() {
		specs[spec.Name] = spec
	}
	if registry != nil {
		for _, spec := range registry.List() {
			if spec.Name == "" {
				spec.Name = spec.ID
			}
			if spec.ID == "" {
				spec.ID = spec.Name
			}
			specs[spec.Name] = spec
		}
	}
	c := PlanningCatalog{tools: map[string]PlanningToolSpec{}}
	for _, spec := range specs {
		tool := spec.Name
		if tool == "" {
			tool = spec.ID
		}
		examples, negatives := examplesForTool(tool)
		effects := append([]string(nil), spec.DefaultEffects...)
		c.tools[tool] = PlanningToolSpec{
			ToolID:           tool,
			Domain:           domainForTool(tool),
			Description:      spec.Description,
			InputSchema:      cloneMap(spec.InputSchema),
			DefaultEffects:   effects,
			AutoSelectable:   autoSelectable(tool, effects),
			RequiresApproval: requiresApproval(tool, effects),
			Examples:         cloneExamples(examples),
			NegativeExamples: cloneExamples(negatives),
		}
	}
	return c
}

// Tool returns one planning tool spec.
func (c PlanningCatalog) Tool(tool string) (PlanningToolSpec, bool) {
	spec, ok := c.tools[tool]
	return spec, ok
}

// Has reports whether a tool is known.
func (c PlanningCatalog) Has(tool string) bool {
	_, ok := c.tools[tool]
	return ok
}

// All returns a deterministic list of tool specs.
func (c PlanningCatalog) All() []PlanningToolSpec {
	items := make([]PlanningToolSpec, 0, len(c.tools))
	for _, spec := range c.tools {
		items = append(items, spec)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ToolID < items[j].ToolID })
	return items
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneExamples(in []ToolExample) []ToolExample {
	if len(in) == 0 {
		return nil
	}
	out := make([]ToolExample, 0, len(in))
	for _, item := range in {
		out = append(out, ToolExample{User: item.User, Input: cloneMap(item.Input)})
	}
	return out
}

func coreToolSpecs() []core.ToolSpec {
	return []core.ToolSpec{
		{Name: "shell.exec", Description: "Execute a shell command after effect inference and policy checks", InputSchema: map[string]any{"shell": "string", "command": "string", "cwd": "string", "timeout_seconds": "number", "purpose": "string"}, DefaultEffects: []string{"unknown.effect"}},
		{Name: "code.read_file", Description: "Read a file from the workspace", InputSchema: map[string]any{"path": "string", "max_bytes": "number"}, DefaultEffects: []string{"read", "code.read"}},
		{Name: "code.search_text", Description: "Search text in workspace files and return line matches", InputSchema: map[string]any{"path": "string", "query": "string", "limit": "number"}, DefaultEffects: []string{"read", "code.read"}},
		{Name: "code.inspect_project", Description: "Inspect project language, config files, and likely commands", InputSchema: map[string]any{"path": "string"}, DefaultEffects: []string{"read", "code.read"}},
		{Name: "code.run_tests", Description: "Run an allowlisted test command inside the workspace", InputSchema: map[string]any{"workspace": "string", "use_detected": "boolean", "timeout_seconds": "number", "max_output_bytes": "number"}, DefaultEffects: []string{"code.test", "process.read", "fs.read"}},
		{Name: "code.fix_test_failure_loop", Description: "Run tests and prepare the next repair-loop action without applying code changes", InputSchema: map[string]any{"workspace": "string", "use_detected": "boolean", "max_iterations": "number"}, DefaultEffects: []string{"code.test", "process.read", "fs.read", "code.plan"}},
		{Name: "code.parse_test_failure", Description: "Parse test output into structured failure information", InputSchema: map[string]any{"workspace": "string", "command": "string", "stdout": "string", "stderr": "string", "exit_code": "number"}, DefaultEffects: []string{"read", "code.plan"}},
		{Name: "code.propose_patch", Description: "Preview a code patch without applying it", InputSchema: map[string]any{"path": "string", "diff": "string"}, DefaultEffects: []string{"read", "code.plan"}},
		{Name: "code.apply_patch", Description: "Apply a patch inside the workspace", InputSchema: map[string]any{"path": "string", "diff": "string"}, DefaultEffects: []string{"fs.write", "code.modify"}},
		{Name: "code.validate_patch", Description: "Validate a patch without modifying files", InputSchema: map[string]any{"path": "string", "diff": "string"}, DefaultEffects: []string{"read", "code.plan"}},
		{Name: "git.status", Description: "Read git working tree status", InputSchema: map[string]any{"workspace": "string"}, DefaultEffects: []string{"read", "git.read"}},
		{Name: "git.diff", Description: "Read git diff", InputSchema: map[string]any{"workspace": "string"}, DefaultEffects: []string{"read", "git.read"}},
		{Name: "git.diff_summary", Description: "Summarize git diff", InputSchema: map[string]any{"workspace": "string", "staged": "boolean"}, DefaultEffects: []string{"read", "git.read"}},
		{Name: "git.commit_message_proposal", Description: "Propose a git commit message", InputSchema: map[string]any{"workspace": "string"}, DefaultEffects: []string{"read", "git.read"}},
		{Name: "git.log", Description: "Read git log", InputSchema: map[string]any{"workspace": "string", "limit": "number"}, DefaultEffects: []string{"read", "git.read"}},
		{Name: "git.add", Description: "Stage files", InputSchema: map[string]any{"workspace": "string", "paths": "array"}, DefaultEffects: []string{"git.write", "fs.write"}},
		{Name: "git.commit", Description: "Create a commit", InputSchema: map[string]any{"workspace": "string", "message": "string"}, DefaultEffects: []string{"git.write", "fs.write"}},
		{Name: "git.clean", Description: "Delete untracked files", InputSchema: map[string]any{"workspace": "string"}, DefaultEffects: []string{"fs.delete", "dangerous"}},
		{Name: "ops.local.system_info", Description: "Read local system information", InputSchema: map[string]any{}, DefaultEffects: []string{"read", "system.read"}},
		{Name: "ops.local.processes", Description: "Read local process list", InputSchema: map[string]any{}, DefaultEffects: []string{"process.read", "system.metrics.read"}},
		{Name: "ops.local.disk_usage", Description: "Read local disk usage", InputSchema: map[string]any{}, DefaultEffects: []string{"disk.read", "system.metrics.read"}},
		{Name: "ops.local.memory_usage", Description: "Read local memory usage", InputSchema: map[string]any{}, DefaultEffects: []string{"memory.read", "system.metrics.read"}},
		{Name: "ops.local.logs_tail", Description: "Read local logs", InputSchema: map[string]any{"path": "string", "max_lines": "number"}, DefaultEffects: []string{"log.read"}},
		{Name: "ops.local.service_restart", Description: "Restart a local service", InputSchema: map[string]any{"service": "string"}, DefaultEffects: []string{"service.restart", "system.write"}},
		{Name: "ops.docker.ps", Description: "List docker containers", InputSchema: map[string]any{}, DefaultEffects: []string{"container.read"}},
		{Name: "ops.docker.logs", Description: "Read docker logs", InputSchema: map[string]any{"container": "string", "max_lines": "number"}, DefaultEffects: []string{"container.read", "log.read"}},
		{Name: "ops.docker.restart", Description: "Restart a docker container", InputSchema: map[string]any{"container": "string"}, DefaultEffects: []string{"container.restart", "container.write"}},
		{Name: "ops.k8s.get", Description: "Get Kubernetes resources", InputSchema: map[string]any{"resource": "string"}, DefaultEffects: []string{"k8s.read"}},
		{Name: "ops.k8s.logs", Description: "Read Kubernetes logs", InputSchema: map[string]any{"target": "string", "max_lines": "number"}, DefaultEffects: []string{"k8s.read", "log.read"}},
		{Name: "ops.k8s.apply", Description: "Apply Kubernetes manifest", InputSchema: map[string]any{"manifest_path": "string"}, DefaultEffects: []string{"k8s.write", "network.write"}},
		{Name: "kb.answer", Description: "Answer using knowledge base evidence", InputSchema: map[string]any{"kb_id": "string", "query": "string", "mode": "string", "top_k": "number", "require_citations": "boolean", "rerank": "boolean"}, DefaultEffects: []string{"kb.read"}},
		{Name: "kb.retrieve", Description: "Retrieve knowledge base evidence", InputSchema: map[string]any{"kb_id": "string", "query": "string", "mode": "string", "top_k": "number", "rerank": "boolean"}, DefaultEffects: []string{"kb.read"}},
		{Name: "memory.extract_candidates", Description: "Extract memory candidates into review queue", InputSchema: map[string]any{"conversation_id": "string", "text": "string", "project_key": "string", "queue": "boolean"}, DefaultEffects: []string{"memory.review.write"}},
		{Name: "memory.item_archive", Description: "Archive a memory item", InputSchema: map[string]any{"id": "string"}, DefaultEffects: []string{"fs.write", "memory.modify"}},
		{Name: "memory.patch", Description: "Patch markdown memory", InputSchema: map[string]any{"path": "string", "content": "string"}, DefaultEffects: []string{"fs.write", "memory.modify"}},
		{Name: "skill.run", Description: "Run a local skill", InputSchema: map[string]any{"skill_id": "string", "input": "object"}, DefaultEffects: []string{"unknown.effect"}},
		{Name: "mcp.call_tool", Description: "Call an MCP tool", InputSchema: map[string]any{"server_id": "string", "tool_name": "string", "args": "object"}, DefaultEffects: []string{"unknown.effect"}},
	}
}
