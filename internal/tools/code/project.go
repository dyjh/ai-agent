package code

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"local-agent/internal/core"
)

// InspectProjectExecutor returns lightweight project metadata for the workspace.
type InspectProjectExecutor struct {
	Workspace Workspace
}

// DetectLanguageExecutor detects the dominant language for a workspace path.
type DetectLanguageExecutor struct {
	Workspace Workspace
}

// DetectTestCommandExecutor detects likely test commands without running them.
type DetectTestCommandExecutor struct {
	Workspace Workspace
}

// ExplainDiffExecutor summarizes a diff-like patch payload.
type ExplainDiffExecutor struct{}

// Execute implements code.inspect_project.
func (e *InspectProjectExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	path, _ := input["path"].(string)
	root, relRoot, err := e.Workspace.resolve(path)
	if err != nil {
		return nil, err
	}
	project, err := inspectProject(root, e.Workspace)
	if err != nil {
		return nil, err
	}
	project["path"] = relRoot
	return &core.ToolResult{Output: project}, nil
}

// Execute implements code.detect_language.
func (e *DetectLanguageExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	path, _ := input["path"].(string)
	root, relRoot, err := e.Workspace.resolve(path)
	if err != nil {
		return nil, err
	}
	languages, err := detectLanguages(root)
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{
		Output: map[string]any{
			"path":              relRoot,
			"language":          dominantLanguage(languages),
			"language_counts":   languages,
			"detected_by_files": len(languages) > 0,
		},
	}, nil
}

// Execute implements code.detect_test_command.
func (e *DetectTestCommandExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	path, _ := input["path"].(string)
	root, relRoot, err := e.Workspace.resolve(path)
	if err != nil {
		return nil, err
	}
	commands := detectTestCommands(root)
	return &core.ToolResult{
		Output: map[string]any{
			"path":          relRoot,
			"test_commands": commands,
		},
	}, nil
}

// Execute implements code.explain_diff.
func (e *ExplainDiffExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	files, _ := parsePatchFiles(input)
	diff, _ := input["diff"].(string)
	if diff == "" && len(files) > 0 {
		paths := make([]string, 0, len(files))
		for _, file := range files {
			paths = append(paths, file.Path)
		}
		sort.Strings(paths)
		return &core.ToolResult{
			Output: map[string]any{
				"changed_files": paths,
				"file_count":    len(paths),
			},
		}, nil
	}
	if parsed, err := ParseUnifiedDiff(diff); err == nil {
		added, removed := diffStats(diff)
		paths := make([]string, 0, len(parsed.Files))
		fileSummaries := make([]map[string]any, 0, len(parsed.Files))
		for _, file := range parsed.Files {
			paths = append(paths, file.Path)
			fileSummaries = append(fileSummaries, map[string]any{
				"path":      file.Path,
				"old_path":  file.OldPath,
				"new_path":  file.NewPath,
				"operation": file.Operation,
				"hunks":     len(file.Hunks),
			})
		}
		sort.Strings(paths)
		return &core.ToolResult{
			Output: map[string]any{
				"changed_files":  paths,
				"file_count":     len(parsed.Files),
				"added_lines":    added,
				"removed_lines":  removed,
				"file_summaries": fileSummaries,
			},
		}, nil
	}
	added, removed := diffStats(diff)
	return &core.ToolResult{
		Output: map[string]any{
			"added_lines":   added,
			"removed_lines": removed,
			"file_count":    strings.Count(diff, "\n--- "),
		},
	}, nil
}

func inspectProject(root string, workspace Workspace) (map[string]any, error) {
	languages, err := detectLanguages(root)
	if err != nil {
		return nil, err
	}
	configFiles := detectConfigFiles(root)
	return map[string]any{
		"language":        dominantLanguage(languages),
		"language_counts": languages,
		"config_files":    configFiles,
		"test_commands":   detectTestCommands(root),
		"build_commands":  detectBuildCommands(root),
		"workspace_root":  defaultRoot(workspace.Root),
	}, nil
}

func detectLanguages(root string) (map[string]int, error) {
	counts := map[string]int{}
	seen := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != root && shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		seen++
		if seen > 5000 {
			return filepath.SkipAll
		}
		if lang := languageForPath(path); lang != "" {
			counts[lang]++
		}
		return nil
	})
	return counts, err
}

func dominantLanguage(counts map[string]int) string {
	type item struct {
		lang  string
		count int
	}
	items := make([]item, 0, len(counts))
	for lang, count := range counts {
		items = append(items, item{lang: lang, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].lang < items[j].lang
		}
		return items[i].count > items[j].count
	})
	if len(items) == 0 {
		return "unknown"
	}
	return items[0].lang
}

func languageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".cs":
		return "csharp"
	case ".cpp", ".cc", ".cxx", ".hpp", ".h":
		return "cpp"
	case ".c":
		return "c"
	case ".sh", ".bash", ".zsh":
		return "shell"
	default:
		return ""
	}
}

func detectConfigFiles(root string) []string {
	candidates := []string{
		"go.mod", "go.work", "package.json", "pnpm-lock.yaml", "yarn.lock",
		"package-lock.json", "pyproject.toml", "requirements.txt", "Cargo.toml",
		"Makefile", "Dockerfile", "docker-compose.yml",
	}
	var found []string
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(root, candidate)); err == nil {
			found = append(found, candidate)
		}
	}
	return found
}

func detectTestCommands(root string) []string {
	var commands []string
	if exists(root, "go.mod") {
		commands = append(commands, "go test ./...")
	}
	if exists(root, "package.json") {
		if hasPackageScript(root, "test") {
			commands = append(commands, "npm test")
		}
	}
	if exists(root, "pyproject.toml") || exists(root, "pytest.ini") || exists(root, "requirements.txt") {
		commands = append(commands, "pytest")
	}
	if exists(root, "Cargo.toml") {
		commands = append(commands, "cargo test")
	}
	if exists(root, "Makefile") {
		commands = append(commands, "make test")
	}
	return uniqueStrings(commands)
}

func detectBuildCommands(root string) []string {
	var commands []string
	if exists(root, "go.mod") {
		commands = append(commands, "go build ./...")
	}
	if exists(root, "package.json") && hasPackageScript(root, "build") {
		commands = append(commands, "npm run build")
	}
	if exists(root, "Cargo.toml") {
		commands = append(commands, "cargo build")
	}
	if exists(root, "Makefile") {
		commands = append(commands, "make")
	}
	return uniqueStrings(commands)
}

func exists(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, name))
	return err == nil
}

func hasPackageScript(root, script string) bool {
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]any `json:"scripts"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return false
	}
	_, ok := pkg.Scripts[script]
	return ok
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func diffStats(diff string) (int, int) {
	added := 0
	removed := 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			added++
		}
		if strings.HasPrefix(line, "-") {
			removed++
		}
	}
	return added, removed
}
