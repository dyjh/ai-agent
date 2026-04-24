package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"

	apidocs "local-agent/docs"
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
	opsHandler := handlers.NewOpsHandler(deps)
	wsHandler := ws.NewChatHandler(deps)

	apidocs.SwaggerInfo.BasePath = "/"
	r.Get("/swagger", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/swagger/index.html", http.StatusMovedPermanently)
	})
	r.Handle("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
		httpSwagger.DocExpansion("list"),
		httpSwagger.DomID("swagger-ui"),
	))

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
		r.Post("/kbs/{kb_id}/retrieve", knowledge.Retrieve)
		r.Post("/kbs/{kb_id}/answer", knowledge.Answer)
		r.Post("/kbs/{kb_id}/sources", knowledge.CreateSource)
		r.Get("/kbs/{kb_id}/sources", knowledge.ListSources)
		r.Get("/kbs/{kb_id}/sources/{source_id}", knowledge.GetSource)
		r.Patch("/kbs/{kb_id}/sources/{source_id}", knowledge.UpdateSource)
		r.Delete("/kbs/{kb_id}/sources/{source_id}", knowledge.DeleteSource)
		r.Post("/kbs/{kb_id}/sources/{source_id}/sync", knowledge.SyncSource)
		r.Get("/kbs/{kb_id}/index-jobs", knowledge.ListIndexJobs)
		r.Get("/kbs/{kb_id}/index-jobs/{job_id}", knowledge.GetIndexJob)
		r.Get("/rag/evals", knowledge.ListRAGEvals)
		r.Post("/rag/evals", knowledge.CreateRAGEval)
		r.Post("/rag/evals/run", knowledge.RunRAGEval)
		r.Get("/rag/evals/runs/{run_id}", knowledge.GetRAGEvalRun)

		r.Post("/skills/upload", skillsHandler.Upload)
		r.Post("/skills/upload-zip", skillsHandler.UploadZip)
		r.Get("/skills", skillsHandler.List)
		r.Get("/skills/{id}", skillsHandler.Get)
		r.Delete("/skills/{id}", skillsHandler.Remove)
		r.Get("/skills/{id}/manifest", skillsHandler.Manifest)
		r.Get("/skills/{id}/package", skillsHandler.Package)
		r.Post("/skills/{id}/enable", skillsHandler.Enable)
		r.Post("/skills/{id}/disable", skillsHandler.Disable)
		r.Post("/skills/{id}/test", skillsHandler.Test)
		r.Post("/skills/{id}/validate", skillsHandler.Validate)
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

		r.Get("/ops/hosts", opsHandler.ListHosts)
		r.Post("/ops/hosts", opsHandler.CreateHost)
		r.Get("/ops/hosts/{host_id}", opsHandler.GetHost)
		r.Patch("/ops/hosts/{host_id}", opsHandler.UpdateHost)
		r.Delete("/ops/hosts/{host_id}", opsHandler.DeleteHost)
		r.Post("/ops/hosts/{host_id}/test", opsHandler.TestHost)
		r.Get("/ops/runbooks", opsHandler.ListRunbooks)
		r.Get("/ops/runbooks/{id}", opsHandler.ReadRunbook)
		r.Post("/ops/runbooks/{id}/plan", opsHandler.PlanRunbook)
		r.Post("/ops/runbooks/{id}/execute", opsHandler.ExecuteRunbook)
	})

	return r
}
