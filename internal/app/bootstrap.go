package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
	"local-agent/internal/db"
	"local-agent/internal/db/repo"
	"local-agent/internal/einoapp"
	"local-agent/internal/events"
	toolscore "local-agent/internal/tools"
	"local-agent/internal/tools/code"
	"local-agent/internal/tools/kb"
	"local-agent/internal/tools/mcp"
	memstore "local-agent/internal/tools/memory"
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
}

// NewBootstrap wires the base application dependencies.
func NewBootstrap(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Bootstrap, error) {
	_ = os.MkdirAll(cfg.Memory.RootDir, 0o755)
	_ = os.MkdirAll(cfg.Events.JSONLRoot, 0o755)
	_ = os.MkdirAll(cfg.Events.AuditRoot, 0o755)
	_ = os.MkdirAll("skills", 0o755)

	store := repo.NewMemoryStore()
	if cfg.Database.URL != "" {
		pool, err := db.Open(ctx, cfg.Database.URL)
		if err != nil {
			logger.Warn("postgres unavailable, falling back to memory store", "error", err)
		} else if pgStore := repo.NewPostgresStore(pool); pgStore != nil {
			store = pgStore
		}
	}

	embedder := kb.FakeEmbedder{Dimensions: cfg.Vector.EmbeddingDimension}
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

	knowledgeService := kb.NewService(index, embedder, cfg.CollectionName("kb"))
	skillsManager := skills.NewManager("skills")
	mcpManager := mcp.NewManager()
	if err := mcpManager.LoadConfig(resolveConfigPath("config/mcp.servers.yaml"), resolveConfigPath("config/mcp.tool-policies.yaml")); err != nil {
		return nil, err
	}
	approvals := agent.NewApprovalCenter()
	eventWriter := events.NewJSONLWriter(cfg.Events.JSONLRoot, cfg.Events.AuditRoot)
	registry := registerTools(cfg, memoryStore, knowledgeService, skillsManager, mcpManager)
	router := toolscore.NewRouter(
		registry,
		agent.NewEffectInferrer(cfg.Policy, skillsManager, mcpManager),
		agent.NewPolicyEngine(cfg.Policy),
		approvals,
		eventWriter,
	)

	model, err := einoapp.NewChatModel(ctx, cfg.LLM)
	if err != nil {
		return nil, err
	}

	runtime := &agent.Runtime{
		Store:      store,
		Planner:    agent.HeuristicPlanner{},
		Runner:     einoapp.Runner{Model: model},
		Approvals:  approvals,
		Events:     eventWriter,
		StateStore: agent.NewRunStateStore(),
		ContextBuilder: &agent.ContextBuilder{
			Store:       store,
			Memory:      memoryStore,
			Knowledge:   knowledgeService,
			ToolCatalog: registry,
			MaxChars:    8000,
		},
		Router: router,
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
	}, nil
}

func registerTools(cfg config.Config, memoryStore *memstore.Store, knowledge *kb.Service, skillsManager *skills.Manager, mcpManager *mcp.Manager) *toolscore.Registry {
	registry := toolscore.NewRegistry()
	workspace := code.Workspace{Root: cfg.Owner.DefaultWorkspace}

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
		InputSchema:    map[string]any{"path": "string"},
		DefaultEffects: []string{"read", "code.read"},
	}, &code.ReadExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.search",
		Provider:       "local",
		Name:           "code.search",
		Description:    "Search for text in the workspace",
		InputSchema:    map[string]any{"path": "string", "query": "string"},
		DefaultEffects: []string{"read", "code.read"},
	}, &code.SearchExecutor{Workspace: workspace})
	registry.Register(core.ToolSpec{
		ID:             "code.propose_patch",
		Provider:       "local",
		Name:           "code.propose_patch",
		Description:    "Preview a code patch without applying it",
		InputSchema:    map[string]any{"path": "string", "content": "string"},
		DefaultEffects: []string{"read", "code.plan"},
	}, &code.ProposePatchExecutor{})
	registry.Register(core.ToolSpec{
		ID:             "code.apply_patch",
		Provider:       "local",
		Name:           "code.apply_patch",
		Description:    "Apply a patch inside the workspace",
		InputSchema:    map[string]any{"path": "string", "content": "string"},
		DefaultEffects: []string{"fs.write", "code.modify"},
	}, &code.ApplyPatchExecutor{Workspace: workspace})
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
				"filters": map[string]any{"type": "object"},
			},
			"required": []string{"query"},
		},
		DefaultEffects: []string{"kb.read"},
	}, &kb.SearchExecutor{Service: knowledge})
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
	}, &skills.Runner{Manager: skillsManager, MaxOutputChars: cfg.Shell.MaxOutputChars})
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
