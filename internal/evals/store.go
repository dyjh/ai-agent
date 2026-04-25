package evals

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/ids"
	"local-agent/internal/security"
	toolscore "local-agent/internal/tools"
)

// Manager owns eval case storage, safe-mode execution, reports and replay records.
type Manager struct {
	root       string
	eventsRoot string
	registry   *toolscore.Registry
	policy     config.PolicyConfig
	runtime    *agent.Runtime
	mu         sync.Mutex
}

// NewManager constructs an eval manager rooted at evals/.
func NewManager(root, eventsRoot string, registry *toolscore.Registry, policy config.PolicyConfig, runtime *agent.Runtime) *Manager {
	return &Manager{
		root:       root,
		eventsRoot: eventsRoot,
		registry:   registry,
		policy:     policy,
		runtime:    runtime,
	}
}

// Root returns the eval storage root.
func (m *Manager) Root() string {
	if strings.TrimSpace(m.root) == "" {
		return "evals"
	}
	return m.root
}

func (m *Manager) casesRoot() string { return filepath.Join(m.Root(), "cases") }
func (m *Manager) runsRoot() string  { return filepath.Join(m.Root(), "runs") }
func (m *Manager) reportsRoot() string {
	return filepath.Join(m.Root(), "reports")
}
func (m *Manager) replaysRoot() string {
	return filepath.Join(m.Root(), "replays")
}
func (m *Manager) fixturesRoot() string {
	return filepath.Join(m.Root(), "fixtures")
}

// EnsureLayout creates the standard eval directory structure.
func (m *Manager) EnsureLayout() error {
	for _, dir := range []string{
		m.casesRoot(),
		filepath.Join(m.casesRoot(), string(EvalCategoryChat)),
		filepath.Join(m.casesRoot(), string(EvalCategoryRAG)),
		filepath.Join(m.casesRoot(), string(EvalCategoryCode)),
		filepath.Join(m.casesRoot(), string(EvalCategoryOps)),
		filepath.Join(m.casesRoot(), string(EvalCategorySafety)),
		filepath.Join(m.fixturesRoot(), string(EvalCategoryCode)),
		filepath.Join(m.fixturesRoot(), "kb"),
		filepath.Join(m.fixturesRoot(), string(EvalCategoryOps)),
		filepath.Join(m.fixturesRoot(), "memory"),
		m.runsRoot(),
		m.reportsRoot(),
		m.replaysRoot(),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// ParseEvalCase parses one YAML or JSON eval case and rejects secret-bearing content.
func ParseEvalCase(raw []byte, name string) (EvalCase, error) {
	if scan := security.ScanText(string(raw)); scan.HasSecret {
		return EvalCase{}, fmt.Errorf("eval case contains secret-like content: %s", summarizeFindings(scan.Findings))
	}
	var c EvalCase
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json":
		if err := json.Unmarshal(raw, &c); err != nil {
			return EvalCase{}, err
		}
	default:
		if err := yaml.Unmarshal(raw, &c); err != nil {
			return EvalCase{}, err
		}
	}
	if err := ValidateEvalCase(c); err != nil {
		return EvalCase{}, err
	}
	return c, nil
}

// ValidateEvalCase validates required fields and category.
func ValidateEvalCase(c EvalCase) error {
	if strings.TrimSpace(c.ID) == "" {
		return errors.New("eval case id is required")
	}
	if !c.Category.Valid() {
		return fmt.Errorf("invalid eval category %q", c.Category)
	}
	if strings.TrimSpace(c.Input) == "" && len(c.Conversation) == 0 {
		return errors.New("eval case input or conversation is required")
	}
	return nil
}

// ListCases returns eval cases filtered by request fields.
func (m *Manager) ListCases(filter EvalRunRequest) ([]EvalCase, error) {
	if err := m.EnsureLayout(); err != nil {
		return nil, err
	}
	items := []EvalCase{}
	err := filepath.WalkDir(m.casesRoot(), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isCaseFile(path) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		item, err := ParseEvalCase(raw, path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		item.SourcePath = path
		if caseMatchesFilter(item, filter) {
			items = append(items, item)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// GetCase returns one eval case by id.
func (m *Manager) GetCase(id string) (EvalCase, error) {
	items, err := m.ListCases(EvalRunRequest{CaseIDs: []string{id}})
	if err != nil {
		return EvalCase{}, err
	}
	if len(items) == 0 {
		return EvalCase{}, fmt.Errorf("eval case not found: %s", id)
	}
	return items[0], nil
}

// CreateCase writes one eval case to disk.
func (m *Manager) CreateCase(c EvalCase) (EvalCase, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(c.ID) == "" {
		c.ID = ids.New("evalcase")
	}
	if err := ValidateEvalCase(c); err != nil {
		return EvalCase{}, err
	}
	raw, err := yaml.Marshal(c)
	if err != nil {
		return EvalCase{}, err
	}
	if scan := security.ScanText(string(raw)); scan.HasSecret {
		return EvalCase{}, fmt.Errorf("eval case contains secret-like content: %s", summarizeFindings(scan.Findings))
	}
	if err := m.EnsureLayout(); err != nil {
		return EvalCase{}, err
	}
	path := filepath.Join(m.casesRoot(), string(c.Category), safeFileName(c.ID)+".yaml")
	if _, err := os.Stat(path); err == nil {
		return EvalCase{}, fmt.Errorf("eval case already exists: %s", c.ID)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return EvalCase{}, err
	}
	c.SourcePath = path
	return c, nil
}

// UpdateCase replaces an existing eval case, preserving id/category when omitted.
func (m *Manager) UpdateCase(id string, c EvalCase) (EvalCase, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, err := m.getCaseUnlocked(id)
	if err != nil {
		return EvalCase{}, err
	}
	if strings.TrimSpace(c.ID) == "" {
		c.ID = existing.ID
	}
	if c.ID != existing.ID {
		return EvalCase{}, errors.New("eval case id cannot be changed")
	}
	if c.Category == "" {
		c.Category = existing.Category
	}
	if strings.TrimSpace(c.Input) == "" && len(c.Conversation) == 0 {
		c.Input = existing.Input
		c.Conversation = existing.Conversation
	}
	if err := ValidateEvalCase(c); err != nil {
		return EvalCase{}, err
	}
	raw, err := yaml.Marshal(c)
	if err != nil {
		return EvalCase{}, err
	}
	if scan := security.ScanText(string(raw)); scan.HasSecret {
		return EvalCase{}, fmt.Errorf("eval case contains secret-like content: %s", summarizeFindings(scan.Findings))
	}
	path := existing.SourcePath
	if c.Category != existing.Category {
		path = filepath.Join(m.casesRoot(), string(c.Category), safeFileName(c.ID)+".yaml")
		_ = os.Remove(existing.SourcePath)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return EvalCase{}, err
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return EvalCase{}, err
	}
	c.SourcePath = path
	return c, nil
}

// DeleteCase removes one eval case file.
func (m *Manager) DeleteCase(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, err := m.getCaseUnlocked(id)
	if err != nil {
		return err
	}
	return os.Remove(existing.SourcePath)
}

func (m *Manager) getCaseUnlocked(id string) (EvalCase, error) {
	items, err := m.ListCases(EvalRunRequest{CaseIDs: []string{id}})
	if err != nil {
		return EvalCase{}, err
	}
	if len(items) == 0 {
		return EvalCase{}, fmt.Errorf("eval case not found: %s", id)
	}
	return items[0], nil
}

func (m *Manager) saveRun(run EvalRun) error {
	if err := m.EnsureLayout(); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(m.runsRoot(), safeFileName(run.RunID)+".json"), run)
}

// ListRuns returns recent eval runs.
func (m *Manager) ListRuns(limit int) ([]EvalRun, error) {
	if err := m.EnsureLayout(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	entries, err := os.ReadDir(m.runsRoot())
	if err != nil {
		return nil, err
	}
	items := []EvalRun{}
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			continue
		}
		var run EvalRun
		if err := readJSONFile(filepath.Join(m.runsRoot(), entry.Name()), &run); err != nil {
			return nil, err
		}
		items = append(items, run)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt.After(items[j].StartedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// GetRun returns one eval run. The special id "latest" resolves to the newest run.
func (m *Manager) GetRun(runID string) (EvalRun, error) {
	if runID == "latest" || strings.TrimSpace(runID) == "" {
		items, err := m.ListRuns(1)
		if err != nil {
			return EvalRun{}, err
		}
		if len(items) == 0 {
			return EvalRun{}, errors.New("no eval runs found")
		}
		return items[0], nil
	}
	var run EvalRun
	if err := readJSONFile(filepath.Join(m.runsRoot(), safeFileName(runID)+".json"), &run); err != nil {
		return EvalRun{}, err
	}
	return run, nil
}

// GetReport returns a JSON report by run id.
func (m *Manager) GetReport(runID string) (EvalReport, error) {
	run, err := m.GetRun(runID)
	if err != nil {
		return EvalReport{}, err
	}
	var report EvalReport
	if err := readJSONFile(filepath.Join(m.reportsRoot(), safeFileName(run.RunID)+".json"), &report); err != nil {
		return EvalReport{}, err
	}
	return report, nil
}

// GetReportMarkdown returns a Markdown report by run id.
func (m *Manager) GetReportMarkdown(runID string) (string, error) {
	run, err := m.GetRun(runID)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(m.reportsRoot(), safeFileName(run.RunID)+".md"))
	if err != nil {
		return "", err
	}
	return security.RedactString(string(raw)), nil
}

func isCaseFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}

func caseMatchesFilter(c EvalCase, filter EvalRunRequest) bool {
	if len(filter.CaseIDs) > 0 && !containsString(filter.CaseIDs, c.ID) {
		return false
	}
	if filter.Category != "" && c.Category != filter.Category {
		return false
	}
	tags := append([]string{}, filter.Tags...)
	if filter.Tag != "" {
		tags = append(tags, filter.Tag)
	}
	for _, tag := range tags {
		if !containsString(c.Tags, tag) {
			return false
		}
	}
	return true
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

func safeFileName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unnamed"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_")
	return replacer.Replace(value)
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = []byte(security.RedactString(string(raw)))
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func readJSONFile(path string, out any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func summarizeFindings(findings []security.SecretFinding) string {
	parts := make([]string, 0, len(findings))
	for _, finding := range findings {
		parts = append(parts, finding.Type)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
