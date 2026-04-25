package evals

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"local-agent/internal/agent"
	"local-agent/internal/core"
	"local-agent/internal/ids"
	"local-agent/internal/security"
)

// ReplayRun replays a workflow run in event-only or safe behavior mode.
func (m *Manager) ReplayRun(ctx context.Context, runID string, options ReplayOptions) (ReplayResult, error) {
	if options.Mode == "" {
		options.Mode = ReplayModeEvent
	}
	if !options.UseMockTools && options.Mode == ReplayModeBehavior {
		options.UseMockTools = true
	}
	if !options.RedactSecrets {
		options.RedactSecrets = true
	}
	result := ReplayResult{
		ReplayID:    ids.New("replay"),
		SourceRunID: runID,
		Mode:        options.Mode,
		Status:      EvalRunRunning,
		CreatedAt:   time.Now().UTC(),
		Options:     options,
	}
	events, _ := m.readRunEvents(runID)
	if options.RedactSecrets {
		events = redactEvents(events)
	}
	result.Events = events

	switch options.Mode {
	case ReplayModeEvent:
		result.Status = EvalRunPassed
		result.Summary = fmt.Sprintf("event replay loaded %d events", len(events))
	case ReplayModeBehavior:
		behavior, diff, err := m.behaviorReplay(ctx, runID, events, options)
		if err != nil {
			result.Status = EvalRunError
			result.Error = security.RedactString(err.Error())
			result.Summary = "behavior replay failed"
			break
		}
		result.Behavior = behavior
		result.Diff = diff
		if diff.ToolSequenceChanged || diff.ApprovalChanged || diff.RiskLevelChanged || diff.CitationChanged {
			result.Status = EvalRunFailed
			result.Summary = "behavior replay completed with differences"
		} else {
			result.Status = EvalRunPassed
			result.Summary = "behavior replay matched tracked behavior"
		}
	default:
		return ReplayResult{}, fmt.Errorf("unsupported replay mode %q", options.Mode)
	}
	if err := m.saveReplay(result); err != nil {
		return ReplayResult{}, err
	}
	return result, nil
}

// GetReplay returns one saved replay result.
func (m *Manager) GetReplay(replayID string) (ReplayResult, error) {
	var result ReplayResult
	if err := readJSONFile(filepath.Join(m.replaysRoot(), safeFileName(replayID)+".json"), &result); err != nil {
		return ReplayResult{}, err
	}
	return result, nil
}

func (m *Manager) behaviorReplay(ctx context.Context, runID string, events []core.Event, options ReplayOptions) (*EvalResult, ReplayDiff, error) {
	if m.runtime == nil {
		return nil, ReplayDiff{}, fmt.Errorf("runtime unavailable for behavior replay")
	}
	state, err := m.runtime.GetRun(ctx, runID)
	if err != nil {
		return nil, ReplayDiff{}, err
	}
	expectedTools := toolsFromEvents(events)
	expectedApprovals := approvalsFromEvents(events)
	if len(expectedTools) == 0 {
		steps, _ := m.runtime.ListRunSteps(ctx, runID)
		expectedTools = toolsFromSteps(steps)
		expectedApprovals = approvalsFromSteps(steps)
	}
	approvalRequired := len(expectedApprovals) > 0
	c := EvalCase{
		ID:       "replay-" + runID,
		Title:    "Behavior replay for " + runID,
		Category: EvalCategoryChat,
		Input:    state.UserMessage,
		Expected: EvalExpected{
			ToolSequence:     expectedTools,
			ApprovalRequired: &approvalRequired,
		},
		Replay: options,
	}
	req := EvalRunRequest{ApprovalMode: EvalApprovalRejectAllWrites, MaxSteps: 6}
	behavior := m.runCase(ctx, ids.New("replayrun"), c, req)
	diff := ReplayDiff{
		ExpectedTools:     expectedTools,
		ActualTools:       behavior.ToolCalls,
		ExpectedApprovals: expectedApprovals,
		ActualApprovals:   behavior.Approvals,
	}
	if options.CompareToolCalls {
		diff.ToolSequenceChanged = !sameStrings(expectedTools, behavior.ToolCalls)
	}
	if options.CompareApprovals {
		diff.ApprovalChanged = (len(expectedApprovals) > 0) != (len(behavior.Approvals) > 0)
	}
	return &behavior, diff, nil
}

func (m *Manager) saveReplay(result ReplayResult) error {
	if err := m.EnsureLayout(); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(m.replaysRoot(), safeFileName(result.ReplayID)+".json"), result)
}

func (m *Manager) readRunEvents(runID string) ([]core.Event, error) {
	if strings.TrimSpace(m.eventsRoot) == "" {
		return nil, fmt.Errorf("events root is not configured")
	}
	pattern := filepath.Join(m.eventsRoot, "*", "run_"+safeFileName(runID)+".jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	events := []core.Event{}
	for _, path := range matches {
		items, err := readEventsFile(path)
		if err != nil {
			return nil, err
		}
		events = append(events, items...)
	}
	return events, nil
}

func readEventsFile(path string) ([]core.Event, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	events := []core.Event{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event core.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

func redactEvents(events []core.Event) []core.Event {
	out := make([]core.Event, 0, len(events))
	for _, event := range events {
		event.Content = security.RedactString(event.Content)
		event.Payload = security.RedactMap(event.Payload)
		out = append(out, event)
	}
	return out
}

func toolsFromEvents(events []core.Event) []string {
	out := []string{}
	for _, event := range events {
		if event.Type == "tool.proposed" && event.Tool != "" {
			out = append(out, event.Tool)
		}
	}
	return out
}

func approvalsFromEvents(events []core.Event) []string {
	out := []string{}
	for _, event := range events {
		if event.Type == "approval.requested" && event.ApprovalID != "" {
			out = append(out, event.ApprovalID)
		}
	}
	return out
}

func toolsFromSteps(steps []agent.RunStep) []string {
	out := []string{}
	for _, step := range steps {
		if step.Proposal != nil && step.Proposal.Tool != "" {
			out = append(out, step.Proposal.Tool)
		}
	}
	return out
}

func approvalsFromSteps(steps []agent.RunStep) []string {
	out := []string{}
	for _, step := range steps {
		if step.Approval != nil && step.Approval.ID != "" {
			out = append(out, step.Approval.ID)
		}
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for idx := range a {
		if a[idx] != b[idx] {
			return false
		}
	}
	return true
}
