package chat

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
)

// requireSession — резолвит воркспейс + актора + сессию из {sessionId}, и
// проверяет "доступна только создателю сессии" (x-roles: "member (только
// создатель сессии)" на каждом маршруте /api/chat/sessions/{sessionId}/**).
func (d *Deps) requireSession(w http.ResponseWriter, r *http.Request) (workspaceID string, actor *httpapi.Actor, sess Session, ok bool) {
	wsID, role, a, rok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !rok {
		return "", nil, Session{}, false
	}
	if role == httpapi.RoleAgent {
		httpapi.Forbidden(w, "this endpoint is only available to human members")
		return "", nil, Session{}, false
	}
	sess, err := d.Store.GetSession(r.Context(), wsID, r.PathValue("sessionId"))
	if err == ErrNotFound {
		httpapi.WriteError(w, http.StatusNotFound, "chat session not found", "not_found")
		return "", nil, Session{}, false
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return "", nil, Session{}, false
	}
	if sess.CreatorID != a.UserID {
		httpapi.WriteError(w, http.StatusForbidden, "chat session does not belong to you", "forbidden")
		return "", nil, Session{}, false
	}
	return wsID, a, sess, true
}

type createSessionRequest struct {
	AgentID   string  `json:"agent_id"`
	Title     string  `json:"title"`
	ProjectID *string `json:"project_id"`
}

func (d *Deps) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	workspaceID, role, actor, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	if role == httpapi.RoleAgent {
		httpapi.Forbidden(w, "this endpoint is only available to human members")
		return
	}
	var req createSessionRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.AgentID == "" {
		httpapi.WriteError(w, http.StatusBadRequest, "agent_id is required", "invalid_request")
		return
	}
	agent, err := d.Store.GetAgent(r.Context(), workspaceID, req.AgentID)
	if err == ErrNotFound {
		httpapi.NotFound(w, "agent not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if agent.ArchivedAt != nil {
		httpapi.WriteError(w, http.StatusNotFound, "agent is archived", "agent_archived")
		return
	}
	can, err := d.Store.CanInvoke(r.Context(), agent, actor.UserID, role)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !can {
		httpapi.Forbidden(w, "no access to invoke this agent")
		return
	}
	sess, err := d.Store.CreateSession(r.Context(), CreateSessionParams{
		WorkspaceID: workspaceID, AgentID: req.AgentID, CreatorID: actor.UserID, ProjectID: req.ProjectID, Title: req.Title,
	})
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, sess)
}

func (d *Deps) handleListSessions(w http.ResponseWriter, r *http.Request) {
	workspaceID, role, actor, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	if role == httpapi.RoleAgent {
		httpapi.Forbidden(w, "this endpoint is only available to human members")
		return
	}
	includeArchived := r.URL.Query().Get("status") == "all"
	list, err := d.Store.ListSessions(r.Context(), workspaceID, actor.UserID, includeArchived)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []SessionListItem{}
	}
	httpapi.WriteJSON(w, http.StatusOK, list)
}

func (d *Deps) handleGetSession(w http.ResponseWriter, r *http.Request) {
	_, _, sess, ok := d.requireSession(w, r)
	if !ok {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, sess)
}

type updateSessionRequest struct {
	Title     *string `json:"title"`
	ProjectID *string `json:"project_id"`
}

func (d *Deps) handleUpdateSession(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, sess, ok := d.requireSession(w, r)
	if !ok {
		return
	}
	var raw map[string]json.RawMessage
	if err := httpapi.DecodeJSON(r, &raw); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	_, hasTitle := raw["title"]
	_, hasProject := raw["project_id"]
	if hasTitle == hasProject {
		httpapi.WriteError(w, http.StatusBadRequest, "exactly one of title/project_id is required", "invalid_request")
		return
	}
	var req updateSessionRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	patch := UpdateSessionPatch{}
	if hasTitle {
		if req.Title == nil || strings.TrimSpace(*req.Title) == "" || len(*req.Title) > 200 {
			httpapi.WriteError(w, http.StatusBadRequest, "title must be 1..200 characters", "invalid_request")
			return
		}
		patch.Title = req.Title
	} else {
		patch.HasProj, patch.ProjectID = true, req.ProjectID
	}
	updated, err := d.Store.UpdateSession(r.Context(), workspaceID, sess.ID, patch)
	if err == ErrNotFound {
		httpapi.NotFound(w, "chat session not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		payload := map[string]any{"chat_session_id": updated.ID, "title": updated.Title, "updated_at": updated.UpdatedAt}
		if hasProject {
			payload["project_id"] = updated.ProjectID
		}
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "chat:session_updated", Payload: payload})
	}
	httpapi.WriteJSON(w, http.StatusOK, updated)
}

func (d *Deps) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, sess, ok := d.requireSession(w, r)
	if !ok {
		return
	}
	if d.Dispatch != nil {
		jobs, err := d.Dispatch.CancelActiveForConvo(r.Context(), d.DB.Pool, workspaceID, sess.ID)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		_ = jobs
	}
	if err := d.Store.DeleteSession(r.Context(), workspaceID, sess.ID); err != nil && err != ErrNotFound {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "chat:session_deleted", Payload: map[string]any{"chat_session_id": sess.ID}})
	}
	w.WriteHeader(http.StatusNoContent)
}

type pinRequest struct {
	Pinned bool `json:"pinned"`
}

func (d *Deps) handleSetPinned(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, sess, ok := d.requireSession(w, r)
	if !ok {
		return
	}
	var req pinRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	updated, err := d.Store.SetPinned(r.Context(), workspaceID, sess.ID, req.Pinned)
	if err == ErrNotFound {
		httpapi.NotFound(w, "chat session not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "chat:session_updated",
			Payload: map[string]any{"chat_session_id": updated.ID, "title": updated.Title, "pinned": updated.Pinned, "updated_at": updated.UpdatedAt}})
	}
	httpapi.WriteJSON(w, http.StatusOK, updated)
}

type archiveRequest struct {
	Archived bool `json:"archived"`
}

func (d *Deps) handleSetArchived(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, sess, ok := d.requireSession(w, r)
	if !ok {
		return
	}
	var req archiveRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	updated, err := d.Store.SetArchived(r.Context(), workspaceID, sess.ID, req.Archived)
	if err == ErrNotFound {
		httpapi.NotFound(w, "chat session not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "chat:session_updated",
			Payload: map[string]any{"chat_session_id": updated.ID, "title": updated.Title, "status": updated.Status, "updated_at": updated.UpdatedAt}})
	}
	httpapi.WriteJSON(w, http.StatusOK, updated)
}

func (d *Deps) handleMarkSessionRead(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, sess, ok := d.requireSession(w, r)
	if !ok {
		return
	}
	if err := d.Store.MarkSessionRead(r.Context(), workspaceID, sess.ID); err != nil && err != ErrNotFound {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "chat:session_read", Payload: map[string]any{"chat_session_id": sess.ID}})
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- messages --------------------------------------------------------------------

// chatPriority — приоритет dispatch_jobs для задач, поставленных из чата.
// Контракт требует "приоритет чата" выше фонового (0 по умолчанию у
// dispatch_jobs), не называя число — пробел спецификации, зафиксирован в
// server2/docs/decisions.md.
const chatPriority = 10

// generateTitle — простая эвристика заголовка сессии по первому сообщению
// (обрезка). Контракт описывает "асинхронную генерацию заголовка" (обычно —
// вызов LLM), которая вне досягаемости clean-room без внешнего API; решение
// зафиксировано в server2/docs/decisions.md.
func generateTitle(content string) string {
	const maxLen = 60
	t := strings.TrimSpace(content)
	t = strings.Join(strings.Fields(t), " ")
	r := []rune(t)
	if len(r) <= maxLen {
		return t
	}
	return string(r[:maxLen]) + "…"
}

type sendMessageRequest struct {
	Content       string   `json:"content"`
	AttachmentIDs []string `json:"attachment_ids"`
}

func (d *Deps) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	workspaceID, actor, sess, ok := d.requireSession(w, r)
	if !ok {
		return
	}
	if sess.Status != "active" {
		httpapi.WriteError(w, http.StatusBadRequest, "chat session is archived", "session_archived")
		return
	}
	var req sendMessageRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		httpapi.WriteError(w, http.StatusBadRequest, "content must not be empty", "invalid_request")
		return
	}

	agent, err := d.Store.GetAgent(r.Context(), workspaceID, sess.AgentID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if agent.ArchivedAt != nil || agent.ExecutorID == "" {
		httpapi.WriteError(w, http.StatusConflict, "agent is archived or has no assigned runtime", "agent_unavailable")
		return
	}

	isFirst, err := d.Store.CountMessages(r.Context(), sess.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	var jobID string
	if d.Dispatch != nil {
		jobID, err = d.Dispatch.Enqueue(r.Context(), d.DB.Pool, dispatch.JobSpec{
			WorkspaceID:     workspaceID,
			OperativeID:     sess.AgentID,
			ExecutorID:      agent.ExecutorID,
			InitiativeID:    derefOrEmpty(sess.ProjectID),
			ConvoID:         sess.ID,
			Kind:            dispatch.KindChat,
			Priority:        chatPriority,
			ChatMessage:     content,
			ChatIntro:       isFirst == 0,
			InitiatorType:   "member",
			InitiatorID:     actor.UserID,
			ContextSnapshot: d.Store.BuildContextSnapshot(r.Context(), sess, actor.Name),
		})
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
	}

	msg, err := d.Store.CreateMessage(r.Context(), CreateMessageParams{ConvoID: sess.ID, Role: "user", Content: content, TaskID: strPtrOrNil(jobID)})
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	claimed, err := d.Store.ClaimAttachments(r.Context(), workspaceID, sess.ID, msg.ID, req.AttachmentIDs)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if isFirst == 0 && sess.Title == "" {
		_, _ = d.Store.UpdateSession(r.Context(), workspaceID, sess.ID, UpdateSessionPatch{Title: strPtr(generateTitle(content))})
	}
	if d.Publisher != nil {
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "chat:message", Payload: map[string]any{
			"chat_session_id": sess.ID, "message_id": msg.ID, "role": "user", "content": content,
			"task_id": jobID, "created_at": msg.CreatedAt,
		}})
	}
	httpapi.WriteJSON(w, http.StatusCreated, map[string]any{
		"message_id": msg.ID, "task_id": strPtrOrNil(jobID), "attachment_ids": claimed, "created_at": msg.CreatedAt,
	})
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func strPtr(s string) *string { return &s }

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (d *Deps) handleListMessages(w http.ResponseWriter, r *http.Request) {
	_, _, sess, ok := d.requireSession(w, r)
	if !ok {
		return
	}
	list, err := d.Store.ListMessages(r.Context(), sess.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []Message{}
	}
	httpapi.WriteJSON(w, http.StatusOK, list)
}

func (d *Deps) handleListMessagesPage(w http.ResponseWriter, r *http.Request) {
	_, _, sess, ok := d.requireSession(w, r)
	if !ok {
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			httpapi.WriteError(w, http.StatusBadRequest, "limit must be 1..100", "invalid_request")
			return
		}
		limit = n
	}
	beforeCreated := r.URL.Query().Get("before_created_at")
	beforeID := r.URL.Query().Get("before_id")
	if (beforeCreated == "") != (beforeID == "") {
		httpapi.WriteError(w, http.StatusBadRequest, "before_created_at and before_id must be passed together", "invalid_request")
		return
	}
	var cursor *PageCursor
	if beforeCreated != "" {
		t, err := time.Parse(time.RFC3339, beforeCreated)
		if err != nil {
			httpapi.WriteError(w, http.StatusBadRequest, "invalid before_created_at", "invalid_request")
			return
		}
		cursor = &PageCursor{CreatedAt: t, ID: beforeID}
	}
	page, err := d.Store.ListMessagesPage(r.Context(), sess.ID, limit, cursor)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if page.Messages == nil {
		page.Messages = []Message{}
	}
	httpapi.WriteJSON(w, http.StatusOK, page)
}

func (d *Deps) handleGetPendingTask(w http.ResponseWriter, r *http.Request) {
	_, _, sess, ok := d.requireSession(w, r)
	if !ok {
		return
	}
	t, err := d.Store.PendingForSession(r.Context(), sess.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if t == nil {
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{})
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, t)
}

func (d *Deps) handleListDraftRestores(w http.ResponseWriter, r *http.Request) {
	_, _, sess, ok := d.requireSession(w, r)
	if !ok {
		return
	}
	list, err := d.Store.ListDraftRestores(r.Context(), sess.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"restores": list})
}

func (d *Deps) handleConsumeDraftRestore(w http.ResponseWriter, r *http.Request) {
	_, _, sess, ok := d.requireSession(w, r)
	if !ok {
		return
	}
	if err := d.Store.ConsumeDraftRestore(r.Context(), sess.ID, r.PathValue("restoreId")); err != nil && err != ErrNotFound {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- workspace-wide pending tasks / pinned agents ---------------------------------

func (d *Deps) handleListPendingTasks(w http.ResponseWriter, r *http.Request) {
	workspaceID, role, actor, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	if role == httpapi.RoleAgent {
		httpapi.Forbidden(w, "this endpoint is only available to human members")
		return
	}
	list, err := d.Store.PendingForUser(r.Context(), workspaceID, actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"tasks": list})
}

func (d *Deps) handleHasPendingTasks(w http.ResponseWriter, r *http.Request) {
	workspaceID, role, actor, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	if role == httpapi.RoleAgent {
		httpapi.Forbidden(w, "this endpoint is only available to human members")
		return
	}
	has, err := d.Store.HasPendingForUser(r.Context(), workspaceID, actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"has_pending": has})
}

func (d *Deps) handleListPinnedAgents(w http.ResponseWriter, r *http.Request) {
	_, role, actor, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	if role == httpapi.RoleAgent {
		httpapi.Forbidden(w, "this endpoint is only available to human members")
		return
	}
	list, err := d.Store.ListPinnedAgents(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []PinnedAgent{}
	}
	httpapi.WriteJSON(w, http.StatusOK, list)
}

type pinAgentRequest struct {
	AgentID string `json:"agent_id"`
}

func (d *Deps) handlePinAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID, role, actor, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	if role == httpapi.RoleAgent {
		httpapi.Forbidden(w, "this endpoint is only available to human members")
		return
	}
	var req pinAgentRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.AgentID == "" {
		httpapi.WriteError(w, http.StatusBadRequest, "agent_id is required", "invalid_request")
		return
	}
	agent, err := d.Store.GetAgent(r.Context(), workspaceID, req.AgentID)
	if err == ErrNotFound {
		httpapi.NotFound(w, "agent not available to you")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	can, err := d.Store.CanInvoke(r.Context(), agent, actor.UserID, role)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !can {
		httpapi.NotFound(w, "agent not available to you")
		return
	}
	n, err := d.Store.CountPinnedAgents(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if n >= PinnedLimit {
		httpapi.WriteError(w, http.StatusBadRequest, "at most 5 pinned agents are allowed", "pin_limit_reached")
		return
	}
	p, err := d.Store.PinAgent(r.Context(), actor.UserID, req.AgentID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, p)
}

func (d *Deps) handleUnpinAgent(w http.ResponseWriter, r *http.Request) {
	_, role, actor, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	if role == httpapi.RoleAgent {
		httpapi.Forbidden(w, "this endpoint is only available to human members")
		return
	}
	if err := d.Store.UnpinAgent(r.Context(), actor.UserID, r.PathValue("agentId")); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- channel history (вызывается агентом из контекста задачи) --------------------

// requireTaskToken — общий пролог getChatChannelHistory/getChatThread:
// X-Actor-Source: task_token + X-Task-ID, задача обязана быть чатовой.
func (d *Deps) requireTaskToken(w http.ResponseWriter, r *http.Request) (sess Session, ok bool) {
	actor, aok := httpapi.RequireActor(w, r)
	if !aok {
		return Session{}, false
	}
	if r.Header.Get("X-Actor-Source") != "task_token" {
		httpapi.Forbidden(w, "request must be marked X-Actor-Source: task_token")
		return Session{}, false
	}
	taskID := r.Header.Get("X-Task-ID")
	if taskID == "" {
		httpapi.WriteError(w, http.StatusBadRequest, "X-Task-ID is required", "invalid_request")
		return Session{}, false
	}
	if d.Dispatch == nil {
		httpapi.WriteError(w, http.StatusNotFound, "task not found", "not_found")
		return Session{}, false
	}
	job, err := d.Dispatch.Store.GetJob(r.Context(), d.DB.Pool, taskID)
	if err == dispatch.ErrNotFound {
		httpapi.WriteError(w, http.StatusNotFound, "task not found", "not_found")
		return Session{}, false
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return Session{}, false
	}
	if job.Kind != dispatch.KindChat || job.ConvoID == nil {
		httpapi.WriteError(w, http.StatusBadRequest, "task is not a chat task", "invalid_request")
		return Session{}, false
	}
	_ = actor
	sess, err = d.Store.GetSession(r.Context(), job.WorkspaceID, *job.ConvoID)
	if err == ErrNotFound {
		httpapi.WriteError(w, http.StatusNotFound, "chat session not found", "not_found")
		return Session{}, false
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return Session{}, false
	}
	return sess, true
}

// channelHistory — и getChatChannelHistory, и getChatThread по контракту
// одинаковы для внутреннего (не внешнего) чата: у сессии нет привязанного
// внешнего канала (convo_channel_links пуст в этой версии — Slack/внешние
// интеграции вне рамок T-027, см. server2/docs/decisions.md), поэтому оба
// маршрута отвечают note вместо ошибки, как и оговаривает контракт.
func (d *Deps) channelHistory(w http.ResponseWriter, r *http.Request) {
	_, ok := d.requireTaskToken(w, r)
	if !ok {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"channel_type": "",
		"messages":     []any{},
		"note":         "chat session has no linked external channel",
	})
}
