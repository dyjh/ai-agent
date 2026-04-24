package handlers

import (
	"log/slog"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/db/repo"
	toolscore "local-agent/internal/tools"
	"local-agent/internal/tools/kb"
	"local-agent/internal/tools/mcp"
	memstore "local-agent/internal/tools/memory"
	"local-agent/internal/tools/ops"
	"local-agent/internal/tools/skills"
)

// Dependencies is the service container for HTTP and WebSocket handlers.
type Dependencies struct {
	Logger    *slog.Logger
	Config    config.Config
	Store     *repo.Store
	Runtime   *agent.Runtime
	Approvals *agent.ApprovalCenter
	Router    toolscore.Router
	Memory    *memstore.Store
	Knowledge *kb.Service
	Skills    *skills.Manager
	MCP       *mcp.Manager
	Ops       *ops.Manager
}
