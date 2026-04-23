package code

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"local-agent/internal/core"
	"local-agent/internal/tools"
)

// Workspace provides workspace-bound code operations.
type Workspace struct {
	Root string
}

// ReadExecutor reads a file from the workspace.
type ReadExecutor struct {
	Workspace Workspace
}

// SearchExecutor searches for substring matches within the workspace.
type SearchExecutor struct {
	Workspace Workspace
}

// Execute implements code.read_file.
func (e *ReadExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	path, err := tools.GetString(input, "path")
	if err != nil {
		return nil, err
	}
	abs, err := e.Workspace.resolve(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{
		Output: map[string]any{
			"path":    path,
			"content": string(data),
		},
	}, nil
}

// Execute implements code.search.
func (e *SearchExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	query, err := tools.GetString(input, "query")
	if err != nil {
		return nil, err
	}
	basePath, _ := input["path"].(string)
	if basePath == "" {
		basePath = "."
	}
	root, err := e.Workspace.resolve(basePath)
	if err != nil {
		return nil, err
	}

	var matches []map[string]any
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		if !strings.Contains(content, query) {
			return nil
		}
		rel, _ := filepath.Rel(e.Workspace.Root, path)
		matches = append(matches, map[string]any{
			"path": rel,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &core.ToolResult{
		Output: map[string]any{
			"query":   query,
			"matches": matches,
		},
	}, nil
}

func (w Workspace) resolve(path string) (string, error) {
	abs := filepath.Join(w.Root, path)
	abs = filepath.Clean(abs)
	root := filepath.Clean(w.Root)
	if !strings.HasPrefix(abs, root) {
		return "", errors.New("path escapes workspace")
	}
	return abs, nil
}
