package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"local-agent/internal/agent"
	"local-agent/internal/core"
	"local-agent/internal/ids"
)

// ConversationsHandler serves conversation and message APIs.
type ConversationsHandler struct {
	Base
}

// NewConversationsHandler creates a conversations handler.
func NewConversationsHandler(deps Dependencies) *ConversationsHandler {
	return &ConversationsHandler{Base{Deps: deps}}
}

// CreateConversation handles POST /v1/conversations.
// @Tags Conversations
// @Summary Create conversation
// @Accept application/json
// @Produce application/json
// @Param body body CreateConversationRequest false "Conversation payload"
// @Success 201 {object} core.Conversation
// @Failure 500 {object} LegacyErrorResponse
// @Router /v1/conversations [post]
func (h *ConversationsHandler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	var body CreateConversationRequest
	_ = decodeJSON(r, &body)

	item := core.Conversation{
		ID:         ids.New("conv"),
		Title:      body.Title,
		ProjectKey: body.ProjectKey,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := h.Deps.Store.Conversations.CreateConversation(r.Context(), item); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// ListConversations handles GET /v1/conversations.
// @Tags Conversations
// @Summary List conversations
// @Produce application/json
// @Success 200 {object} ConversationListResponse
// @Failure 500 {object} LegacyErrorResponse
// @Router /v1/conversations [get]
func (h *ConversationsHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	items, err := h.Deps.Store.Conversations.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, ConversationListResponse{Items: items})
}

// GetConversation handles GET /v1/conversations/{conversation_id}.
// @Tags Conversations
// @Summary Get conversation
// @Produce application/json
// @Param conversation_id path string true "Conversation ID"
// @Success 200 {object} core.Conversation
// @Failure 404 {object} LegacyErrorResponse
// @Router /v1/conversations/{conversation_id} [get]
func (h *ConversationsHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	item, err := h.Deps.Store.Conversations.Get(r.Context(), chi.URLParam(r, "conversation_id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// ListMessages handles GET /v1/conversations/{conversation_id}/messages.
// @Tags Conversations
// @Summary List conversation messages
// @Produce application/json
// @Param conversation_id path string true "Conversation ID"
// @Success 200 {object} MessageListResponse
// @Failure 500 {object} LegacyErrorResponse
// @Router /v1/conversations/{conversation_id}/messages [get]
func (h *ConversationsHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	items, err := h.Deps.Store.Messages.ListByConversation(r.Context(), chi.URLParam(r, "conversation_id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, MessageListResponse{Items: items})
}

// PostMessage handles POST /v1/conversations/{conversation_id}/messages.
// @Tags Conversations
// @Summary Post user message
// @Accept application/json
// @Produce application/json
// @Param conversation_id path string true "Conversation ID"
// @Param body body PostMessageRequest true "User message"
// @Success 200 {object} agent.RunResponse
// @Failure 400 {object} LegacyErrorResponse
// @Failure 500 {object} LegacyErrorResponse
// @Router /v1/conversations/{conversation_id}/messages [post]
func (h *ConversationsHandler) PostMessage(w http.ResponseWriter, r *http.Request) {
	var body PostMessageRequest
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	response, err := h.Deps.Runtime.Run(r.Context(), agent.RunRequest{
		ConversationID: chi.URLParam(r, "conversation_id"),
		Content:        body.Content,
	}, nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, response)
}
