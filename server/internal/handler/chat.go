package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/middleware"
	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

const chatSessionTitleMaxLen = 200

type CreateChatSessionRequest struct {
	AgentID   string  `json:"agent_id"`
	Title     string  `json:"title"`
	ProjectID *string `json:"project_id"`
}

func (h *Handler) CreateChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req CreateChatSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	projectID := pgtype.UUID{Valid: false}
	if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" {
		projectID, ok = parseUUIDOrBadRequest(w, strings.TrimSpace(*req.ProjectID), "project_id")
		if !ok {
			return
		}
	}

	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentID,
		WorkspaceID: workspaceUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "agent is archived")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.canInvokeAgent(r.Context(), agent, actorType, actorID, h.invokeAuthorityFromRequest(r, actorType, actorID, noInvokeScope), workspaceID) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), workspaceUUID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lock workspace")
		return
	}
	if projectID.Valid {
		if _, err := qtx.LockProjectForChatSessionCreate(r.Context(), db.LockProjectForChatSessionCreateParams{
			ID:          projectID,
			WorkspaceID: workspaceUUID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "project not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to lock project")
			return
		}
	}

	session, err := qtx.CreateChatSession(r.Context(), db.CreateChatSessionParams{
		WorkspaceID: workspaceUUID,
		AgentID:     agentID,
		CreatorID:   parseUUID(userID),
		Title:       req.Title,
		ProjectID:   projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create chat session")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit chat session create")
		return
	}

	writeJSON(w, http.StatusCreated, chatSessionToResponse(session))
}

func (h *Handler) ListChatSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}

	status := r.URL.Query().Get("status")

	var resp []ChatSessionResponse
	if status == "all" {
		rows, err := h.Queries.ListAllChatSessionsByCreator(r.Context(), db.ListAllChatSessionsByCreatorParams{
			WorkspaceID: parseUUID(workspaceID),
			CreatorID:   parseUUID(userID),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list chat sessions")
			return
		}
		resp = make([]ChatSessionResponse, 0, len(rows))
		for _, s := range rows {
			if _, ok := allowed[uuidToString(s.AgentID)]; !ok {
				continue
			}
			resp = append(resp, ChatSessionResponse{
				ID:          uuidToString(s.ID),
				WorkspaceID: uuidToString(s.WorkspaceID),
				AgentID:     uuidToString(s.AgentID),
				CreatorID:   uuidToString(s.CreatorID),
				ProjectID:   uuidToPtr(s.ProjectID),
				Title:       s.Title,
				Status:      s.Status,
				HasUnread:   s.UnreadCount > 0,
				UnreadCount: int(s.UnreadCount),
				LastMessage: buildChatLastMessage(s.LastMessageAt, s.LastMessageContent, s.LastMessageRole, s.LastMessageFailureReason, s.LastMessageKind),
				Pinned:      s.PinnedAt.Valid,
				CreatedAt:   timestampToString(s.CreatedAt),
				UpdatedAt:   timestampToString(s.UpdatedAt),
			})
		}
	} else {
		rows, err := h.Queries.ListChatSessionsByCreator(r.Context(), db.ListChatSessionsByCreatorParams{
			WorkspaceID: parseUUID(workspaceID),
			CreatorID:   parseUUID(userID),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list chat sessions")
			return
		}
		resp = make([]ChatSessionResponse, 0, len(rows))
		for _, s := range rows {
			if _, ok := allowed[uuidToString(s.AgentID)]; !ok {
				continue
			}
			resp = append(resp, ChatSessionResponse{
				ID:          uuidToString(s.ID),
				WorkspaceID: uuidToString(s.WorkspaceID),
				AgentID:     uuidToString(s.AgentID),
				CreatorID:   uuidToString(s.CreatorID),
				ProjectID:   uuidToPtr(s.ProjectID),
				Title:       s.Title,
				Status:      s.Status,
				HasUnread:   s.UnreadCount > 0,
				UnreadCount: int(s.UnreadCount),
				LastMessage: buildChatLastMessage(s.LastMessageAt, s.LastMessageContent, s.LastMessageRole, s.LastMessageFailureReason, s.LastMessageKind),
				Pinned:      s.PinnedAt.Valid,
				CreatedAt:   timestampToString(s.CreatedAt),
				UpdatedAt:   timestampToString(s.UpdatedAt),
			})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) loadChatSessionForUser(w http.ResponseWriter, r *http.Request, userID, workspaceID, sessionID string) (db.ChatSession, bool) {
	sessionUUID, ok := parseUUIDOrBadRequest(w, sessionID, "chat session id")
	if !ok {
		return db.ChatSession{}, false
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.ChatSession{}, false
	}
	session, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
		ID:          sessionUUID,
		WorkspaceID: workspaceUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return db.ChatSession{}, false
	}
	if uuidToString(session.CreatorID) != userID {
		writeError(w, http.StatusForbidden, "not your chat session")
		return db.ChatSession{}, false
	}
	return session, true
}

func (h *Handler) gateChatSessionForUser(w http.ResponseWriter, r *http.Request, userID, workspaceID, sessionID string) (db.ChatSession, bool) {
	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return db.ChatSession{}, false
	}
	agent, err := h.Queries.GetAgent(r.Context(), session.AgentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return db.ChatSession{}, false
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return db.ChatSession{}, false
	}
	return session, true
}

func (h *Handler) GetChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, chatSessionToResponse(session))
}

type UpdateChatSessionRequest struct {
	Title     *string         `json:"title"`
	ProjectID json.RawMessage `json:"project_id"`
}

func (h *Handler) UpdateChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	var req UpdateChatSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	hasTitle := req.Title != nil
	hasProjectID := req.ProjectID != nil
	if hasTitle == hasProjectID {
		writeError(w, http.StatusBadRequest, "exactly one of title or project_id is required")
		return
	}

	session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	var (
		updated db.ChatSession
		err     error
	)
	var projectIDChanged bool
	if hasTitle {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "title is required")
			return
		}
		if len([]rune(title)) > chatSessionTitleMaxLen {
			writeError(w, http.StatusBadRequest, "title is too long")
			return
		}
		updated, err = h.Queries.UpdateChatSessionTitle(r.Context(), db.UpdateChatSessionTitleParams{
			ID:    session.ID,
			Title: title,
		})
	} else {
		projectID := pgtype.UUID{Valid: false}
		if string(req.ProjectID) != "null" {
			var rawProjectID string
			if err := json.Unmarshal(req.ProjectID, &rawProjectID); err != nil {
				writeError(w, http.StatusBadRequest, "project_id must be a UUID or null")
				return
			}
			rawProjectID = strings.TrimSpace(rawProjectID)
			if rawProjectID == "" {
				writeError(w, http.StatusBadRequest, "project_id must be a UUID or null")
				return
			}
			projectID, ok = parseUUIDOrBadRequest(w, rawProjectID, "project_id")
			if !ok {
				return
			}
		}

		tx, txErr := h.TxStarter.Begin(r.Context())
		if txErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to start transaction")
			return
		}
		defer tx.Rollback(r.Context())
		qtx := h.Queries.WithTx(tx)

		if projectID.Valid {
			if _, lockErr := qtx.LockProjectForChatSessionCreate(r.Context(), db.LockProjectForChatSessionCreateParams{
				ID:          projectID,
				WorkspaceID: session.WorkspaceID,
			}); lockErr != nil {
				if errors.Is(lockErr, pgx.ErrNoRows) {
					writeError(w, http.StatusNotFound, "project not found")
					return
				}
				writeError(w, http.StatusInternalServerError, "failed to lock project")
				return
			}
		}

		updated, err = qtx.UpdateChatSessionProject(r.Context(), db.UpdateChatSessionProjectParams{
			ID:          session.ID,
			WorkspaceID: session.WorkspaceID,
			ProjectID:   projectID,
		})
		if err == nil {
			err = tx.Commit(r.Context())
		}
		projectIDChanged = true
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update chat session")
		return
	}

	resolvedSessionID := uuidToString(updated.ID)
	payload := protocol.ChatSessionUpdatedPayload{
		ChatSessionID: resolvedSessionID,
		Title:         updated.Title,
		UpdatedAt:     timestampToString(updated.UpdatedAt),
	}
	if projectIDChanged {
		projectID := uuidToPtr(updated.ProjectID)
		payload.ProjectID = &projectID
	}
	h.publishChat(protocol.EventChatSessionUpdated, workspaceID, "member", userID, resolvedSessionID, payload)

	writeJSON(w, http.StatusOK, chatSessionToResponse(updated))
}

type SetChatSessionPinnedRequest struct {
	Pinned bool `json:"pinned"`
}

func (h *Handler) SetChatSessionPinned(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	var req SetChatSessionPinnedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	updated, err := h.Queries.SetChatSessionPinned(r.Context(), db.SetChatSessionPinnedParams{
		ID:     session.ID,
		Pinned: req.Pinned,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update chat session")
		return
	}

	resolvedSessionID := uuidToString(updated.ID)
	pinned := updated.PinnedAt.Valid
	h.publishChat(protocol.EventChatSessionUpdated, workspaceID, "member", userID, resolvedSessionID, protocol.ChatSessionUpdatedPayload{
		ChatSessionID: resolvedSessionID,
		Title:         updated.Title,
		Pinned:        &pinned,
		UpdatedAt:     timestampToString(updated.UpdatedAt),
	})

	writeJSON(w, http.StatusOK, chatSessionToResponse(updated))
}

type SetChatSessionArchivedRequest struct {
	Archived bool `json:"archived"`
}

func (h *Handler) SetChatSessionArchived(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	var req SetChatSessionArchivedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	updated, err := qtx.SetChatSessionArchived(r.Context(), db.SetChatSessionArchivedParams{
		ID:       session.ID,
		Archived: req.Archived,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update chat session")
		return
	}

	if req.Archived {
		if err := qtx.DeleteChannelChatSessionBindingBySession(r.Context(), session.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clear chat session channel binding")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("commit chat session archive failed", "session_id", sessionID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to commit chat session update")
		return
	}

	resolvedSessionID := uuidToString(updated.ID)
	status := updated.Status
	h.publishChat(protocol.EventChatSessionUpdated, workspaceID, "member", userID, resolvedSessionID, protocol.ChatSessionUpdatedPayload{
		ChatSessionID: resolvedSessionID,
		Title:         updated.Title,
		Status:        &status,
		UpdatedAt:     timestampToString(updated.UpdatedAt),
	})

	writeJSON(w, http.StatusOK, chatSessionToResponse(updated))
}

func (h *Handler) DeleteChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockChatSessionForDelete(r.Context(), session.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {

			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lock chat session")
		return
	}

	cancelled, err := qtx.CancelAgentTasksByChatSession(r.Context(), session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel chat session tasks")
		return
	}

	if err := qtx.DeleteChannelChatSessionBindingBySession(r.Context(), session.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete chat session binding")
		return
	}

	if err := qtx.DeleteChannelOutboundCardMessagesBySession(r.Context(), session.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete chat session outbound cards")
		return
	}

	if err := qtx.DeleteChatDraftRestoresBySession(r.Context(), session.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete chat session draft restores")
		return
	}

	if err := qtx.DeleteChatSession(r.Context(), db.DeleteChatSessionParams{
		ID:          session.ID,
		WorkspaceID: session.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete chat session")
		return
	}
	if err := qtx.DeleteAgentLabelAssignmentsByAgent(r.Context(), session.AgentID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove chat session agent label assignments")
		return
	}
	if err := qtx.DeleteSystemAgentByID(r.Context(), session.AgentID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up chat session agent")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("commit chat session delete failed", "session_id", sessionID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to commit chat session delete")
		return
	}

	h.TaskService.BroadcastCancelledTasks(r.Context(), cancelled)

	resolvedSessionID := uuidToString(session.ID)
	h.publishChat(protocol.EventChatSessionDeleted, workspaceID, "member", userID, resolvedSessionID, protocol.ChatSessionDeletedPayload{
		ChatSessionID: resolvedSessionID,
	})

	w.WriteHeader(http.StatusNoContent)
}

type SendChatMessageRequest struct {
	Content       string   `json:"content"`
	AttachmentIDs []string `json:"attachment_ids"`
}

type SendChatMessageResponse struct {
	MessageID string `json:"message_id"`
	TaskID    string `json:"task_id"`

	AttachmentIDs []string `json:"attachment_ids"`

	CreatedAt string `json:"created_at"`
}

func (h *Handler) SendChatMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	var req SendChatMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	attachmentIDs, ok := parseUUIDSliceOrBadRequest(w, req.AttachmentIDs, "attachment_ids")
	if !ok {
		return
	}

	session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	if session.Status != "active" {
		writeError(w, http.StatusBadRequest, "chat session is archived")
		return
	}

	agent, err := h.Queries.GetAgent(r.Context(), session.AgentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load chat agent")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "chat agent is archived")
		return
	}
	if !agent.RuntimeID.Valid {
		writeError(w, http.StatusConflict, "chat agent has no runtime")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.canInvokeAgent(r.Context(), agent, actorType, actorID, h.invokeAuthorityFromRequest(r, actorType, actorID, chatSessionInvokeScope(session.ID)), workspaceID) {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonInvocationNotAllowed)
		return
	}

	hadUserMessage := true
	if existed, err := h.Queries.ChatSessionHasUserMessage(r.Context(), session.ID); err == nil {
		hadUserMessage = existed
	}

	sent, err := h.TaskService.SendDirectChatMessage(r.Context(), session, agent, parseUUID(userID), req.Content, attachmentIDs, actorType, parseUUID(actorID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send chat message: "+err.Error())
		return
	}
	msg := sent.Message
	task := sent.Task

	var boundAttachmentIDs []string
	if len(attachmentIDs) > 0 {
		boundAttachmentIDs = make([]string, 0, len(sent.BoundAttachmentIDs))
		for _, id := range sent.BoundAttachmentIDs {
			boundAttachmentIDs = append(boundAttachmentIDs, uuidToString(id))
		}
	}

	taskContext := h.TaskService.AnalyticsContextForTask(r.Context(), task)
	platform, _, _ := middleware.ClientMetadataFromContext(r.Context())
	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.ChatMessageSent(
		userID,
		workspaceID,
		uuidToString(session.ID),
		uuidToString(task.ID),
		uuidToString(session.AgentID),
		taskContext.RuntimeMode,
		taskContext.Provider,
		platform,
	))

	resolvedSessionID := uuidToString(session.ID)
	h.publishChat(protocol.EventChatMessage, workspaceID, "member", userID, resolvedSessionID, protocol.ChatMessagePayload{
		ChatSessionID: resolvedSessionID,
		MessageID:     uuidToString(msg.ID),
		Role:          "user",
		Content:       req.Content,
		TaskID:        uuidToString(task.ID),
		CreatedAt:     timestampToString(msg.CreatedAt),
	})

	if !hadUserMessage {
		h.maybeGenerateChatTitleAsync(workspaceID, userID, session.ID, session.Title, req.Content)
	}

	writeJSON(w, http.StatusCreated, SendChatMessageResponse{
		MessageID:     uuidToString(msg.ID),
		TaskID:        uuidToString(task.ID),
		CreatedAt:     timestampToString(task.CreatedAt),
		AttachmentIDs: boundAttachmentIDs,
	})
}

type ChatMessagesCursorResponse struct {
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

type ChatMessagesPageResponse struct {
	Messages   []ChatMessageResponse       `json:"messages"`
	Limit      int                         `json:"limit"`
	HasMore    bool                        `json:"has_more"`
	NextCursor *ChatMessagesCursorResponse `json:"next_cursor,omitempty"`
}

func parseChatMessagesPageParams(r *http.Request) (int, pgtype.Timestamptz, pgtype.UUID, error) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			return 0, pgtype.Timestamptz{}, pgtype.UUID{}, errors.New("invalid limit")
		}
		limit = parsed
	}

	rawBeforeCreatedAt := r.URL.Query().Get("before_created_at")
	rawBeforeID := r.URL.Query().Get("before_id")
	if rawBeforeCreatedAt == "" && rawBeforeID == "" {
		return limit, pgtype.Timestamptz{}, pgtype.UUID{}, nil
	}
	if rawBeforeCreatedAt == "" || rawBeforeID == "" {
		return 0, pgtype.Timestamptz{}, pgtype.UUID{}, errors.New("invalid cursor")
	}
	beforeTime, err := time.Parse(time.RFC3339Nano, rawBeforeCreatedAt)
	if err != nil {
		return 0, pgtype.Timestamptz{}, pgtype.UUID{}, errors.New("invalid cursor")
	}
	beforeID, err := util.ParseUUID(rawBeforeID)
	if err != nil {
		return 0, pgtype.Timestamptz{}, pgtype.UUID{}, errors.New("invalid cursor")
	}
	return limit, pgtype.Timestamptz{Time: beforeTime, Valid: true}, beforeID, nil
}

func (h *Handler) ListChatMessages(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	messages, err := h.Queries.ListChatMessages(r.Context(), session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chat messages")
		return
	}

	messageIDs := make([]pgtype.UUID, len(messages))
	for i, m := range messages {
		messageIDs[i] = m.ID
	}
	groupedAtt := h.groupChatMessageAttachments(r.Context(), workspaceID, messageIDs)

	resp := make([]ChatMessageResponse, len(messages))
	for i, m := range messages {
		resp[i] = chatMessageToResponse(m, groupedAtt[uuidToString(m.ID)])
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListChatMessagesPage(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	limit, beforeCreatedAt, beforeID, err := parseChatMessagesPageParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	messages, err := h.Queries.ListChatMessagesPage(r.Context(), db.ListChatMessagesPageParams{
		ChatSessionID:   session.ID,
		Limit:           int32(limit + 1),
		BeforeCreatedAt: beforeCreatedAt,
		BeforeID:        beforeID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chat messages")
		return
	}
	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}
	var nextCursor *ChatMessagesCursorResponse
	if hasMore && len(messages) > 0 {
		oldest := messages[len(messages)-1]
		nextCursor = &ChatMessagesCursorResponse{
			CreatedAt: oldest.CreatedAt.Time.Format(time.RFC3339Nano),
			ID:        uuidToString(oldest.ID),
		}
	}

	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	messageIDs := make([]pgtype.UUID, len(messages))
	for i, m := range messages {
		messageIDs[i] = m.ID
	}
	groupedAtt := h.groupChatMessageAttachments(r.Context(), workspaceID, messageIDs)

	resp := make([]ChatMessageResponse, len(messages))
	for i, m := range messages {
		resp[i] = chatMessageToResponse(m, groupedAtt[uuidToString(m.ID)])
	}
	writeJSON(w, http.StatusOK, ChatMessagesPageResponse{
		Messages:   resp,
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	})
}

type PendingChatTaskResponse struct {
	TaskID    string `json:"task_id,omitempty"`
	Status    string `json:"status,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

func (h *Handler) MarkChatSessionRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	if err := h.Queries.MarkChatSessionRead(r.Context(), session.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark session read")
		return
	}

	resolvedSessionID := uuidToString(session.ID)
	h.publishChat(protocol.EventChatSessionRead, workspaceID, "member", userID, resolvedSessionID, protocol.ChatSessionReadPayload{
		ChatSessionID: resolvedSessionID,
	})

	w.WriteHeader(http.StatusNoContent)
}

type ChatDraftRestoreResponse struct {
	ID            string               `json:"id"`
	ChatSessionID string               `json:"chat_session_id"`
	TaskID        string               `json:"task_id"`
	Content       string               `json:"content"`
	Attachments   []AttachmentResponse `json:"attachments,omitempty"`
	CreatedAt     string               `json:"created_at"`
}

type ChatDraftRestoresResponse struct {
	Restores []ChatDraftRestoreResponse `json:"restores"`
}

func (h *Handler) ListChatDraftRestores(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	rows, err := h.Queries.ListChatDraftRestoresBySession(r.Context(), session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list draft restores")
		return
	}

	var attachmentIDs []pgtype.UUID
	for _, row := range rows {
		attachmentIDs = append(attachmentIDs, row.AttachmentIds...)
	}
	attachmentsByID := map[string]AttachmentResponse{}
	if len(attachmentIDs) > 0 {
		attRows, err := h.Queries.ListAttachmentsByIDs(r.Context(), db.ListAttachmentsByIDsParams{
			AttachmentIds: attachmentIDs,
			WorkspaceID:   session.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load draft restore attachments")
			return
		}
		for _, a := range attRows {
			attachmentsByID[uuidToString(a.ID)] = h.attachmentToResponse(a)
		}
	}

	resp := ChatDraftRestoresResponse{Restores: make([]ChatDraftRestoreResponse, 0, len(rows))}
	for _, row := range rows {
		item := ChatDraftRestoreResponse{
			ID:            uuidToString(row.ID),
			ChatSessionID: uuidToString(row.ChatSessionID),
			TaskID:        uuidToString(row.TaskID),
			Content:       row.Content,
			CreatedAt:     timestampToString(row.CreatedAt),
		}
		for _, attID := range row.AttachmentIds {
			if a, ok := attachmentsByID[uuidToString(attID)]; ok {
				item.Attachments = append(item.Attachments, a)
			}
		}
		resp.Restores = append(resp.Restores, item)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ConsumeChatDraftRestore(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}
	restoreUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "restoreId"), "restore id")
	if !ok {
		return
	}

	if _, err := h.Queries.DeleteChatDraftRestore(r.Context(), db.DeleteChatDraftRestoreParams{
		ID:            restoreUUID,
		ChatSessionID: session.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to consume draft restore")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func pruneRuntimeAgentChatDraftRestores(ctx context.Context, q *db.Queries, runtimeID pgtype.UUID, includeSystemAgents bool) error {
	if _, err := q.LockChatSessionsByArchivedRuntimeAgents(ctx, runtimeID); err != nil {
		return err
	}
	if err := q.DeleteChatDraftRestoresByArchivedRuntimeAgents(ctx, runtimeID); err != nil {
		return err
	}
	if !includeSystemAgents {
		return nil
	}
	if _, err := q.LockChatSessionsBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return err
	}
	return q.DeleteChatDraftRestoresBySystemRuntimeAgents(ctx, runtimeID)
}

type PendingChatTasksResponse struct {
	Tasks []PendingChatTaskItem `json:"tasks"`
}

type PendingChatTaskItem struct {
	TaskID        string `json:"task_id"`
	Status        string `json:"status"`
	ChatSessionID string `json:"chat_session_id"`
}

type CancelledChatMessageResponse struct {
	ChatSessionID  string               `json:"chat_session_id"`
	MessageID      string               `json:"message_id"`
	Content        string               `json:"content"`
	RestoreToInput bool                 `json:"restore_to_input"`
	Attachments    []AttachmentResponse `json:"attachments,omitempty"`
}

type CancelTaskByUserResponse struct {
	AgentTaskResponse
	CancelledChatMessage *CancelledChatMessageResponse `json:"cancelled_chat_message,omitempty"`
}

func (h *Handler) ListPendingChatTasks(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}

	if len(allowed) == 0 {
		writeJSON(w, http.StatusOK, PendingChatTasksResponse{Tasks: []PendingChatTaskItem{}})
		return
	}

	rows, err := h.Queries.ListPendingChatTasksByCreator(r.Context(), db.ListPendingChatTasksByCreatorParams{
		WorkspaceID: parseUUID(workspaceID),
		CreatorID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending chat tasks")
		return
	}

	items := make([]PendingChatTaskItem, 0, len(rows))
	for _, row := range rows {
		agentID := uuidToString(row.AgentID)
		if _, ok := allowed[agentID]; !ok {
			continue
		}
		items = append(items, PendingChatTaskItem{
			TaskID:        uuidToString(row.TaskID),
			Status:        row.Status,
			ChatSessionID: uuidToString(row.ChatSessionID),
		})
	}
	writeJSON(w, http.StatusOK, PendingChatTasksResponse{Tasks: items})
}

type HasPendingChatTasksResponse struct {
	HasPending bool `json:"has_pending"`
}

func (h *Handler) HasPendingChatTasks(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}

	if len(allowed) == 0 {
		writeJSON(w, http.StatusOK, HasPendingChatTasksResponse{HasPending: false})
		return
	}

	agentIDs := make([]pgtype.UUID, 0, len(allowed))
	for id := range allowed {
		agentIDs = append(agentIDs, parseUUID(id))
	}

	hasPending, err := h.Queries.HasPendingChatTasksByCreator(r.Context(), db.HasPendingChatTasksByCreatorParams{
		WorkspaceID: parseUUID(workspaceID),
		CreatorID:   parseUUID(userID),
		AgentIds:    agentIDs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check pending chat tasks")
		return
	}

	writeJSON(w, http.StatusOK, HasPendingChatTasksResponse{HasPending: hasPending})
}

func (h *Handler) GetPendingChatTask(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	task, err := h.Queries.GetPendingChatTask(r.Context(), session.ID)
	if err != nil {

		writeJSON(w, http.StatusOK, PendingChatTaskResponse{})
		return
	}

	writeJSON(w, http.StatusOK, PendingChatTaskResponse{
		TaskID:    uuidToString(task.ID),
		Status:    task.Status,
		CreatedAt: timestampToString(task.CreatedAt),
	})
}

func (h *Handler) CancelTaskByUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	taskID := chi.URLParam(r, "taskId")
	taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task id")
	if !ok {
		return
	}

	task, err := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{
		ID:          taskUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	if task.ChatSessionID.Valid {

		cs, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
			ID:          task.ChatSessionID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		if uuidToString(cs.CreatorID) != userID {
			writeError(w, http.StatusForbidden, "not your task")
			return
		}
	} else {

		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID:          task.AgentID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
			writeError(w, http.StatusForbidden, "you do not have access to this agent")
			return
		}
	}

	cancelled, err := h.TaskService.CancelTaskWithResult(r.Context(), taskUUID, service.CancelTaskOptions{
		ClientSupportsDraftRestore: requestHasClientCapability(r, protocol.AppCapabilityChatDraftRestoreV1),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := CancelTaskByUserResponse{
		AgentTaskResponse: taskToResponse(cancelled.Task, workspaceID),
	}
	h.hydrateTaskAttributions(r.Context(), []*TaskAttribution{resp.AgentTaskResponse.Attribution})
	if cancelled.CancelledChatMessage != nil {
		attachments := make([]AttachmentResponse, 0, len(cancelled.CancelledChatMessage.Attachments))
		for _, a := range cancelled.CancelledChatMessage.Attachments {
			attachments = append(attachments, h.attachmentToResponse(a))
		}
		resp.CancelledChatMessage = &CancelledChatMessageResponse{
			ChatSessionID:  cancelled.CancelledChatMessage.ChatSessionID,
			MessageID:      cancelled.CancelledChatMessage.MessageID,
			Content:        cancelled.CancelledChatMessage.Content,
			RestoreToInput: cancelled.CancelledChatMessage.RestoreToInput,
			Attachments:    attachments,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

type ChatSessionResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	AgentID     string  `json:"agent_id"`
	CreatorID   string  `json:"creator_id"`
	ProjectID   *string `json:"project_id"`
	Title       string  `json:"title"`
	Status      string  `json:"status"`

	HasUnread   bool             `json:"has_unread"`
	UnreadCount int              `json:"unread_count"`
	LastMessage *ChatLastMessage `json:"last_message"`

	Pinned    bool   `json:"pinned"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type ChatLastMessage struct {
	Content       string  `json:"content"`
	Role          string  `json:"role"`
	CreatedAt     string  `json:"created_at"`
	FailureReason *string `json:"failure_reason"`

	MessageKind string `json:"message_kind"`
}

func buildChatLastMessage(at pgtype.Timestamptz, content, role string, failure pgtype.Text, kind string) *ChatLastMessage {
	if !at.Valid {
		return nil
	}
	return &ChatLastMessage{
		Content:       content,
		Role:          role,
		CreatedAt:     timestampToString(at),
		FailureReason: textToPtr(failure),
		MessageKind:   normalizeMessageKind(kind),
	}
}

type ChatMessageResponse struct {
	ID            string  `json:"id"`
	ChatSessionID string  `json:"chat_session_id"`
	Role          string  `json:"role"`
	Content       string  `json:"content"`
	TaskID        *string `json:"task_id"`
	CreatedAt     string  `json:"created_at"`

	FailureReason *string `json:"failure_reason"`

	ElapsedMs *int64 `json:"elapsed_ms"`

	MessageKind string `json:"message_kind"`

	Attachments []AttachmentResponse `json:"attachments,omitempty"`
}

func chatSessionToResponse(s db.ChatSession) ChatSessionResponse {
	return ChatSessionResponse{
		ID:          uuidToString(s.ID),
		WorkspaceID: uuidToString(s.WorkspaceID),
		AgentID:     uuidToString(s.AgentID),
		CreatorID:   uuidToString(s.CreatorID),
		ProjectID:   uuidToPtr(s.ProjectID),
		Title:       s.Title,
		Status:      s.Status,
		Pinned:      s.PinnedAt.Valid,
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
	}
}

func chatMessageToResponse(m db.ChatMessage, attachments []AttachmentResponse) ChatMessageResponse {
	return ChatMessageResponse{
		ID:            uuidToString(m.ID),
		ChatSessionID: uuidToString(m.ChatSessionID),
		Role:          m.Role,
		Content:       m.Content,
		TaskID:        uuidToPtr(m.TaskID),
		CreatedAt:     timestampToString(m.CreatedAt),
		FailureReason: textToPtr(m.FailureReason),
		ElapsedMs:     int8ToPtr(m.ElapsedMs),
		MessageKind:   normalizeMessageKind(m.MessageKind),
		Attachments:   attachments,
	}
}

func normalizeMessageKind(kind string) string {
	switch kind {
	case protocol.ChatMessageKindNoResponse:
		return protocol.ChatMessageKindNoResponse
	default:
		return protocol.ChatMessageKindMessage
	}
}
