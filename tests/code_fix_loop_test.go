package tests

import (
	"context"
	"testing"

	"local-agent/internal/tools/code"
)

func TestCodeFixTestFailureLoopStopsForPatchProposal(t *testing.T) {
	root := newGoTestProject(t, `func TestFail(t *testing.T) {
	t.Fatalf("broken")
}
`)
	result, err := (&code.FixTestFailureLoopExecutor{
		Workspace:             code.Workspace{Root: root},
		DefaultTimeoutSeconds: 10,
		MaxOutputBytes:        20000,
		MaxIterations:         3,
	}).Execute(context.Background(), map[string]any{
		"workspace":        ".",
		"max_iterations":   10,
		"stop_on_approval": true,
	})
	if err != nil {
		t.Fatalf("FixTestFailureLoopExecutor.Execute() error = %v", err)
	}
	if result.Output["final_passed"] != false {
		t.Fatalf("final_passed = %v, want false", result.Output["final_passed"])
	}
	if result.Output["stopped_reason"] != "waiting_for_patch_proposal" {
		t.Fatalf("stopped_reason = %v", result.Output["stopped_reason"])
	}
	if result.Output["next_tool"] != "code.propose_patch" {
		t.Fatalf("next_tool = %v, want code.propose_patch", result.Output["next_tool"])
	}
	if _, ok := result.Output["next_proposal"].(map[string]any); !ok {
		t.Fatalf("next_proposal = %#v, want object", result.Output["next_proposal"])
	}
	if result.Output["max_iterations"] != 3 {
		t.Fatalf("max_iterations = %v, want clamp to 3", result.Output["max_iterations"])
	}
	if runs, ok := result.Output["test_runs"].([]map[string]any); !ok || len(runs) != 1 {
		t.Fatalf("test_runs = %#v, want one run", result.Output["test_runs"])
	}
}

func TestCodeFixTestFailureLoopPassedShape(t *testing.T) {
	root := newGoTestProject(t, `func TestPass(t *testing.T) {
}
`)
	result, err := (&code.FixTestFailureLoopExecutor{
		Workspace:             code.Workspace{Root: root},
		DefaultTimeoutSeconds: 10,
		MaxOutputBytes:        20000,
		MaxIterations:         3,
	}).Execute(context.Background(), map[string]any{
		"workspace":      ".",
		"max_iterations": 3,
	})
	if err != nil {
		t.Fatalf("FixTestFailureLoopExecutor.Execute() error = %v", err)
	}
	if result.Output["final_passed"] != true {
		t.Fatalf("final_passed = %v, want true", result.Output["final_passed"])
	}
	if result.Output["stopped_reason"] != "tests_passed" {
		t.Fatalf("stopped_reason = %v, want tests_passed", result.Output["stopped_reason"])
	}
}
