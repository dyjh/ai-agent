package code

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"local-agent/internal/core"
)

var hunkHeaderRE = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$`)

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
	fileSummaries := []map[string]any{}
	dryRunFiles := []map[string]any{}
	stats := patchStats{}

	if diff, _ := input["diff"].(string); strings.TrimSpace(diff) != "" {
		patches, err := parseUnifiedDiff(diff)
		if err != nil {
			return nil, err
		}
		for idx, patch := range patches {
			abs, rel, err := e.Workspace.resolve(patch.Path)
			if err != nil {
				return nil, err
			}
			current, exists, _, err := readPatchTarget(abs)
			if err != nil {
				return nil, err
			}
			expected := expectedHashForPatch(input, rel, idx, len(patches))
			if expected != "" && expected != sha256Hex(current) {
				conflicts = append(conflicts, fmt.Sprintf("%s: expected_sha256 mismatch", rel))
			}

			newContent, apply, err := applyUnifiedPatchToContentDetailed(string(current), patch)
			if err != nil {
				conflicts = append(conflicts, fmt.Sprintf("%s: %v", rel, err))
			}
			changedFiles = append(changedFiles, rel)
			if e.Workspace.isSensitive(rel) {
				sensitiveFiles = append(sensitiveFiles, rel)
			}
			fileStats := patch.fileStats()
			stats.add(fileStats)
			summary := map[string]any{
				"path":           rel,
				"old_path":       patch.OldPath,
				"new_path":       patch.NewPath,
				"operation":      patch.Operation,
				"hunks":          len(patch.Hunks),
				"additions":      fileStats.Additions,
				"deletions":      fileStats.Deletions,
				"existed_before": exists,
			}
			if len(apply.Hunks) > 0 {
				summary["hunk_matches"] = apply.Hunks
			}
			fileSummaries = append(fileSummaries, summary)
			if includeDryRun && err == nil && expectedHashMatches(expected, current) {
				dryRunFiles = append(dryRunFiles, map[string]any{
					"path":              rel,
					"old_sha256":        sha256Hex(current),
					"new_sha256":        sha256Hex([]byte(newContent)),
					"old_size":          len(current),
					"new_size":          len(newContent),
					"rollback_snapshot": rollbackSnapshotForFile(rel, current, nil, exists, 0),
				})
			}
		}
		stats.FileCount = len(uniqueStrings(changedFiles))
		return patchValidationOutput(changedFiles, conflicts, sensitiveFiles, fileSummaries, dryRunFiles, stats, includeDryRun), nil
	}

	files, err := parsePatchFiles(input)
	if err != nil {
		return nil, err
	}
	for idx, file := range files {
		abs, rel, err := e.Workspace.resolve(file.Path)
		if err != nil {
			return nil, err
		}
		current, exists, _, err := readPatchTarget(abs)
		if err != nil {
			return nil, err
		}
		expected := file.ExpectedSHA256
		if expected == "" {
			expected = expectedHashForPatch(input, rel, idx, len(files))
		}
		if expected != "" && expected != sha256Hex(current) {
			conflicts = append(conflicts, fmt.Sprintf("%s: expected_sha256 mismatch", rel))
		}
		changedFiles = append(changedFiles, rel)
		if e.Workspace.isSensitive(rel) {
			sensitiveFiles = append(sensitiveFiles, rel)
		}
		added, removed := contentLineDelta(string(current), file.Content)
		stats.Additions += added
		stats.Deletions += removed
		fileSummaries = append(fileSummaries, map[string]any{
			"path":           rel,
			"operation":      "replace",
			"additions":      added,
			"deletions":      removed,
			"existed_before": exists,
		})
		if includeDryRun {
			dryRunFiles = append(dryRunFiles, map[string]any{
				"path":              rel,
				"old_sha256":        sha256Hex(current),
				"new_sha256":        sha256Hex([]byte(file.Content)),
				"old_size":          len(current),
				"new_size":          len(file.Content),
				"rollback_snapshot": rollbackSnapshotForFile(rel, current, nil, exists, 0),
			})
		}
	}
	stats.FileCount = len(uniqueStrings(changedFiles))
	return patchValidationOutput(changedFiles, conflicts, sensitiveFiles, fileSummaries, dryRunFiles, stats, includeDryRun), nil
}

func patchValidationOutput(changedFiles, conflicts, sensitiveFiles []string, fileSummaries, dryRunFiles []map[string]any, stats patchStats, includeDryRun bool) map[string]any {
	valid := len(conflicts) == 0
	out := map[string]any{
		"valid":             valid,
		"changed_files":     uniqueStrings(changedFiles),
		"conflicts":         conflicts,
		"sensitive_files":   uniqueStrings(sensitiveFiles),
		"requires_approval": true,
		"summary":           patchValidationSummary(valid, changedFiles, conflicts, sensitiveFiles, stats),
		"file_summaries":    fileSummaries,
		"statistics":        stats.toMap(),
	}
	if includeDryRun {
		out["dry_run"] = true
		out["files"] = dryRunFiles
	}
	return out
}

func patchValidationSummary(valid bool, changedFiles, conflicts, sensitiveFiles []string, stats patchStats) string {
	if !valid {
		return fmt.Sprintf("patch has %d conflict(s)", len(conflicts))
	}
	base := fmt.Sprintf("patch is valid for %d file(s), +%d/-%d", len(uniqueStrings(changedFiles)), stats.Additions, stats.Deletions)
	if len(sensitiveFiles) > 0 {
		return base + fmt.Sprintf(" and touches %d sensitive file(s)", len(uniqueStrings(sensitiveFiles)))
	}
	return base
}

type unifiedFilePatch struct {
	OldPath   string
	NewPath   string
	Path      string
	Operation string
	Hunks     []unifiedHunk
}

type unifiedHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Section  string
	Lines    []unifiedLine
}

type unifiedLine struct {
	Kind byte
	Text string
}

type patchStats struct {
	FileCount int `json:"file_count"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

func (s *patchStats) add(other patchStats) {
	s.Additions += other.Additions
	s.Deletions += other.Deletions
}

func (s patchStats) toMap() map[string]any {
	return map[string]any{
		"file_count": s.FileCount,
		"additions":  s.Additions,
		"deletions":  s.Deletions,
	}
}

type hunkApplyInfo struct {
	OldStart     int  `json:"old_start"`
	NewStart     int  `json:"new_start"`
	MatchedLine  int  `json:"matched_line"`
	Offset       int  `json:"offset"`
	PartialMatch bool `json:"partial_match"`
}

type applyInfo struct {
	Hunks []hunkApplyInfo `json:"hunks"`
}

// ParseUnifiedDiff parses a common unified diff into a serializable structure.
func ParseUnifiedDiff(diff string) (UnifiedDiff, error) {
	patches, err := parseUnifiedDiff(diff)
	if err != nil {
		return UnifiedDiff{}, err
	}
	out := UnifiedDiff{Files: make([]DiffFile, 0, len(patches))}
	for _, patch := range patches {
		file := DiffFile{
			OldPath:   patch.OldPath,
			NewPath:   patch.NewPath,
			Path:      patch.Path,
			Operation: patch.Operation,
			Hunks:     make([]DiffHunk, 0, len(patch.Hunks)),
		}
		for _, hunk := range patch.Hunks {
			item := DiffHunk{
				OldStart: hunk.OldStart,
				OldCount: hunk.OldCount,
				NewStart: hunk.NewStart,
				NewCount: hunk.NewCount,
				Section:  hunk.Section,
				Lines:    make([]DiffLine, 0, len(hunk.Lines)),
			}
			for _, line := range hunk.Lines {
				item.Lines = append(item.Lines, DiffLine{Kind: diffLineKind(line.Kind), Text: line.Text})
			}
			file.Hunks = append(file.Hunks, item)
		}
		out.Files = append(out.Files, file)
	}
	return out, nil
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
		oldPath := normalizeDiffPath(strings.TrimSpace(strings.TrimPrefix(lines[i], "--- ")))
		newPath := normalizeDiffPath(strings.TrimSpace(strings.TrimPrefix(lines[i+1], "+++ ")))
		path := newPath
		if path == "" || isDevNullPath(path) {
			path = oldPath
		}
		if path == "" || isDevNullPath(path) {
			return nil, errors.New("unified diff file path is required")
		}
		patch := unifiedFilePatch{
			OldPath:   oldPath,
			NewPath:   newPath,
			Path:      path,
			Operation: diffOperation(oldPath, newPath),
		}
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
			hunk, err := parseHunkHeader(lines[i])
			if err != nil {
				return nil, err
			}
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
			if err := validateHunkCounts(hunk); err != nil {
				return nil, err
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

func parseHunkHeader(line string) (unifiedHunk, error) {
	match := hunkHeaderRE.FindStringSubmatch(line)
	if match == nil {
		return unifiedHunk{}, fmt.Errorf("invalid hunk header: %s", line)
	}
	oldStart, _ := strconv.Atoi(match[1])
	oldCount := parseHunkCount(match[2])
	newStart, _ := strconv.Atoi(match[3])
	newCount := parseHunkCount(match[4])
	return unifiedHunk{
		OldStart: oldStart,
		OldCount: oldCount,
		NewStart: newStart,
		NewCount: newCount,
		Section:  strings.TrimSpace(match[5]),
	}, nil
}

func parseHunkCount(value string) int {
	if value == "" {
		return 1
	}
	n, _ := strconv.Atoi(value)
	return n
}

func validateHunkCounts(hunk unifiedHunk) error {
	oldCount := 0
	newCount := 0
	for _, line := range hunk.Lines {
		if line.Kind == ' ' || line.Kind == '-' {
			oldCount++
		}
		if line.Kind == ' ' || line.Kind == '+' {
			newCount++
		}
	}
	if oldCount != hunk.OldCount {
		return fmt.Errorf("hunk old line count mismatch at -%d: header=%d actual=%d", hunk.OldStart, hunk.OldCount, oldCount)
	}
	if newCount != hunk.NewCount {
		return fmt.Errorf("hunk new line count mismatch at +%d: header=%d actual=%d", hunk.NewStart, hunk.NewCount, newCount)
	}
	return nil
}

func normalizeDiffPath(path string) string {
	path = strings.TrimSpace(path)
	if idx := strings.IndexAny(path, "\t "); idx >= 0 {
		path = path[:idx]
	}
	if path == "/dev/null" {
		return path
	}
	path = strings.TrimPrefix(path, "a/")
	path = strings.TrimPrefix(path, "b/")
	return filepath.ToSlash(path)
}

func isDevNullPath(path string) bool {
	return path == "/dev/null"
}

func diffOperation(oldPath, newPath string) string {
	switch {
	case isDevNullPath(oldPath):
		return "create"
	case isDevNullPath(newPath):
		return "delete"
	case oldPath != "" && newPath != "" && oldPath != newPath:
		return "rename_or_modify"
	default:
		return "modify"
	}
}

func applyUnifiedPatchToContent(current string, patch unifiedFilePatch) (string, error) {
	out, _, err := applyUnifiedPatchToContentDetailed(current, patch)
	return out, err
}

func applyUnifiedPatchToContentDetailed(current string, patch unifiedFilePatch) (string, applyInfo, error) {
	lines := splitLines(current)
	cursor := 0
	lineOffset := 0
	info := applyInfo{}
	for _, hunk := range patch.Hunks {
		oldSeq := hunkOldLines(hunk)
		newSeq := hunkNewLines(hunk)
		hint := hunk.OldStart - 1 + lineOffset
		pos := findHunkPosition(lines, oldSeq, cursor, hint)
		if pos < 0 {
			return "", info, fmt.Errorf("hunk at -%d +%d context does not match current file", hunk.OldStart, hunk.NewStart)
		}
		replaced := append([]string{}, lines[:pos]...)
		replaced = append(replaced, newSeq...)
		replaced = append(replaced, lines[pos+len(oldSeq):]...)
		lines = replaced
		cursor = pos + len(newSeq)
		lineOffset += len(newSeq) - len(oldSeq)
		expectedLine := hunk.OldStart
		if expectedLine <= 0 {
			expectedLine = 1
		}
		matchedLine := pos + 1
		info.Hunks = append(info.Hunks, hunkApplyInfo{
			OldStart:     hunk.OldStart,
			NewStart:     hunk.NewStart,
			MatchedLine:  matchedLine,
			Offset:       matchedLine - expectedLine,
			PartialMatch: matchedLine != expectedLine,
		})
	}
	if len(lines) == 0 {
		return "", info, nil
	}
	return strings.Join(lines, "\n") + "\n", info, nil
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

func findHunkPosition(lines, seq []string, cursor, hint int) int {
	if len(seq) == 0 {
		if hint < cursor {
			hint = cursor
		}
		if hint < 0 {
			return 0
		}
		if hint > len(lines) {
			return len(lines)
		}
		return hint
	}
	if hint >= cursor && sequenceMatches(lines, seq, hint) {
		return hint
	}
	return findLineSequence(lines, seq, cursor)
}

func sequenceMatches(lines, seq []string, pos int) bool {
	if pos < 0 || pos+len(seq) > len(lines) {
		return false
	}
	for idx := range seq {
		if lines[pos+idx] != seq[idx] {
			return false
		}
	}
	return true
}

func findLineSequence(lines, seq []string, start int) int {
	if len(seq) == 0 {
		if start > len(lines) {
			return len(lines)
		}
		return start
	}
	for i := start; i+len(seq) <= len(lines); i++ {
		if sequenceMatches(lines, seq, i) {
			return i
		}
	}
	return -1
}

func prepareUnifiedDiffPatch(workspace Workspace, input map[string]any, diff string) ([]preparedPatchFile, error) {
	patches, err := parseUnifiedDiff(diff)
	if err != nil {
		return nil, err
	}
	prepared := make([]preparedPatchFile, 0, len(patches))
	for idx, patch := range patches {
		abs, rel, err := workspace.resolve(patch.Path)
		if err != nil {
			return nil, err
		}
		current, exists, mode, err := readPatchTarget(abs)
		if err != nil {
			return nil, err
		}
		expected := expectedHashForPatch(input, rel, idx, len(patches))
		if expected != "" && expected != sha256Hex(current) {
			return nil, fmt.Errorf("patch base hash mismatch for %s", rel)
		}
		newContent, _, err := applyUnifiedPatchToContentDetailed(string(current), patch)
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
			Delete:        patch.Operation == "delete",
		})
	}
	return prepared, nil
}

func (p unifiedFilePatch) fileStats() patchStats {
	stats := patchStats{FileCount: 1}
	for _, hunk := range p.Hunks {
		for _, line := range hunk.Lines {
			switch line.Kind {
			case '+':
				stats.Additions++
			case '-':
				stats.Deletions++
			}
		}
	}
	return stats
}

func expectedHashForPatch(input map[string]any, rel string, index, total int) string {
	if raw, ok := input["expected_sha256_by_path"]; ok {
		if values, ok := raw.(map[string]any); ok {
			if value, _ := values[rel].(string); value != "" {
				return value
			}
		}
		if values, ok := raw.(map[string]string); ok {
			if value := values[rel]; value != "" {
				return value
			}
		}
	}
	if raw, ok := input["expected_sha256"]; ok && total == 1 {
		if value, _ := raw.(string); value != "" {
			return value
		}
	}
	if raw, ok := input["expected_sha256"]; ok && total > 1 {
		if values, ok := raw.([]any); ok && index < len(values) {
			value, _ := values[index].(string)
			return value
		}
		if values, ok := raw.([]string); ok && index < len(values) {
			return values[index]
		}
	}
	return ""
}

func expectedHashMatches(expected string, current []byte) bool {
	return expected == "" || expected == sha256Hex(current)
}

func diffLineKind(kind byte) string {
	switch kind {
	case '+':
		return "add"
	case '-':
		return "delete"
	default:
		return "context"
	}
}

func contentLineDelta(oldContent, newContent string) (int, int) {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)
	return len(newLines), len(oldLines)
}
