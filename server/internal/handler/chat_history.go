package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/integrations/channel"
	"github.com/adanman/goosar/server/internal/integrations/slack"
	"github.com/adanman/goosar/server/internal/logger"
	"github.com/adanman/goosar/server/internal/util"
)

type ChatChannelHistoryReader interface {
	ChannelOverview(ctx context.Context, chatSessionID pgtype.UUID, opts channel.HistoryOptions) (channel.HistoryPage, error)
	Thread(ctx context.Context, chatSessionID pgtype.UUID, threadID string, opts channel.HistoryOptions) (channel.HistoryPage, error)
}

type ChatChannelHistoryResponse struct {
	ChannelType string `json:"channel_type"`

	ThreadID   string                   `json:"thread_id,omitempty"`
	Messages   []channel.HistoryMessage `json:"messages"`
	NextCursor string                   `json:"next_cursor,omitempty"`

	Note string `json:"note,omitempty"`
}

func (h *Handler) GetChatChannelHistory(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := h.chatHistorySession(w, r)
	if !ok {
		return
	}
	if h.SlackHistory == nil {
		h.writeNoChannelIntegration(w)
		return
	}
	page, err := h.SlackHistory.ChannelOverview(r.Context(), sessionID, historyOptionsFrom(r))
	h.respondChatHistory(w, r, sessionID, page, err)
}

func (h *Handler) GetChatThread(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := h.chatHistorySession(w, r)
	if !ok {
		return
	}
	if h.SlackHistory == nil {
		h.writeNoChannelIntegration(w)
		return
	}
	threadID := r.URL.Query().Get("id")
	page, err := h.SlackHistory.Thread(r.Context(), sessionID, threadID, historyOptionsFrom(r))
	h.respondChatHistory(w, r, sessionID, page, err)
}

func (h *Handler) chatHistorySession(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "chat history is only available from within an agent task")
		return pgtype.UUID{}, false
	}
	taskIDHeader := r.Header.Get("X-Task-ID")
	if taskIDHeader == "" {
		writeError(w, http.StatusBadRequest, "missing task context")
		return pgtype.UUID{}, false
	}
	taskUUID, err := util.ParseUUID(taskIDHeader)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return pgtype.UUID{}, false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return pgtype.UUID{}, false
	}
	if !task.ChatSessionID.Valid {
		writeError(w, http.StatusBadRequest, "this task is not a chat task")
		return pgtype.UUID{}, false
	}

	session, err := h.Queries.GetChatSession(r.Context(), task.ChatSessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return pgtype.UUID{}, false
	}
	if ws := ctxWorkspaceID(r.Context()); ws != "" && uuidToString(session.WorkspaceID) != ws {
		writeError(w, http.StatusForbidden, "chat session does not belong to this workspace")
		return pgtype.UUID{}, false
	}
	return task.ChatSessionID, true
}

func (h *Handler) respondChatHistory(w http.ResponseWriter, r *http.Request, sessionID pgtype.UUID, page channel.HistoryPage, err error) {
	if err != nil {
		if errors.Is(err, slack.ErrNoSlackSession) {
			writeJSON(w, http.StatusOK, ChatChannelHistoryResponse{
				Messages: []channel.HistoryMessage{},
				Note:     "This conversation is not connected to a chat channel, so there is no channel history to read.",
			})
			return
		}
		slog.Error("chat channel history read failed", append(logger.RequestAttrs(r),
			"error", err, "chat_session_id", uuidToString(sessionID))...)
		writeError(w, http.StatusBadGateway, "failed to read channel history")
		return
	}
	messages := page.Messages
	if messages == nil {
		messages = []channel.HistoryMessage{}
	}
	writeJSON(w, http.StatusOK, ChatChannelHistoryResponse{
		ChannelType: page.ChannelType,
		ThreadID:    page.ThreadID,
		Messages:    messages,
		NextCursor:  page.NextCursor,
	})
}

func (h *Handler) writeNoChannelIntegration(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, ChatChannelHistoryResponse{
		Messages: []channel.HistoryMessage{},
		Note:     "No chat channel integration is configured on this server.",
	})
}

func historyOptionsFrom(r *http.Request) channel.HistoryOptions {
	return channel.HistoryOptions{
		Limit:  parseHistoryLimit(r.URL.Query().Get("limit")),
		Before: r.URL.Query().Get("before"),
	}
}

func parseHistoryLimit(raw string) int {
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
