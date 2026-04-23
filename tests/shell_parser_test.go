package tests

import (
	"testing"

	"local-agent/internal/tools/shell"
)

func TestShellParser(t *testing.T) {
	t.Run("pipeline", func(t *testing.T) {
		got := shell.ParseStructure("ps aux | head -n 1")
		if !got.HasPipeline {
			t.Fatalf("expected pipeline to be detected")
		}
		if len(got.Segments) != 2 {
			t.Fatalf("segments = %d, want 2", len(got.Segments))
		}
	})

	t.Run("redirect write", func(t *testing.T) {
		got := shell.ParseStructure("echo hi > out.txt")
		if !got.HasWriteRedirect {
			t.Fatalf("expected write redirect")
		}
		if !containsString(got.RedirectTargets, "out.txt") {
			t.Fatalf("redirect targets = %v", got.RedirectTargets)
		}
	})

	t.Run("possible file target", func(t *testing.T) {
		got := shell.ParseStructure("cat .env")
		if !containsString(got.PossibleFileTargets, ".env") {
			t.Fatalf("possible file targets = %v", got.PossibleFileTargets)
		}
	})

	t.Run("output schema", func(t *testing.T) {
		got := shell.ParseStructure("ls -la")
		if got.Command != "ls -la" {
			t.Fatalf("command = %s", got.Command)
		}
		if len(got.Segments) != 1 || got.Segments[0].Name != "ls" {
			t.Fatalf("segments = %+v", got.Segments)
		}
	})
}
