package code

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"local-agent/internal/core"
	"local-agent/internal/security"
	"local-agent/internal/tools"
)

const (
	defaultMaxReadBytes = int64(1024 * 1024)
	defaultSearchLimit  = 100
	defaultListLimit    = 500
	defaultMaxDepth     = 8
	maxLineBytes        = 1024 * 1024
)

// Workspace provides workspace-bound code operations.
type Workspace struct {
	Root           string
	SensitivePaths []string
}

// ReadExecutor reads a file from the workspace.
type ReadExecutor struct {
	Workspace Workspace
}

// SearchExecutor searches for substring matches within the workspace.
type SearchExecutor struct {
	Workspace Workspace
}

// ListFilesExecutor lists files within the workspace.
type ListFilesExecutor struct {
	Workspace Workspace
}

// SearchSymbolExecutor searches for symbol-like token matches within source files.
type SearchSymbolExecutor struct {
	Workspace Workspace
}

// Execute implements code.read_file.
func (e *ReadExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	path, err := tools.GetString(input, "path")
	if err != nil {
		return nil, err
	}
	abs, rel, err := e.Workspace.resolve(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory: %s", path)
	}
	maxBytes := tools.GetInt(input, "max_bytes", int(defaultMaxReadBytes))
	if maxBytes <= 0 {
		maxBytes = int(defaultMaxReadBytes)
	}
	data, truncated, err := readFileLimit(abs, int64(maxBytes))
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{
		Output: map[string]any{
			"path":      rel,
			"content":   string(data),
			"size":      info.Size(),
			"truncated": truncated,
			"sha256":    sha256Hex(data),
		},
	}, nil
}

// Execute implements code.search and code.search_text.
func (e *SearchExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	query, err := tools.GetString(input, "query")
	if err != nil {
		return nil, err
	}
	if query == "" {
		return nil, errors.New("query is required")
	}
	basePath, _ := input["path"].(string)
	if basePath == "" {
		basePath = "."
	}
	root, relRoot, err := e.Workspace.resolve(basePath)
	if err != nil {
		return nil, err
	}
	limit := tools.GetInt(input, "limit", defaultSearchLimit)
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	includeSensitive, _ := input["include_sensitive"].(bool)

	var matches []map[string]any
	skippedSensitive := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if len(matches) >= limit {
			return filepath.SkipAll
		}
		rel, _ := e.Workspace.rel(path)
		if e.Workspace.isSensitive(rel) && !includeSensitive {
			skippedSensitive++
			return nil
		}
		fileMatches, err := searchFile(path, query, limit-len(matches), false)
		if err != nil {
			return nil
		}
		matches = append(matches, withPath(rel, fileMatches)...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &core.ToolResult{
		Output: map[string]any{
			"path":              relRoot,
			"query":             query,
			"matches":           matches,
			"skipped_sensitive": skippedSensitive,
		},
	}, nil
}

// Execute implements code.list_files.
func (e *ListFilesExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	basePath, _ := input["path"].(string)
	if basePath == "" {
		basePath = "."
	}
	root, relRoot, err := e.Workspace.resolve(basePath)
	if err != nil {
		return nil, err
	}
	limit := tools.GetInt(input, "limit", defaultListLimit)
	if limit <= 0 {
		limit = defaultListLimit
	}
	maxDepth := tools.GetInt(input, "max_depth", defaultMaxDepth)
	if maxDepth < 0 {
		maxDepth = defaultMaxDepth
	}
	includeSensitive, _ := input["include_sensitive"].(bool)

	var entries []map[string]any
	skippedSensitive := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, _ := e.Workspace.rel(path)
		depth := pathDepth(relRoot, rel)
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() && shouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}
		if e.Workspace.isSensitive(rel) && !includeSensitive {
			skippedSensitive++
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if len(entries) >= limit {
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		kind := "file"
		if d.IsDir() {
			kind = "dir"
		}
		entries = append(entries, map[string]any{
			"path": rel,
			"type": kind,
			"size": info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return fmt.Sprint(entries[i]["path"]) < fmt.Sprint(entries[j]["path"])
	})
	return &core.ToolResult{
		Output: map[string]any{
			"path":              relRoot,
			"entries":           entries,
			"skipped_sensitive": skippedSensitive,
			"truncated":         len(entries) >= limit,
		},
	}, nil
}

// Execute implements code.search_symbol.
func (e *SearchSymbolExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	symbol, _ := input["symbol"].(string)
	if symbol == "" {
		symbol, _ = input["query"].(string)
	}
	if symbol == "" {
		return nil, errors.New("symbol is required")
	}
	basePath, _ := input["path"].(string)
	if basePath == "" {
		basePath = "."
	}
	root, relRoot, err := e.Workspace.resolve(basePath)
	if err != nil {
		return nil, err
	}
	limit := tools.GetInt(input, "limit", defaultSearchLimit)
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	includeSensitive, _ := input["include_sensitive"].(bool)

	var matches []map[string]any
	skippedSensitive := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if len(matches) >= limit {
			return filepath.SkipAll
		}
		rel, _ := e.Workspace.rel(path)
		if e.Workspace.isSensitive(rel) && !includeSensitive {
			skippedSensitive++
			return nil
		}
		fileMatches, err := searchFile(path, symbol, limit-len(matches), true)
		if err != nil {
			return nil
		}
		matches = append(matches, withPath(rel, fileMatches)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &core.ToolResult{
		Output: map[string]any{
			"path":              relRoot,
			"symbol":            symbol,
			"matches":           matches,
			"skipped_sensitive": skippedSensitive,
		},
	}, nil
}

func (w Workspace) resolve(path string) (string, string, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	root, err := filepath.Abs(defaultRoot(w.Root))
	if err != nil {
		return "", "", err
	}
	root = filepath.Clean(root)
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(root, path))
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", "", errors.New("path escapes workspace")
	}
	if rel == "." {
		return abs, ".", nil
	}
	return abs, filepath.ToSlash(rel), nil
}

func (w Workspace) rel(abs string) (string, error) {
	root, err := filepath.Abs(defaultRoot(w.Root))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(abs))
	if err != nil {
		return "", err
	}
	if rel == "." {
		return ".", nil
	}
	return filepath.ToSlash(rel), nil
}

func (w Workspace) isSensitive(path string) bool {
	return security.IsSensitivePath(path, w.SensitivePaths)
}

func defaultRoot(root string) string {
	if strings.TrimSpace(root) == "" {
		return "."
	}
	return root
}

func readFileLimit(path string, maxBytes int64) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > maxBytes {
		return data[:maxBytes], true, nil
	}
	return data, false, nil
}

func searchFile(path, query string, limit int, symbol bool) ([]map[string]any, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	head := make([]byte, 512)
	n, _ := file.Read(head)
	if looksBinary(head[:n]) {
		return nil, nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
	var matches []map[string]any
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		matched := strings.Contains(line, query)
		if symbol {
			matched = containsSymbol(line, query)
		}
		if !matched {
			continue
		}
		matches = append(matches, map[string]any{
			"line_number": lineNumber,
			"line":        trimLine(line),
		})
		if len(matches) >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return matches, nil
}

func withPath(path string, matches []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		cp := core.CloneMap(match)
		cp["path"] = path
		out = append(out, cp)
	}
	return out
}

func looksBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return !utf8.Valid(data)
}

func trimLine(line string) string {
	const max = 500
	if len(line) <= max {
		return line
	}
	return line[:max] + "..."
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", ".next", ".cache":
		return true
	default:
		return false
	}
}

func pathDepth(base, rel string) int {
	if base == "." {
		if rel == "." {
			return 0
		}
		return strings.Count(rel, "/") + 1
	}
	trimmed := strings.TrimPrefix(rel, strings.TrimSuffix(base, "/")+"/")
	if trimmed == rel {
		return 0
	}
	return strings.Count(trimmed, "/") + 1
}

func containsSymbol(line, symbol string) bool {
	idx := strings.Index(line, symbol)
	for idx >= 0 {
		beforeOK := idx == 0 || !isSymbolChar(runeBefore(line, idx))
		afterIdx := idx + len(symbol)
		afterOK := afterIdx >= len(line) || !isSymbolChar(runeAt(line, afterIdx))
		if beforeOK && afterOK {
			return true
		}
		next := strings.Index(line[idx+len(symbol):], symbol)
		if next < 0 {
			break
		}
		idx += len(symbol) + next
	}
	return false
}

func isSymbolChar(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func runeBefore(s string, idx int) rune {
	r, _ := utf8.DecodeLastRuneInString(s[:idx])
	return r
}

func runeAt(s string, idx int) rune {
	r, _ := utf8.DecodeRuneInString(s[idx:])
	return r
}
