package agentbuilder

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/adanman/goosar/server2/internal/agent"
	"github.com/adanman/goosar/server2/internal/chat"
	"github.com/adanman/goosar/server2/internal/httpapi"
)

// builderInstructions — системная инструкция конструктора, зашитая на
// сервере (contract §2): требует от модели заканчивать каждый ответ
// машиночитаемым блоком <agent_draft>{...}</agent_draft> с текущим
// черновиком конфигурации агента (name/description/instructions/model/...).
const builderInstructions = `Ты помогаешь пользователю собрать конфигурацию нового агента Goosar через диалог.
Уточняющими вопросами выясни имя, описание, системную инструкцию, модель и права вызова агента.
Каждый свой ответ обязательно заканчивай машиночитаемым блоком с текущим черновиком:
<agent_draft>{"name": "...", "description": "...", "instructions": "...", "model": "..."}</agent_draft>
Ты сам никогда не создаёшь агента — это делает клиент отдельным вызовом после того, как пользователь одобрит черновик.`

func newFlowID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

const builderSystemKeyPrefix = "agent_builder:"

func (d *Deps) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	wsID, role, actor, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	var req struct {
		RuntimeID string `json:"runtime_id"`
		Model     string `json:"model"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.RuntimeID == "" {
		httpapi.BadRequest(w, "runtime_id is required")
		return
	}
	found, online, accessible, err := d.Agent.RuntimeAccessible(r.Context(), wsID, req.RuntimeID, actor.UserID, httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !found {
		httpapi.BadRequest(w, "runtime_id does not belong to this workspace")
		return
	}
	if !accessible {
		httpapi.Forbidden(w, "runtime is private and not accessible to the caller")
		return
	}
	if !online {
		httpapi.WriteError(w, http.StatusConflict, "runtime is not online", "runtime_offline")
		return
	}

	ownerID := ""
	if actor.IsHuman {
		ownerID = actor.UserID
	}
	builderAgent, err := d.Agent.Create(r.Context(), agent.CreateParams{
		WorkspaceID: wsID, ExecutorID: req.RuntimeID, Title: "Agent Builder", Instructions: builderInstructions,
		RuntimeMode: "local", PermissionMode: "private", OwnerAccountID: ownerID, Model: req.Model,
		Kind: "system", SystemKey: builderSystemKeyPrefix + newFlowID(),
	})
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	session, err := d.Chat.CreateSession(r.Context(), chat.CreateSessionParams{
		WorkspaceID: wsID, AgentID: builderAgent.ID, CreatorID: actor.UserID, Title: "Agent Builder",
	})
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, map[string]any{
		"session_id": session.ID, "builder_agent_id": builderAgent.ID, "runtime_id": req.RuntimeID,
	})
}

func (d *Deps) handleSwitchRuntime(w http.ResponseWriter, r *http.Request) {
	wsID, role, actor, ok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !ok {
		return
	}
	sessionID := r.PathValue("sessionId")
	session, err := d.Chat.GetSession(r.Context(), wsID, sessionID)
	if errors.Is(err, chat.ErrNotFound) || session.CreatorID != actor.UserID {
		httpapi.NotFound(w, "agent builder session not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if session.Status != "active" {
		httpapi.BadRequest(w, "session is archived")
		return
	}
	carrierAgent, err := d.Agent.Get(r.Context(), wsID, session.AgentID)
	if errors.Is(err, agent.ErrNotFound) || carrierAgent.Kind != "system" || carrierAgent.SystemKey == nil ||
		!strings.HasPrefix(*carrierAgent.SystemKey, builderSystemKeyPrefix) {
		httpapi.NotFound(w, "this chat session is not an agent builder session")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	pending, err := d.hasPendingConvoTask(r.Context(), session.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if pending {
		httpapi.WriteError(w, http.StatusConflict, "session has a pending response; stop it first", "session_has_pending_task")
		return
	}

	var req struct {
		RuntimeID string `json:"runtime_id"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.RuntimeID == "" {
		httpapi.BadRequest(w, "runtime_id is required")
		return
	}
	found, online, accessible, err := d.Agent.RuntimeAccessible(r.Context(), wsID, req.RuntimeID, actor.UserID, httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !found {
		httpapi.BadRequest(w, "runtime_id does not belong to this workspace")
		return
	}
	if !accessible {
		httpapi.Forbidden(w, "runtime is private and not accessible to the caller")
		return
	}
	if !online {
		httpapi.WriteError(w, http.StatusConflict, "runtime is not online", "runtime_offline")
		return
	}

	carrierAgent.ExecutorID = req.RuntimeID
	if err := d.Agent.SaveCore(r.Context(), carrierAgent); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"runtime_id": req.RuntimeID})
}

// hasPendingConvoTask — есть ли у сессии незавершённый запуск (dispatch_jobs
// по convo_id в активном статусе); dispatch не даёт отдельного read-only
// метода для этого случая (только Cancel*), поэтому — прямой SQL по общей
// таблице, тот же приём, что internal/squad/status.go применяет к
// dispatch_jobs.
func (d *Deps) hasPendingConvoTask(ctx context.Context, convoID string) (bool, error) {
	return d.DB.RowExists(ctx, `SELECT EXISTS(
		SELECT 1 FROM dispatch_jobs WHERE convo_id = $1 AND dj_status = ANY($2))`,
		convoID, []string{"queued", "dispatched", "waiting_local_directory", "running", "deferred"})
}
