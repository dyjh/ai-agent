package code

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"local-agent/internal/core"
)

// ValidatePatchExecutor validates a patch without writing files.
type ValidatePatchExecutor struct {
	Workspace Workspace
}

// DryRunPatchExecutor computes patch effects without writing files.
type DryRunPatchExecutor struct {
	Workspace Workspace
}

// Execute implements code.validate_patch.
func (e *ValidatePatchExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	result, err := e.validate(input, false)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{Output: result}, nil
}

// Execute implements code.dry_run_patch.
func (e *DryRunPatchExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	result, err := (&ValidatePatchExecutor{Workspace: e.Workspace}).validate(input, true)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{Output: result}, nil
}

func (e *ValidatePatchExecutor) validate(input map[string]any, includeDryRun bool) (map[string]any, error) {
	changedFiles := []string{}
	conflicts := []string{}
	sensitiveFiles := []string{}
	dryRunFiles := []map[string]any{}

	if diff, _ := input["diff"].(string); strings.TrimSpace(diff) != "" {
		patches, err := parseUnifiedDiff(diff)
		if err != nil {
			return nil, err
		}
		for _, patch := range patches {
			abs, rel, err := e.Workspace.resolve(patch.Path)
			if err != nil {
				return nil, err
			}
			current, _, _, err := readPatchTarget(abs)
			if err != nil {
				return nil, err
			}
			newContent, err := applyUnifiedPatchToContent(string(current), patch)
			if err != nil {
				conflicts = append(conflicts, fmt.Sprintf("%s: %v", rel, err))
			}
			changedFiles = append(changedFiles, rel)
			if e.Workspace.isSensitive(rel) {
				sensitiveFiles = append(sensitiveFiles, rel)
			}
			if includeDryRun && err == nil {
				dryRunFiles = append(dryRunFiles, map[string]any{
					"path":       rel,
					"old_sha256": sha256Hex(current),
					"new_sha256": sha256Hex([]byte(newContent)),
					"old_size":   len(current),
					"new_size":   len(newContent),
				})
			}
		}
		return patchValidationOutput(changedFiles, conflicts, sensitiveFiles, dryRunFiles, includeDryRun), nil
	}

	files, err := parsePatchFiles(input)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		abs, rel, err := e.Workspace.resolve(file.Path)
		if err != nil {
			return nil, err
		}
		current, _, _, err := readPatchTarget(abs)
		if err != nil {
			return nil, err
		}
		expected := file.ExpectedSHA256
		if expected == "" {
			expected, _ = input["expected_sha256"].(string)
		}
		if expected != "" && expected != sha256Hex(current) {
			conflicts = append(conflicts, fmt.Sprintf("%s: expected_sha256 mismatch", rel))
		}
		changedFiles = append(changedFiles, rel)
		if e.Workspace.isSensitive(rel) {
			sensitiveFiles = append(sensitiveFiles, rel)
		}
		if includeDryRun {
			dryRunFiles = append(dryRunFiles, map[string]any{
				"path":       rel,
				"old_sha256": sha256Hex(current),
				"new_sha256": sha256Hex([]byte(file.Content)),
				"old_size":   len(current),
				"new_size":   len(file.Content),
			})
		}
	}
	return patchValidationOutput(changedFiles, conflicts, sensitiveFiles, dryRunFiles, includeDryRun), nil
}

func patchValidationOutput(changedFiles, conflicts, sensitiveFiles []string, dryRunFiles []map[string]any, includeDryRun bool) map[string]any {
	valid := len(conflicts) == 0
	out := map[string]any{
		"valid":             valid,
		"changed_files":     uniqueStrings(changedFiles),
		"conflicts":         conflicts,
		"sensitive_files":   uniqueStrings(sensitiveFiles),
		"requires_approval": true,
		"summary":           patchValidationSummary(valid, changedFiles, conflicts, sensitiveFiles),
	}
	if includeDryRun {
		out["dry_run"] = true
		out["files"] = dryRunFiles
	}
	return out
}

func patchValidationSummary(valid bool, changedFiles, conflicts, sensitiveFiles []string) string {
	if !valid {
		return fmt.Sprintf("patch has %d conflict(s)", len(conflicts))
	}
	if len(sensitiveFiles) > 0 {
		return fmt.Sprintf("patch is valid and touches %d sensitive file(s)", len(sensitiveFiles))
	}
	return fmt.Sprintf("patch is valid for %d file(s)", len(uniqueStrings(changedFiles)))
}

type unifiedFilePatch struct {
	Path  string
	Hunks []unifiedHunk
}

type unifiedHunk struct {
	Lines []unifiedLine
}

type unifiedLine struct {
	Kind byte
	Text string
}

func parseUnifiedDiff(diff string) ([]unifiedFilePatch, error) {
	lines := strings.Split(diff, "\n")
	var patches []unifiedFilePatch
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "--- ") {
			continue
		}
		if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "+++ ") {
			return nil, errors.New("unified diff missing +++ header")
		}
		path := normalizeDiffPath(strings.TrimSpace(strings.TrimPrefix(lines[i+1], "+++ ")))
		if path == "" || path == "/dev/null" {
			path = normalizeDiffPath(strings.TrimSpace(strings.TrimPrefix(lines[i], "--- ")))
		}
		if path == "" || path == "/dev/null" {
			return nil, errors.New("unified diff file path is required")
		}
		patch := unifiedFilePatch{Path: path}
		i += 2
		for i < len(lines) {
			if strings.HasPrefix(lines[i], "--- ") {
				i--
				break
			}
			if !strings.HasPrefix(lines[i], "@@") {
				i++
				continue
			}
			hunk := unifiedHunk{}
			i++
			for i < len(lines) && !strings.HasPrefix(lines[i], "@@") && !strings.HasPrefix(lines[i], "--- ") {
				line := lines[i]
				if line == "" && i == len(lines)-1 {
					break
				}
				if line == `\ No newline at end of file` {
					i++
					continue
				}
				kind := line[0]
				if kind != ' ' && kind != '+' && kind != '-' {
					return nil, fmt.Errorf("invalid unified diff line prefix %q", string(kind))
				}
				hunk.Lines = append(hunk.Lines, unifiedLine{Kind: kind, Text: line[1:]})
				i++
			}
			patch.Hunks = append(patch.Hunks, hunk)
			i--
		}
		if len(patch.Hunks) == 0 {
			return nil, fmt.Errorf("unified diff for %s has no hunks", path)
		}
		patches = append(patches, patch)
	}
	if len(patches) == 0 {
		return nil, errors.New("no unified diff file headers found")
	}
	return patches, nil
}

func normalizeDiffPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "a/")
	path = strings.TrimPrefix(path, "b/")
	if idx := strings.IndexAny(path, "\t "); idx >= 0 {
		path = path[:idx]
	}
	return filepath.ToSlash(path)
}

func applyUnifiedPatchToContent(current string, patch unifiedFilePatch) (string, error) {
	lines := splitLines(current)
	cursor := 0
	for _, hunk := range patch.Hunks {
		oldSeq := hunkOldLines(hunk)
		newSeq := hunkNewLines(hunk)
		pos := findLineSequence(lines, oldSeq, cursor)
		if pos < 0 {
			return "", errors.New("hunk context does not match current file")
		}
		replaced := append([]string{}, lines[:pos]...)
		replaced = append(replaced, newSeq...)
		replaced = append(replaced, lines[pos+len(oldSeq):]...)
		lines = replaced
		cursor = pos + len(newSeq)
	}
	if len(lines) == 0 {
		return "", nil
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func hunkOldLines(hunk unifiedHunk) []string {
	var out []string
	for _, line := range hunk.Lines {
		if line.Kind == ' ' || line.Kind == '-' {
			out = append(out, line.Text)
		}
	}
	return out
}

func hunkNewLines(hunk unifiedHunk) []string {
	var out []string
	for _, line := range hunk.Lines {
		if line.Kind == ' ' || line.Kind == '+' {
			out = append(out, line.Text)
		}
	}
	return out
}

func findLineSequence(lines, seq []string, start int) int {
	if len(seq) == 0 {
		if start > len(lines) {
			return len(lines)
		}
		return start
	}
	for i := start; i+len(seq) <= len(lines); i++ {
		matched := true
		for j := range seq {
			if lines[i+j] != seq[j] {
				matched = false
				break
			}
		}
		if matched {
			return i
		}
	}
	return -1
}

func prepareUnifiedDiffPatch(workspace Workspace, diff string) ([]preparedPatchFile, error) {
	patches, err := parseUnifiedDiff(diff)
	if err != nil {
		return nil, err
	}
	prepared := make([]preparedPatchFile, 0, len(patches))
	for _, patch := range patches {
		abs, rel, err := workspace.resolve(patch.Path)
		if err != nil {
			return nil, err
		}
		current, exists, mode, err := readPatchTarget(abs)
		if err != nil {
			return nil, err
		}
		newContent, err := applyUnifiedPatchToContent(string(current), patch)
		if err != nil {
			return nil, fmt.Errorf("patch conflict for %s: %w", rel, err)
		}
		if !exists {
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return nil, err
			}
		}
		prepared = append(prepared, preparedPatchFile{
			Path:          rel,
			AbsPath:       abs,
			OldContent:    current,
			NewContent:    []byte(newContent),
			Mode:          mode,
			ExistedBefore: exists,
		})
	}
	return prepared, nil
}
