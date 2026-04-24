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
	if result.Output["next_tool"] != "code.propose_patch" {
		t.Fatalf("next_tool = %v, want code.propose_patch", result.Output["next_tool"])
	}
	if result.Output["max_iterations"] != 3 {
		t.Fatalf("max_iterations = %v, want clamp to 3", result.Output["max_iterations"])
	}
}
