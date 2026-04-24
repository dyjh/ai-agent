package code

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"local-agent/internal/core"
	"local-agent/internal/tools"
)

// ProposePatchExecutor previews a patch without writing it.
type ProposePatchExecutor struct {
	Workspace Workspace
}

// ApplyPatchExecutor applies a file replacement inside the workspace.
type ApplyPatchExecutor struct {
	Workspace Workspace
}

// Execute implements code.propose_patch.
func (e *ProposePatchExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	files, err := parsePatchFiles(input)
	if err != nil {
		return nil, err
	}
	var changedFiles []string
	var fileOutputs []map[string]any
	var diffParts []string
	for _, file := range files {
		abs, rel, err := e.Workspace.resolve(file.Path)
		if err != nil {
			return nil, err
		}
		oldContent := ""
		if data, err := os.ReadFile(abs); err == nil {
			oldContent = string(data)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		diff := unifiedDiff(rel, oldContent, file.Content)
		diffParts = append(diffParts, diff)
		changedFiles = append(changedFiles, rel)
		fileOutputs = append(fileOutputs, map[string]any{
			"path":       rel,
			"old_sha256": sha256Hex([]byte(oldContent)),
			"new_sha256": sha256Hex([]byte(file.Content)),
			"old_size":   len(oldContent),
			"new_size":   len(file.Content),
			"diff":       diff,
			"content":    file.Content,
			"expected_sha256": firstNonEmpty(
				file.ExpectedSHA256,
				sha256Hex([]byte(oldContent)),
			),
		})
	}
	return &core.ToolResult{
		Output: map[string]any{
			"changed_files": changedFiles,
			"files":         fileOutputs,
			"diff":          strings.Join(diffParts, "\n"),
		},
	}, nil
}

// Execute implements code.apply_patch.
func (e *ApplyPatchExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	if diff, _ := input["diff"].(string); strings.TrimSpace(diff) != "" {
		prepared, err := prepareUnifiedDiffPatch(e.Workspace, input, diff)
		if err != nil {
			return nil, err
		}
		if err := applyPreparedPatch(prepared); err != nil {
			return nil, err
		}
		return patchAppliedResult(prepared), nil
	}
	files, err := parsePatchFiles(input)
	if err != nil {
		return nil, err
	}
	prepared := make([]preparedPatchFile, 0, len(files))
	for _, file := range files {
		abs, rel, err := e.Workspace.resolve(file.Path)
		if err != nil {
			return nil, err
		}
		current, exists, mode, err := readPatchTarget(abs)
		if err != nil {
			return nil, err
		}
		expected := file.ExpectedSHA256
		if expected == "" {
			expected, _ = input["old_sha256"].(string)
		}
		if expected != "" && expected != sha256Hex(current) {
			return nil, fmt.Errorf("patch base hash mismatch for %s", rel)
		}
		prepared = append(prepared, preparedPatchFile{
			Path:          rel,
			AbsPath:       abs,
			OldContent:    current,
			NewContent:    []byte(file.Content),
			Mode:          mode,
			ExistedBefore: exists,
		})
	}
	if err := applyPreparedPatch(prepared); err != nil {
		return nil, err
	}
	return patchAppliedResult(prepared), nil
}

func patchAppliedResult(prepared []preparedPatchFile) *core.ToolResult {
	changed := make([]map[string]any, 0, len(prepared))
	rollbackFiles := make([]map[string]any, 0, len(prepared))
	for _, file := range prepared {
		changed = append(changed, map[string]any{
			"path":       file.Path,
			"old_sha256": sha256Hex(file.OldContent),
			"new_sha256": sha256Hex(file.NewContent),
			"old_size":   len(file.OldContent),
			"new_size":   len(file.NewContent),
		})
		rollbackFiles = append(rollbackFiles, rollbackSnapshotForFile(file.Path, file.OldContent, file.NewContent, file.ExistedBefore, file.Mode))
	}
	return &core.ToolResult{
		Output: map[string]any{
			"changed_files":     changed,
			"rollback_snapshot": rollbackSnapshot(rollbackFiles),
			"status":            "applied",
		},
	}
}

type patchFile struct {
	Path           string
	Content        string
	ExpectedSHA256 string
}

type preparedPatchFile struct {
	Path          string
	AbsPath       string
	OldContent    []byte
	NewContent    []byte
	Mode          os.FileMode
	ExistedBefore bool
	Delete        bool
}

func parsePatchFiles(input map[string]any) ([]patchFile, error) {
	if rawFiles, ok := input["files"]; ok {
		files, err := parsePatchFileList(rawFiles)
		if err != nil {
			return nil, err
		}
		if len(files) > 0 {
			return files, nil
		}
	}
	path, err := tools.GetString(input, "path")
	if err != nil {
		return nil, err
	}
	content, err := tools.GetString(input, "content")
	if err != nil {
		return nil, err
	}
	expected, _ := input["expected_sha256"].(string)
	return []patchFile{{Path: path, Content: content, ExpectedSHA256: expected}}, nil
}

func parsePatchFileList(raw any) ([]patchFile, error) {
	var items []any
	switch typed := raw.(type) {
	case []any:
		items = typed
	case []map[string]any:
		items = make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
	case []PatchFileInput:
		files := make([]patchFile, 0, len(typed))
		for _, item := range typed {
			if item.Path == "" {
				return nil, errors.New("patch file path is required")
			}
			files = append(files, patchFile{
				Path:           item.Path,
				Content:        item.Content,
				ExpectedSHA256: item.ExpectedSHA256,
			})
		}
		return files, nil
	default:
		return nil, errors.New("files must be an array")
	}
	files := make([]patchFile, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("files entries must be objects")
		}
		path, _ := m["path"].(string)
		content, ok := m["content"].(string)
		if !ok {
			return nil, errors.New("patch file content is required")
		}
		expected, _ := m["expected_sha256"].(string)
		if path == "" {
			return nil, errors.New("patch file path is required")
		}
		files = append(files, patchFile{
			Path:           path,
			Content:        content,
			ExpectedSHA256: expected,
		})
	}
	return files, nil
}

func readPatchTarget(path string) ([]byte, bool, os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, 0o644, nil
		}
		return nil, false, 0, err
	}
	if info.IsDir() {
		return nil, false, 0, fmt.Errorf("patch target is a directory: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, 0, err
	}
	return data, true, info.Mode().Perm(), nil
}

func applyPreparedPatch(files []preparedPatchFile) error {
	applied := make([]preparedPatchFile, 0, len(files))
	for _, file := range files {
		if file.Delete {
			if err := os.Remove(file.AbsPath); err != nil && !os.IsNotExist(err) {
				rollbackPatch(applied)
				return err
			}
			applied = append(applied, file)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(file.AbsPath), 0o755); err != nil {
			rollbackPatch(applied)
			return err
		}
		tmp, err := os.CreateTemp(filepath.Dir(file.AbsPath), ".agent-patch-*")
		if err != nil {
			rollbackPatch(applied)
			return err
		}
		tmpPath := tmp.Name()
		if _, err := tmp.Write(file.NewContent); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
			rollbackPatch(applied)
			return err
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpPath)
			rollbackPatch(applied)
			return err
		}
		if err := os.Chmod(tmpPath, file.Mode); err != nil {
			_ = os.Remove(tmpPath)
			rollbackPatch(applied)
			return err
		}
		if err := os.Rename(tmpPath, file.AbsPath); err != nil {
			_ = os.Remove(tmpPath)
			rollbackPatch(applied)
			return err
		}
		applied = append(applied, file)
	}
	return nil
}

func rollbackPatch(files []preparedPatchFile) {
	for i := len(files) - 1; i >= 0; i-- {
		file := files[i]
		if !file.ExistedBefore {
			_ = os.Remove(file.AbsPath)
			continue
		}
		_ = os.WriteFile(file.AbsPath, file.OldContent, file.Mode)
	}
}

func rollbackSnapshot(files []map[string]any) map[string]any {
	var seed strings.Builder
	for _, file := range files {
		seed.WriteString(fmt.Sprint(file["path"]))
		seed.WriteString(":")
		seed.WriteString(fmt.Sprint(file["old_sha256"]))
		seed.WriteString(":")
		seed.WriteString(fmt.Sprint(file["new_sha256"]))
		seed.WriteString("\n")
	}
	return map[string]any{
		"snapshot_id": sha256Hex([]byte(seed.String())),
		"files":       files,
	}
}

func rollbackSnapshotForFile(path string, oldContent, newContent []byte, existedBefore bool, mode os.FileMode) map[string]any {
	item := map[string]any{
		"path":           path,
		"old_sha256":     sha256Hex(oldContent),
		"old_size":       len(oldContent),
		"existed_before": existedBefore,
	}
	if newContent != nil {
		item["new_sha256"] = sha256Hex(newContent)
		item["new_size"] = len(newContent)
	}
	if mode != 0 {
		item["mode"] = fmt.Sprintf("%#o", mode.Perm())
	}
	return item
}

func unifiedDiff(path, oldContent, newContent string) string {
	if oldContent == newContent {
		return ""
	}
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)
	var b strings.Builder
	b.WriteString("--- a/")
	b.WriteString(path)
	b.WriteString("\n+++ b/")
	b.WriteString(path)
	b.WriteString("\n@@ -1,")
	b.WriteString(fmt.Sprint(len(oldLines)))
	b.WriteString(" +1,")
	b.WriteString(fmt.Sprint(len(newLines)))
	b.WriteString(" @@\n")
	for _, line := range oldLines {
		b.WriteString("-")
		b.WriteString(line)
		b.WriteString("\n")
	}
	for _, line := range newLines {
		b.WriteString("+")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(content, "\n"), "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
