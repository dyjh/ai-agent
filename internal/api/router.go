package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"local-agent/internal/api/handlers"
	"local-agent/internal/api/ws"
)

// NewRouter builds the HTTP router.
func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	health := handlers.NewHealthHandler(deps)
	conversations := handlers.NewConversationsHandler(deps)
	approvals := handlers.NewApprovalsHandler(deps)
	runs := handlers.NewRunsHandler(deps)
	memory := handlers.NewMemoryHandler(deps)
	knowledge := handlers.NewKnowledgeHandler(deps)
	skillsHandler := handlers.NewSkillsHandler(deps)
	mcpHandler := handlers.NewMCPHandler(deps)
	docsHandler := handlers.NewDocsHandler()
	wsHandler := ws.NewChatHandler(deps)

	r.Get("/swagger", docsHandler.Redirect)
	r.Get("/swagger/index.html", docsHandler.Index)
	r.Get("/swagger/doc.json", docsHandler.DocJSON)

	r.Route("/v1", func(r chi.Router) {
		r.Get("/health", health.Get)

		r.Post("/conversations", conversations.CreateConversation)
		r.Get("/conversations", conversations.ListConversations)
		r.Get("/conversations/{conversation_id}", conversations.GetConversation)
		r.Get("/conversations/{conversation_id}/messages", conversations.ListMessages)
		r.Post("/conversations/{conversation_id}/messages", conversations.PostMessage)
		r.Get("/conversations/{conversation_id}/ws", wsHandler.ServeHTTP)

		r.Get("/approvals/pending", approvals.Pending)
		r.Post("/approvals/{approval_id}/approve", approvals.Approve)
		r.Post("/approvals/{approval_id}/reject", approvals.Reject)

		r.Get("/runs", runs.List)
		r.Get("/runs/{run_id}", runs.Get)
		r.Get("/runs/{run_id}/steps", runs.Steps)
		r.Post("/runs/{run_id}/resume", runs.Resume)
		r.Post("/runs/{run_id}/cancel", runs.Cancel)

		r.Get("/memory/files", memory.ListFiles)
		r.Get("/memory/files/*", memory.GetFile)
		r.Post("/memory/search", memory.Search)
		r.Post("/memory/patches", memory.CreatePatch)
		r.Post("/memory/reindex", memory.Reindex)

		r.Get("/kbs/health", knowledge.Health)
		r.Post("/kbs", knowledge.CreateKB)
		r.Get("/kbs", knowledge.ListKBs)
		r.Post("/kbs/{kb_id}/documents/upload", knowledge.Upload)
		r.Post("/kbs/{kb_id}/search", knowledge.Search)

		r.Post("/skills/upload", skillsHandler.Upload)
		r.Get("/skills", skillsHandler.List)
		r.Get("/skills/{id}", skillsHandler.Get)
		r.Post("/skills/{id}/enable", skillsHandler.Enable)
		r.Post("/skills/{id}/disable", skillsHandler.Disable)
		r.Post("/skills/{id}/test", skillsHandler.Test)
		r.Post("/skills/{id}/run", skillsHandler.Run)

		r.Get("/mcp/servers", mcpHandler.ListServers)
		r.Post("/mcp/servers", mcpHandler.CreateServer)
		r.Get("/mcp/servers/{id}", mcpHandler.GetServer)
		r.Patch("/mcp/servers/{id}", mcpHandler.UpdateServer)
		r.Post("/mcp/servers/{id}/refresh", mcpHandler.RefreshServer)
		r.Post("/mcp/servers/{id}/test", mcpHandler.TestServer)
		r.Post("/mcp/servers/{id}/tools/{tool_name}/call", mcpHandler.CallTool)
		r.Get("/mcp/tools", mcpHandler.ListPolicies)
		r.Patch("/mcp/tools/{id}/policy", mcpHandler.UpdatePolicy)
	})

	return r
}
