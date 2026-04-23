package memory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/ids"
	"local-agent/internal/security"
)

// Store manages Markdown memory as the source of truth.
type Store struct {
	Root    string
	Indexer *Indexer
}

// NewStore creates a Markdown memory store.
func NewStore(root string, indexer *Indexer) *Store {
	return &Store{
		Root:    root,
		Indexer: indexer,
	}
}

// ListFiles lists Markdown files under the memory root.
func (s *Store) ListFiles() ([]string, error) {
	var items []string
	err := filepath.WalkDir(s.Root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(s.Root, path)
		if err != nil {
			return err
		}
		items = append(items, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(items)
	return items, nil
}

// ReadFile reads and parses a markdown memory file.
func (s *Store) ReadFile(path string) (core.MemoryFile, error) {
	abs, err := s.resolve(path)
	if err != nil {
		return core.MemoryFile{}, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return core.MemoryFile{}, err
	}
	frontmatter, body := parseMarkdown(string(data))
	return core.MemoryFile{
		Path:        filepath.ToSlash(path),
		Frontmatter: frontmatter,
		Body:        body,
	}, nil
}

// WriteFile writes a markdown memory file.
func (s *Store) WriteFile(file core.MemoryFile) error {
	if security.IsSensitivePath(file.Path, nil) {
		return errors.New("refusing to write sensitive memory path")
	}
	abs, err := s.resolve(file.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(renderMarkdown(file.Frontmatter, file.Body)), 0o644)
}

// Search performs a lexical scan over memory markdown files.
func (s *Store) Search(query string, limit int) ([]core.MemoryFile, error) {
	files, err := s.ListFiles()
	if err != nil {
		return nil, err
	}
	results := make([]core.MemoryFile, 0, len(files))
	for _, path := range files {
		file, err := s.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if strings.Contains(strings.ToLower(file.Body), strings.ToLower(query)) || frontmatterContains(file.Frontmatter, query) {
			results = append(results, file)
		}
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// CreatePatch writes a pending patch record under memory/pending.
func (s *Store) CreatePatch(input core.MemoryPatch) (core.MemoryPatch, error) {
	input.ID = ids.New("mempatch")
	input.CreatedAt = time.Now().UTC()
	if input.Path == "" {
		return core.MemoryPatch{}, errors.New("patch path is required")
	}
	if input.Sensitive {
		return core.MemoryPatch{}, errors.New("sensitive patches must not be auto-created")
	}

	abs := filepath.Join(s.Root, "pending", input.ID+".json")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return core.MemoryPatch{}, err
	}
	raw, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return core.MemoryPatch{}, err
	}
	if err := os.WriteFile(abs, raw, 0o644); err != nil {
		return core.MemoryPatch{}, err
	}
	return input, nil
}

// ApplyPatch applies a memory patch and triggers reindexing.
func (s *Store) ApplyPatch(ctx context.Context, patch core.MemoryPatch) error {
	if patch.Sensitive {
		return errors.New("refusing to apply sensitive memory patch")
	}
	if err := s.WriteFile(core.MemoryFile{
		Path:        patch.Path,
		Frontmatter: patch.Frontmatter,
		Body:        patch.Body,
	}); err != nil {
		return err
	}
	return s.Reindex(ctx)
}

// Reindex reloads markdown files and updates the vector index.
func (s *Store) Reindex(ctx context.Context) error {
	if s.Indexer == nil {
		return nil
	}
	paths, err := s.ListFiles()
	if err != nil {
		return err
	}
	files := make([]core.MemoryFile, 0, len(paths))
	for _, path := range paths {
		file, err := s.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, file)
	}
	return s.Indexer.Reindex(ctx, files)
}

func (s *Store) resolve(path string) (string, error) {
	abs := filepath.Clean(filepath.Join(s.Root, path))
	root := filepath.Clean(s.Root)
	if !strings.HasPrefix(abs, root) {
		return "", errors.New("path escapes memory root")
	}
	return abs, nil
}

func parseMarkdown(content string) (map[string]string, string) {
	lines := strings.Split(content, "\n")
	if len(lines) > 2 && strings.TrimSpace(lines[0]) == "---" {
		frontmatter := map[string]string{}
		end := -1
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				end = i
				break
			}
			parts := strings.SplitN(lines[i], ":", 2)
			if len(parts) != 2 {
				continue
			}
			frontmatter[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
		if end != -1 {
			return frontmatter, strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
		}
	}
	return map[string]string{}, strings.TrimSpace(content)
}

func renderMarkdown(frontmatter map[string]string, body string) string {
	if len(frontmatter) == 0 {
		return strings.TrimSpace(body) + "\n"
	}
	var builder strings.Builder
	builder.WriteString("---\n")
	keys := make([]string, 0, len(frontmatter))
	for key := range frontmatter {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteString(": ")
		builder.WriteString(frontmatter[key])
		builder.WriteString("\n")
	}
	builder.WriteString("---\n\n")
	builder.WriteString(strings.TrimSpace(body))
	builder.WriteString("\n")
	return builder.String()
}

func frontmatterContains(frontmatter map[string]string, query string) bool {
	query = strings.ToLower(query)
	for key, value := range frontmatter {
		if strings.Contains(strings.ToLower(key), query) || strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}
