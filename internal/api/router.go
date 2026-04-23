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
	memory := handlers.NewMemoryHandler(deps)
	knowledge := handlers.NewKnowledgeHandler(deps)
	skillsHandler := handlers.NewSkillsHandler(deps)
	mcpHandler := handlers.NewMCPHandler(deps)
	wsHandler := ws.NewChatHandler(deps)

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
		r.Post("/skills/{id}/enable", skillsHandler.Enable)
		r.Post("/skills/{id}/disable", skillsHandler.Disable)

		r.Get("/mcp/servers", mcpHandler.ListServers)
		r.Post("/mcp/servers", mcpHandler.CreateServer)
		r.Patch("/mcp/servers/{id}", mcpHandler.UpdateServer)
		r.Get("/mcp/tools", mcpHandler.ListPolicies)
		r.Patch("/mcp/tools/{id}/policy", mcpHandler.UpdatePolicy)
	})

	return r
}
