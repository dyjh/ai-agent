package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
	"local-agent/internal/db"
	"local-agent/internal/db/repo"
	"local-agent/internal/einoapp"
	"local-agent/internal/evals"
	"local-agent/internal/events"
	toolscore "local-agent/internal/tools"
	"local-agent/internal/tools/code"
	"local-agent/internal/tools/gittools"
	"local-agent/internal/tools/kb"
	"local-agent/internal/tools/mcp"
	memstore "local-agent/internal/tools/memory"
	"local-agent/internal/tools/ops"
	"local-agent/internal/tools/shell"
	"local-agent/internal/tools/skills"
)

// Bootstrap owns process-wide services created from config.
type Bootstrap struct {
	Config    config.Config
	Logger    *slog.Logger
	Store     *repo.Store
	Events    *events.JSONLWriter
	Approvals *agent.ApprovalCenter
	Registry  *toolscore.Registry
	Router    *toolscore.LocalRouter
	Runtime   *agent.Runtime
	Memory    *memstore.Store
	Knowledge *kb.Service
	Skills    *skills.Manager
	MCP       *mcp.Manager
	Ops       *ops.Manager
	Evals     *evals.Manager
}

// NewBootstrap wires the base application dependencies.
func NewBootstrap(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Bootstrap, error) {
	if err := config.ValidateKnowledgeBase(cfg); err != nil {
		return nil, err
	}
	_ = os.MkdirAll(cfg.Memory.RootDir, 0o755)
	_ = os.MkdirAll(cfg.Events.JSONLRoot, 0o755)
	_ = os.MkdirAll(cfg.Events.AuditRoot, 0o755)
	skillsRoot := filepath.Join(filepath.Dir(cfg.Memory.RootDir), "skills")
	_ = os.MkdirAll(skillsRoot, 0o755)
	runbookRoot := filepath.Join(filepath.Dir(cfg.Memory.RootDir), "runbooks")
	_ = os.MkdirAll(runbookRoot, 0o755)
	evalsRoot := filepath.Join(filepath.Dir(cfg.Memory.RootDir), "evals")
	_ = os.MkdirAll(evalsRoot, 0o755)

	store := repo.NewMemoryStore()
	if cfg.Database.URL != "" {
		pool, err := db.Open(ctx, cfg.Database.URL)
		if err != nil {
			logger.Warn("postgres unavailable, falling back to memory store", "error", err)
		} else if pgStore := repo.NewPostgresStore(pool); pgStore != nil {
			store = pgStore
		}
	}

	embedder, err := kb.NewEmbedder(cfg.Embeddings, cfg.Vector.EmbeddingDimension)
	if err != nil {
		return nil, err
	}
	index, err := kb.NewVectorIndexFactory(logger).NewVectorIndex(ctx, cfg, embedder)
	if err != nil {
		return nil, err
	}
	memoryIndexer := &memstore.Indexer{
		Collection: cfg.CollectionName("memory"),
		Index:      index,
		Embedder:   embedder,
	}
	memoryStore := memstore.NewStore(cfg.Memory.RootDir, memoryIndexer)
	if err := memoryStore.Reindex(ctx); err != nil {
		logger.Warn("memory reindex failed", "error", err)
	}

	var knowledgeService *kb.Service
	if cfg.KB.Enabled {
		kbCfg := cfg
		kbCfg.Vector.Backend = config.VectorBackend(cfg.KB.Provider)
		kbCfg.Vector.FallbackToMemory = false
		kbIndex, err := kb.NewVectorIndexFactory(logger).NewVectorIndex(ctx, kbCfg, embedder)
		if err != nil {
			return nil, fmt.Errorf("knowledge base %s unavailable: %w", cfg.KB.Provider, err)
		}
		knowledgeService = kb.NewService(kbIndex, embedder, kbCfg.CollectionName("kb"))
		knowledgeService.SetNetworkPolicy(cfg.Policy.Network)
	}
	skillsManager := skills.NewManager(skillsRoot)
	mcpManager := mcp.NewManager()
	mcpManager.SetNetworkPolicy(cfg.Policy.Network)
	if err := mcpManager.LoadConfig(resolveConfigPath("config/mcp.servers.yaml"), resolveConfigPath("config/mcp.tool-policies.yaml")); err != nil {
		return nil, err
	}
	opsManager := ops.NewManager(runbookRoot)
	approvals := agent.NewApprovalCenter()
	eventWriter := events.NewJSONLWriter(cfg.Events.JSONLRoot, cfg.Events.AuditRoot)
	registry := registerTools(cfg, memoryStore, knowledgeService, skillsManager, mcpManager, opsManager)
	router := toolscore.NewRouter(
		registry,
		agent.NewEffectInferrer(cfg.Policy, skillsManager, mcpManager),
		agent.NewPolicyEngine(cfg.Policy),
		approvals,
		eventWriter,
	)
	opsManager.SetRouter(router)

	model, err := einoapp.NewChatModel(ctx, cfg.LLM)
	if err != nil {
		return nil, err
	}

	runtime := &agent.Runtime{
		Store:            store,
		Planner:          agent.HeuristicPlanner{},
		Runner:           einoapp.Runner{Model: model},
		Approvals:        approvals,
		Events:           eventWriter,
		StateStore:       agent.NewPersistentRunStateStore(store.AgentRuns, store.AgentRunSteps),
		MaxWorkflowSteps: 6,
		ContextBuilder: &agent.ContextBuilder{
			Store:       store,
			Memory:      memoryStore,
			Knowledge:   knowledgeService,
			ToolCatalog: registry,
			MaxChars:    8000,
		},
		Router: router,
	}
	evalsManager := evals.NewManager(evalsRoot, cfg.Events.JSONLRoot, registry, cfg.Policy, runtime)
	if err := evalsManager.EnsureLayout(); err != nil {
		return nil, err
	}

	return &Bootstrap{
		Config:    cfg,
		Logger:    logger,
		Store:     store,
		Events:    eventWriter,
		Approvals: approvals,
		Registry:  registry,
		Router:    router,
		Runtime:   runtime,
		Memory:    memoryStore,
		Knowledge: knowledgeService,
		Skills:    skillsManager,
		MCP:       mcpManager,
		Ops:       opsManager,
		Evals:     evalsManager,
	}, nil
}

func registerTools(cfg config.Config, memoryStore *memstore.Store, knowledge *kb.Service, skillsManager *skills.Manager, mcpManager *mcp.Manager, opsManager *ops.Manager) *toolscore.Registry {
	registry := toolscore.NewRegistry()
	workspace := code.Workspace{Root: cfg.Owner.DefaultWorkspace, SensitivePaths: cfg.Policy.SensitivePaths}

	registry.Register(coreShellSpec(), &shell.Executor{
		DefaultShell:   cfg.Owner.DefaultShell,
		DefaultTimeout: cfg.Shell.DefaultTimeoutSeconds,
		MaxOutputChars: cfg.Shell.MaxOutputChars,
	})
	registry.Register(core.ToolSpec{
		ID:             "code.read_file",
		Provider:       "local",
		Name:           "code.read_file",
		Description:    "Read a file from the workspace",
		InputSchema:    map[string]any{"path": "string", "max_bytes": "number"},
		DefaultEffects: []string{"read", "code.read"},
	}, &code.ReadExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.list_files",
		Provider:       "local",
		Name:           "code.list_files",
		Description:    "List files within the workspace",
		InputSchema:    map[string]any{"path": "string", "max_depth": "number", "limit": "number"},
		DefaultEffects: []string{"read", "code.read"},
	}, &code.ListFilesExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.search",
		Provider:       "local",
		Name:           "code.search",
		Description:    "Search for text in the workspace (legacy alias for code.search_text)",
		InputSchema:    map[string]any{"path": "string", "query": "string", "limit": "number"},
		DefaultEffects: []string{"read", "code.read"},
	}, &code.SearchExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.search_text",
		Provider:       "local",
		Name:           "code.search_text",
		Description:    "Search text in workspace files and return line matches",
		InputSchema:    map[string]any{"path": "string", "query": "string", "limit": "number"},
		DefaultEffects: []string{"read", "code.read"},
	}, &code.SearchExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.search_symbol",
		Provider:       "local",
		Name:           "code.search_symbol",
		Description:    "Search symbol-like token matches in workspace files",
		InputSchema:    map[string]any{"path": "string", "symbol": "string", "limit": "number"},
		DefaultEffects: []string{"read", "code.read"},
	}, &code.SearchSymbolExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.inspect_project",
		Provider:       "local",
		Name:           "code.inspect_project",
		Description:    "Inspect project language, config files, and likely commands",
		InputSchema:    map[string]any{"path": "string"},
		DefaultEffects: []string{"read", "code.read"},
	}, &code.InspectProjectExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.detect_language",
		Provider:       "local",
		Name:           "code.detect_language",
		Description:    "Detect dominant language for a workspace path",
		InputSchema:    map[string]any{"path": "string"},
		DefaultEffects: []string{"read", "code.read"},
	}, &code.DetectLanguageExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.detect_test_command",
		Provider:       "local",
		Name:           "code.detect_test_command",
		Description:    "Detect likely test commands without running them",
		InputSchema:    map[string]any{"path": "string"},
		DefaultEffects: []string{"read", "code.read"},
	}, &code.DetectTestCommandExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.propose_patch",
		Provider:       "local",
		Name:           "code.propose_patch",
		Description:    "Preview a code patch without applying it",
		InputSchema:    map[string]any{"path": "string", "content": "string", "files": "array", "diff": "string", "expected_sha256": "string", "expected_sha256_by_path": "object"},
		DefaultEffects: []string{"read", "code.plan"},
	}, &code.ProposePatchExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.apply_patch",
		Provider:       "local",
		Name:           "code.apply_patch",
		Description:    "Apply a patch inside the workspace",
		InputSchema:    map[string]any{"path": "string", "content": "string", "files": "array", "diff": "string", "expected_sha256": "string"},
		DefaultEffects: []string{"fs.write", "code.modify"},
	}, &code.ApplyPatchExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.validate_patch",
		Provider:       "local",
		Name:           "code.validate_patch",
		Description:    "Validate a patch without modifying files",
		InputSchema:    map[string]any{"path": "string", "content": "string", "files": "array", "diff": "string", "expected_sha256": "string"},
		DefaultEffects: []string{"read", "code.plan"},
	}, &code.ValidatePatchExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.dry_run_patch",
		Provider:       "local",
		Name:           "code.dry_run_patch",
		Description:    "Dry-run a patch without modifying files",
		InputSchema:    map[string]any{"path": "string", "content": "string", "files": "array", "diff": "string", "expected_sha256": "string"},
		DefaultEffects: []string{"read", "code.plan"},
	}, &code.DryRunPatchExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.explain_diff",
		Provider:       "local",
		Name:           "code.explain_diff",
		Description:    "Summarize a patch or diff payload without modifying files",
		InputSchema:    map[string]any{"diff": "string", "files": "array"},
		DefaultEffects: []string{"read", "code.plan"},
	}, &code.ExplainDiffExecutor{})
	registry.Register(core.ToolSpec{
		ID:             "code.run_tests",
		Provider:       "local",
		Name:           "code.run_tests",
		Description:    "Run an allowlisted test command inside the workspace",
		InputSchema:    map[string]any{"workspace": "string", "command": "string", "args": "array", "timeout_seconds": "number", "max_output_bytes": "number", "use_detected": "boolean", "test_name_pattern": "string"},
		DefaultEffects: []string{"code.test", "process.read", "fs.read"},
	}, &code.RunTestsExecutor{Workspace: workspace, DefaultTimeoutSeconds: cfg.Shell.DefaultTimeoutSeconds, MaxOutputBytes: int64(cfg.Shell.MaxOutputChars)})
	registry.Register(core.ToolSpec{
		ID:             "code.parse_test_failure",
		Provider:       "local",
		Name:           "code.parse_test_failure",
		Description:    "Parse test output into structured failure information",
		InputSchema:    map[string]any{"workspace": "string", "command": "string", "stdout": "string", "stderr": "string", "exit_code": "number", "language": "string"},
		DefaultEffects: []string{"read", "code.plan"},
	}, &code.ParseTestFailureExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.fix_test_failure_loop",
		Provider:       "local",
		Name:           "code.fix_test_failure_loop",
		Description:    "Run tests and prepare the next repair-loop action without applying code changes",
		InputSchema:    map[string]any{"workspace": "string", "test_command": "string", "max_iterations": "number", "iteration": "number", "test_runs": "array", "failures": "array", "proposed_patches": "array", "applied_patches": "array", "approval_rejected": "boolean", "stop_on_approval": "boolean", "auto_rerun_tests": "boolean"},
		DefaultEffects: []string{"code.test", "process.read", "fs.read", "code.plan"},
	}, &code.FixTestFailureLoopExecutor{Workspace: workspace, DefaultTimeoutSeconds: cfg.Shell.DefaultTimeoutSeconds, MaxOutputBytes: int64(cfg.Shell.MaxOutputChars), MaxIterations: 3})

	registerGitTools(registry, cfg)
	registerOpsTools(registry, cfg, opsManager)
	registerMemoryTools(registry, memoryStore)
	if cfg.KB.Enabled && knowledge != nil {
		registry.Register(core.ToolSpec{
			ID:          "kb.search",
			Provider:    "local",
			Name:        "kb.search",
			Description: "Search local knowledge base by semantic query",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kb_id":   map[string]any{"type": "string"},
					"query":   map[string]any{"type": "string"},
					"limit":   map[string]any{"type": "integer"},
					"mode":    map[string]any{"type": "string"},
					"rerank":  map[string]any{"type": "boolean"},
					"filters": map[string]any{"type": "object"},
				},
				"required": []string{"query"},
			},
			DefaultEffects: []string{"kb.read"},
		}, &kb.SearchExecutor{Service: knowledge})
		registry.Register(core.ToolSpec{
			ID:          "kb.retrieve",
			Provider:    "local",
			Name:        "kb.retrieve",
			Description: "Retrieve local knowledge chunks with hybrid search, rerank and citations",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kb_id":   map[string]any{"type": "string"},
					"query":   map[string]any{"type": "string"},
					"top_k":   map[string]any{"type": "integer"},
					"mode":    map[string]any{"type": "string"},
					"rerank":  map[string]any{"type": "boolean"},
					"filters": map[string]any{"type": "object"},
				},
				"required": []string{"query"},
			},
			DefaultEffects: []string{"kb.read"},
		}, &kb.RetrieveExecutor{Service: knowledge})
		registry.Register(core.ToolSpec{
			ID:          "kb.answer",
			Provider:    "local",
			Name:        "kb.answer",
			Description: "Answer using local knowledge base evidence and citations",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kb_id":             map[string]any{"type": "string"},
					"query":             map[string]any{"type": "string"},
					"top_k":             map[string]any{"type": "integer"},
					"mode":              map[string]any{"type": "string"},
					"require_citations": map[string]any{"type": "boolean"},
					"rerank":            map[string]any{"type": "boolean"},
					"filters":           map[string]any{"type": "object"},
				},
				"required": []string{"query"},
			},
			DefaultEffects: []string{"kb.read"},
		}, &kb.AnswerExecutor{Service: knowledge})
	}
	registry.Register(core.ToolSpec{
		ID:          "skill.run",
		Provider:    "skill",
		Name:        "skill.run",
		Description: "Run a local registered skill by id",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"skill_id": map[string]any{"type": "string"},
				"args":     map[string]any{"type": "object"},
			},
			"required": []string{"skill_id"},
		},
		DefaultEffects: []string{"unknown.effect"},
	}, &skills.Runner{
		Manager:        skillsManager,
		MaxOutputChars: cfg.Shell.MaxOutputChars,
	})
	registry.Register(core.ToolSpec{
		ID:          "mcp.call_tool",
		Provider:    "mcp",
		Name:        "mcp.call_tool",
		Description: "Call a tool exposed by a configured MCP server",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server_id": map[string]any{"type": "string"},
				"tool_name": map[string]any{"type": "string"},
				"arguments": map[string]any{"type": "object"},
			},
			"required": []string{"server_id", "tool_name"},
		},
		DefaultEffects: []string{"unknown.effect"},
	}, &mcp.CallToolExecutor{Client: mcpManager})

	return registry
}

func registerMemoryTools(registry *toolscore.Registry, memoryStore *memstore.Store) {
	registry.Register(core.ToolSpec{
		ID:             "memory.search",
		Provider:       "local",
		Name:           "memory.search",
		Description:    "Search Markdown memory files",
		InputSchema:    map[string]any{"query": "string", "limit": "number"},
		DefaultEffects: []string{"read", "memory.read"},
	}, &memstore.SearchExecutor{Store: memoryStore})
	registry.Register(core.ToolSpec{
		ID:             "memory.patch",
		Provider:       "local",
		Name:           "memory.patch",
		Description:    "Create a pending Markdown memory patch",
		InputSchema:    map[string]any{"path": "string", "body": "string"},
		DefaultEffects: []string{"fs.write", "memory.modify"},
	}, &memstore.PatchExecutor{Store: memoryStore})
	registry.Register(core.ToolSpec{
		ID:             "memory.extract_candidates",
		Provider:       "local",
		Name:           "memory.extract_candidates",
		Description:    "Extract candidate long-term memories without directly committing them",
		InputSchema:    map[string]any{"conversation_id": "string", "message_id": "string", "text": "string", "project_key": "string", "queue": "boolean"},
		DefaultEffects: []string{"memory.review"},
	}, &memstore.ExtractCandidatesExecutor{Store: memoryStore})
	registry.Register(core.ToolSpec{
		ID:             "memory.detect_conflicts",
		Provider:       "local",
		Name:           "memory.detect_conflicts",
		Description:    "Detect duplicate or conflicting memory candidates",
		InputSchema:    map[string]any{"candidate": "object"},
		DefaultEffects: []string{"read", "memory.read"},
	}, &memstore.DetectConflictsExecutor{Store: memoryStore})
	registry.Register(core.ToolSpec{
		ID:             "memory.merge_candidates",
		Provider:       "local",
		Name:           "memory.merge_candidates",
		Description:    "Suggest memory candidate merge behavior without writing",
		InputSchema:    map[string]any{"candidate": "object"},
		DefaultEffects: []string{"read", "memory.read"},
	}, &memstore.MergeCandidatesExecutor{Store: memoryStore})
	for _, spec := range []struct {
		name        string
		description string
		executor    toolscore.Executor
	}{
		{name: "memory.item_create", description: "Create a Markdown-backed memory item after approval", executor: &memstore.ItemCreateExecutor{Store: memoryStore}},
		{name: "memory.item_update", description: "Update a Markdown-backed memory item after approval", executor: &memstore.ItemUpdateExecutor{Store: memoryStore}},
		{name: "memory.item_archive", description: "Archive a Markdown-backed memory item after approval", executor: &memstore.ItemArchiveExecutor{Store: memoryStore}},
		{name: "memory.item_restore", description: "Restore a Markdown-backed memory item after approval", executor: &memstore.ItemRestoreExecutor{Store: memoryStore}},
		{name: "memory.item_delete", description: "Archive or force-delete a Markdown-backed memory item after approval", executor: &memstore.ItemDeleteExecutor{Store: memoryStore}},
	} {
		registry.Register(core.ToolSpec{
			ID:             spec.name,
			Provider:       "local",
			Name:           spec.name,
			Description:    spec.description,
			InputSchema:    map[string]any{"id": "string", "text": "string", "scope": "string", "type": "string", "project_key": "string", "tags": "array", "fields": "object", "force": "boolean"},
			DefaultEffects: []string{"fs.write", "memory.modify"},
		}, spec.executor)
	}
}

func registerGitTools(registry *toolscore.Registry, cfg config.Config) {
	readEffects := []string{"read", "git.read"}
	writeEffects := []string{"git.write", "fs.write"}
	operations := []struct {
		name        string
		description string
		effects     []string
	}{
		{name: "status", description: "Show git working tree status", effects: readEffects},
		{name: "diff", description: "Show git diff", effects: readEffects},
		{name: "diff_summary", description: "Summarize git diff without modifying the repository", effects: readEffects},
		{name: "commit_message_proposal", description: "Propose a commit message from staged diff without committing", effects: readEffects},
		{name: "log", description: "Show recent git commits", effects: readEffects},
		{name: "branch", description: "Show current git branch", effects: readEffects},
		{name: "add", description: "Stage paths in git", effects: writeEffects},
		{name: "commit", description: "Create a local git commit", effects: writeEffects},
		{name: "restore", description: "Restore paths from git", effects: []string{"git.write", "fs.write", "code.modify"}},
		{name: "clean", description: "Remove untracked files with git clean", effects: []string{"fs.delete", "dangerous"}},
	}
	for _, operation := range operations {
		toolName := "git." + operation.name
		registry.Register(core.ToolSpec{
			ID:             toolName,
			Provider:       "local",
			Name:           toolName,
			Description:    operation.description,
			InputSchema:    map[string]any{"workspace": "string", "paths": "array", "message": "string", "limit": "number", "staged": "boolean"},
			DefaultEffects: append([]string(nil), operation.effects...),
		}, &gittools.Executor{
			Root:           cfg.Owner.DefaultWorkspace,
			Operation:      operation.name,
			TimeoutSeconds: cfg.Shell.DefaultTimeoutSeconds,
			MaxOutputBytes: int64(cfg.Shell.MaxOutputChars),
		})
	}
}

func registerOpsTools(registry *toolscore.Registry, cfg config.Config, opsManager *ops.Manager) {
	localReadEffects := map[string][]string{
		"system_info":    []string{"system.read"},
		"processes":      []string{"process.read", "system.metrics.read"},
		"disk_usage":     []string{"disk.read", "system.metrics.read"},
		"memory_usage":   []string{"memory.read", "system.metrics.read"},
		"network_info":   []string{"network.read"},
		"service_status": []string{"service.read"},
		"logs_tail":      []string{"log.read"},
	}
	for operation, effects := range localReadEffects {
		toolName := "ops.local." + operation
		registry.Register(core.ToolSpec{
			ID:             toolName,
			Provider:       "local",
			Name:           toolName,
			Description:    "Run fixed local ops read operation: " + operation,
			InputSchema:    map[string]any{"path": "string", "service": "string", "max_lines": "number", "max_output_bytes": "number"},
			DefaultEffects: append([]string{"read"}, effects...),
		}, &ops.LocalExecutor{
			Operation:             operation,
			DefaultTimeoutSeconds: cfg.Shell.DefaultTimeoutSeconds,
			MaxOutputBytes:        int64(cfg.Shell.MaxOutputChars),
		})
	}
	registry.Register(core.ToolSpec{
		ID:             "ops.local.service_restart",
		Provider:       "local",
		Name:           "ops.local.service_restart",
		Description:    "Restart a local service after approval",
		InputSchema:    map[string]any{"service": "string", "timeout_seconds": "number"},
		DefaultEffects: []string{"service.restart", "system.write"},
	}, &ops.LocalExecutor{
		Operation:             "service_restart",
		DefaultTimeoutSeconds: cfg.Shell.DefaultTimeoutSeconds,
		MaxOutputBytes:        int64(cfg.Shell.MaxOutputChars),
	})

	sshReadOps := []string{"system_info", "processes", "disk_usage", "memory_usage", "logs_tail", "service_status"}
	for _, operation := range sshReadOps {
		toolName := "ops.ssh." + operation
		registry.Register(core.ToolSpec{
			ID:             toolName,
			Provider:       "local",
			Name:           toolName,
			Description:    "Run fixed SSH ops read operation: " + operation,
			InputSchema:    map[string]any{"host_id": "string", "path": "string", "service": "string", "max_lines": "number", "max_output_bytes": "number"},
			DefaultEffects: []string{"read", "ssh.read"},
		}, &ops.SSHExecutor{
			Manager:               opsManager,
			Operation:             operation,
			DefaultTimeoutSeconds: cfg.Shell.DefaultTimeoutSeconds,
			MaxOutputBytes:        int64(cfg.Shell.MaxOutputChars),
		})
	}
	registry.Register(core.ToolSpec{
		ID:             "ops.ssh.service_restart",
		Provider:       "local",
		Name:           "ops.ssh.service_restart",
		Description:    "Restart a service over SSH after approval",
		InputSchema:    map[string]any{"host_id": "string", "service": "string"},
		DefaultEffects: []string{"ssh.write", "service.restart", "system.write"},
	}, &ops.SSHExecutor{
		Manager:               opsManager,
		Operation:             "service_restart",
		DefaultTimeoutSeconds: cfg.Shell.DefaultTimeoutSeconds,
		MaxOutputBytes:        int64(cfg.Shell.MaxOutputChars),
	})

	for _, operation := range []string{"ps", "inspect", "logs", "stats", "restart", "stop", "start"} {
		toolName := "ops.docker." + operation
		effects := []string{"read", "container.read"}
		if operation == "logs" {
			effects = append(effects, "log.read")
		}
		if operation == "stats" {
			effects = append(effects, "system.metrics.read")
		}
		if operation == "restart" || operation == "stop" || operation == "start" {
			effects = []string{"container.write", "container." + operation}
		}
		registry.Register(core.ToolSpec{
			ID:             toolName,
			Provider:       "local",
			Name:           toolName,
			Description:    "Run fixed Docker ops operation: " + operation,
			InputSchema:    map[string]any{"container": "string", "max_lines": "number", "max_output_bytes": "number"},
			DefaultEffects: effects,
		}, &ops.DockerExecutor{
			Operation:             operation,
			DefaultTimeoutSeconds: cfg.Shell.DefaultTimeoutSeconds,
			MaxOutputBytes:        int64(cfg.Shell.MaxOutputChars),
		})
	}

	for _, operation := range []string{"get", "describe", "logs", "events", "apply", "delete", "rollout_restart"} {
		toolName := "ops.k8s." + operation
		effects := []string{"read", "k8s.read"}
		if operation == "logs" {
			effects = append(effects, "log.read")
		}
		if operation == "apply" || operation == "rollout_restart" {
			effects = []string{"k8s.write"}
		}
		if operation == "delete" {
			effects = []string{"k8s.delete", "dangerous"}
		}
		registry.Register(core.ToolSpec{
			ID:             toolName,
			Provider:       "local",
			Name:           toolName,
			Description:    "Run fixed Kubernetes ops operation: " + operation,
			InputSchema:    map[string]any{"resource": "string", "name": "string", "target": "string", "namespace": "string", "manifest_path": "string", "context": "string"},
			DefaultEffects: effects,
		}, &ops.K8sExecutor{
			Manager:               opsManager,
			Operation:             operation,
			DefaultTimeoutSeconds: cfg.Shell.DefaultTimeoutSeconds,
			MaxOutputBytes:        int64(cfg.Shell.MaxOutputChars),
		})
	}

	runbookSpecs := []struct {
		name        string
		description string
		executor    toolscore.Executor
		effects     []string
	}{
		{name: "list", description: "List local Markdown runbooks", executor: &ops.RunbookListExecutor{Manager: opsManager}, effects: []string{"read", "runbook.read"}},
		{name: "read", description: "Read one local Markdown runbook", executor: &ops.RunbookReadExecutor{Manager: opsManager}, effects: []string{"read", "runbook.read"}},
		{name: "plan", description: "Plan a runbook without executing steps", executor: &ops.RunbookPlanExecutor{Manager: opsManager}, effects: []string{"read", "runbook.read"}},
		{name: "execute_step", description: "Route one runbook step through ToolRouter", executor: &ops.RunbookExecuteStepExecutor{Manager: opsManager}, effects: []string{"runbook.execute", "workflow.route"}},
		{name: "execute", description: "Execute a runbook by routing each step through ToolRouter", executor: &ops.RunbookExecuteExecutor{Manager: opsManager}, effects: []string{"runbook.execute", "workflow.route"}},
	}
	for _, spec := range runbookSpecs {
		toolName := "runbook." + spec.name
		registry.Register(core.ToolSpec{
			ID:             toolName,
			Provider:       "local",
			Name:           toolName,
			Description:    spec.description,
			InputSchema:    map[string]any{"id": "string", "runbook_id": "string", "host_id": "string", "dry_run": "boolean", "max_steps": "number", "step": "object"},
			DefaultEffects: append([]string(nil), spec.effects...),
		}, spec.executor)
	}
}

func resolveConfigPath(path string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	parent := filepath.Join("..", path)
	if _, err := os.Stat(parent); err == nil {
		return parent
	}
	return path
}

func coreShellSpec() core.ToolSpec {
	return core.ToolSpec{
		ID:          "shell.exec",
		Provider:    "local",
		Name:        "shell.exec",
		Description: "Execute a shell command after effect inference and policy checks",
		InputSchema: map[string]any{
			"shell":           "string",
			"command":         "string",
			"cwd":             "string",
			"timeout_seconds": "number",
			"purpose":         "string",
		},
		DefaultEffects: []string{"read"},
	}
}
