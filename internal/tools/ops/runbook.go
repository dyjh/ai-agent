package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/tools"
)

// RunbookListExecutor implements runbook.list.
type RunbookListExecutor struct {
	Manager *Manager
}

// RunbookReadExecutor implements runbook.read.
type RunbookReadExecutor struct {
	Manager *Manager
}

// RunbookPlanExecutor implements runbook.plan.
type RunbookPlanExecutor struct {
	Manager *Manager
}

// RunbookExecuteStepExecutor implements runbook.execute_step.
type RunbookExecuteStepExecutor struct {
	Manager *Manager
}

// RunbookExecuteExecutor implements runbook.execute.
type RunbookExecuteExecutor struct {
	Manager *Manager
}

// ListRunbooks returns parsed runbook summaries.
func (m *Manager) ListRunbooks() ([]Runbook, error) {
	entries, err := os.ReadDir(m.runbookDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	items := make([]Runbook, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		item, err := m.ReadRunbook(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		if err != nil {
			continue
		}
		item.Body = ""
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	return items, nil
}

// ReadRunbook reads and parses one Markdown runbook.
func (m *Manager) ReadRunbook(id string) (Runbook, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Runbook{}, fmt.Errorf("runbook id is required")
	}
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return Runbook{}, fmt.Errorf("invalid runbook id: %s", id)
	}
	path := filepath.Join(m.runbookDir, id+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return Runbook{}, err
	}
	item := parseRunbook(id, path, string(data))
	return item, nil
}

// PlanRunbook returns a dry-run execution plan.
func (m *Manager) PlanRunbook(id, hostID string, dryRun bool) (RunbookPlan, error) {
	runbook, err := m.ReadRunbook(id)
	if err != nil {
		return RunbookPlan{}, err
	}
	plan := RunbookPlan{
		RunbookID: runbook.ID,
		Title:     runbook.Title,
		HostID:    hostID,
		DryRun:    dryRun,
		Steps:     make([]RunbookPlanStep, 0, len(runbook.StepTexts)),
	}
	for idx, text := range runbook.StepTexts {
		step := planRunbookStep(idx+1, text, hostID)
		plan.Steps = append(plan.Steps, step)
	}
	return plan, nil
}

// ExecuteRunbookStep routes one planned step through ToolRouter.
func (m *Manager) ExecuteRunbookStep(ctx context.Context, step RunbookPlanStep, runID, conversationID string) (*core.ToolResult, error) {
	if step.Tool == "" {
		return &core.ToolResult{
			Output: map[string]any{
				"status": "skipped",
				"step":   step,
				"reason": "runbook step has no executable tool",
			},
			StartedAt:  time.Now().UTC(),
			FinishedAt: time.Now().UTC(),
		}, nil
	}

	m.mu.RLock()
	router := m.router
	m.mu.RUnlock()
	if router == nil {
		return nil, fmt.Errorf("tool router is not available for runbook execution")
	}
	proposal := newProposal(step.Tool, step.Input, "执行 runbook step: "+step.Text, nil)
	route, err := router.Propose(ctx, runID, conversationID, proposal)
	if err != nil {
		return nil, err
	}
	output := map[string]any{
		"status":    "routed",
		"step":      step,
		"proposal":  route.Proposal,
		"inference": route.Inference,
		"decision":  route.Decision,
	}
	if route.Approval != nil {
		output["approval"] = route.Approval
	}
	if route.Result != nil {
		output["result"] = route.Result
	}
	return &core.ToolResult{
		Output:     output,
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
	}, nil
}

// Execute implements runbook.list.
func (e *RunbookListExecutor) Execute(_ context.Context, _ map[string]any) (*core.ToolResult, error) {
	items, err := e.Manager.ListRunbooks()
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{Output: map[string]any{"items": items}}, nil
}

// Execute implements runbook.read.
func (e *RunbookReadExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	id, err := tools.GetString(input, "id")
	if err != nil {
		id, err = tools.GetString(input, "runbook_id")
	}
	if err != nil {
		return nil, err
	}
	item, err := e.Manager.ReadRunbook(id)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{Output: map[string]any{"runbook": item}}, nil
}

// Execute implements runbook.plan.
func (e *RunbookPlanExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	id, err := runbookID(input)
	if err != nil {
		return nil, err
	}
	hostID, _ := input["host_id"].(string)
	plan, err := e.Manager.PlanRunbook(id, hostID, true)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{Output: map[string]any{"plan": plan}}, nil
}

// Execute implements runbook.execute_step.
func (e *RunbookExecuteStepExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	step, err := stepFromInput(input)
	if err != nil {
		return nil, err
	}
	runID, _ := input["run_id"].(string)
	conversationID, _ := input["conversation_id"].(string)
	return e.Manager.ExecuteRunbookStep(ctx, step, runID, conversationID)
}

// Execute implements runbook.execute.
func (e *RunbookExecuteExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	id, err := runbookID(input)
	if err != nil {
		return nil, err
	}
	hostID, _ := input["host_id"].(string)
	dryRun, _ := input["dry_run"].(bool)
	maxSteps := tools.GetInt(input, "max_steps", 5)
	if maxSteps <= 0 {
		maxSteps = 5
	}
	plan, err := e.Manager.PlanRunbook(id, hostID, dryRun)
	if err != nil {
		return nil, err
	}
	if dryRun {
		return &core.ToolResult{Output: map[string]any{"status": "dry_run", "plan": plan}}, nil
	}
	results := make([]map[string]any, 0, maxSteps)
	for idx, step := range plan.Steps {
		if idx >= maxSteps {
			break
		}
		result, err := e.Manager.ExecuteRunbookStep(ctx, step, "", "")
		if err != nil {
			return nil, err
		}
		entry := map[string]any{"step": step, "result": result}
		results = append(results, entry)
		if result.Output["approval"] != nil {
			return &core.ToolResult{
				Output: map[string]any{
					"status":  "paused_for_approval",
					"plan":    plan,
					"results": results,
				},
			}, nil
		}
	}
	return &core.ToolResult{
		Output: map[string]any{
			"status":  "completed",
			"plan":    plan,
			"results": results,
		},
	}, nil
}

func parseRunbook(id, path, content string) Runbook {
	metadata := map[string]string{}
	body := content
	if strings.HasPrefix(content, "---\n") {
		rest := strings.TrimPrefix(content, "---\n")
		if end := strings.Index(rest, "\n---"); end >= 0 {
			fm := rest[:end]
			body = strings.TrimLeft(rest[end+len("\n---"):], "\r\n")
			for _, line := range strings.Split(fm, "\n") {
				key, value, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				metadata[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
			}
		}
	}
	if metaID := metadata["id"]; metaID != "" {
		id = metaID
	}
	title := metadata["title"]
	if title == "" {
		title = firstMarkdownTitle(body)
	}
	if title == "" {
		title = id
	}
	return Runbook{
		ID:         id,
		Title:      title,
		Scope:      metadata["scope"],
		HostType:   metadata["host_type"],
		Metadata:   metadata,
		Body:       body,
		SourcePath: filepath.Base(path),
		StepTexts:  extractRunbookSteps(body),
		LoadedAt:   time.Now().UTC(),
	}
}

func firstMarkdownTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func extractRunbookSteps(body string) []string {
	var steps []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, ". "); idx > 0 {
			if _, err := strconv.Atoi(line[:idx]); err == nil {
				steps = append(steps, strings.TrimSpace(line[idx+2:]))
			}
			continue
		}
		if strings.HasPrefix(line, "- ") {
			steps = append(steps, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
		}
	}
	return steps
}

func planRunbookStep(index int, text, hostID string) RunbookPlanStep {
	normalized := strings.ToLower(text)
	input := map[string]any{}
	if hostID != "" {
		input["host_id"] = hostID
	}
	step := RunbookPlanStep{Index: index, Text: text, Input: input}
	switch {
	case strings.Contains(normalized, "cpu") || strings.Contains(normalized, "process"):
		step.Tool = "ops.local.processes"
	case strings.Contains(normalized, "memory"):
		step.Tool = "ops.local.memory_usage"
	case strings.Contains(normalized, "disk"):
		step.Tool = "ops.local.disk_usage"
	case strings.Contains(normalized, "network"):
		step.Tool = "ops.local.network_info"
	case strings.Contains(normalized, "system"):
		step.Tool = "ops.local.system_info"
	case strings.Contains(normalized, "restart") && strings.Contains(normalized, "service"):
		step.Tool = "ops.local.service_restart"
		step.Input["service"] = "unknown"
		step.RequiresApproval = true
	default:
		step.Reason = "no deterministic ops tool mapping for this runbook step"
	}
	return step
}

func runbookID(input map[string]any) (string, error) {
	id, err := tools.GetString(input, "id")
	if err != nil {
		id, err = tools.GetString(input, "runbook_id")
	}
	return id, err
}

func stepFromInput(input map[string]any) (RunbookPlanStep, error) {
	if raw, ok := input["step"].(map[string]any); ok {
		tool, _ := raw["tool"].(string)
		text, _ := raw["text"].(string)
		stepInput, _ := raw["input"].(map[string]any)
		return RunbookPlanStep{
			Index:            tools.GetInt(raw, "index", 0),
			Text:             text,
			Tool:             tool,
			Input:            core.CloneMap(stepInput),
			RequiresApproval: boolFromAny(raw["requires_approval"]),
			Reason:           fmt.Sprint(raw["reason"]),
		}, nil
	}
	tool, err := tools.GetString(input, "tool")
	if err != nil {
		return RunbookPlanStep{}, err
	}
	stepInput, _ := input["input"].(map[string]any)
	text, _ := input["text"].(string)
	return RunbookPlanStep{
		Index: tools.GetInt(input, "index", 0),
		Text:  text,
		Tool:  tool,
		Input: core.CloneMap(stepInput),
	}, nil
}

func boolFromAny(value any) bool {
	result, _ := value.(bool)
	return result
}
