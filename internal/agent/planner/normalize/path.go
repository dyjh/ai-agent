package normalize

import (
	"path/filepath"
	"strings"
)

// PossibleWorkspaceFiles joins explicit workspace scope with quoted file-like
// spans. It intentionally does not decide whether a read tool should run.
func PossibleWorkspaceFiles(workspace string, quoted []string) []string {
	out := []string{}
	for _, item := range quoted {
		if !looksLikeFile(item) {
			continue
		}
		out = append(out, JoinWorkspacePath(workspace, item))
	}
	return uniq(out)
}

// JoinWorkspacePath scopes a relative path to workspace when provided.
func JoinWorkspacePath(workspace, path string) string {
	workspace = strings.TrimSpace(workspace)
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) || workspace == "" || workspace == "." {
		return filepath.Clean(path)
	}
	return filepath.ToSlash(filepath.Clean(filepath.Join(workspace, path)))
}

func looksLikeFile(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.ContainsAny(value, `/\`) {
		return true
	}
	base := filepath.Base(value)
	ext := filepath.Ext(base)
	return ext != "" && len(strings.TrimPrefix(ext, ".")) > 0
}
