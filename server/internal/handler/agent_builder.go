package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const agentBuilderInstructions = `You are Goosar Agent Builder. Help the user design one practical AI agent through a short conversation.

Your job is to propose and refine configuration, never to create resources yourself. Ask only questions that materially change behavior. Prefer making a reasonable draft immediately, then ask at most two focused questions per turn.

Every response MUST end with exactly one <agent_draft> JSON block using this shape:
<agent_draft>{"name":"","description":"","instructions":"","model":"","skill_ids":[],"permission_scope":"private","member_ids":[]}</agent_draft>

Rules:
- The JSON must be valid, compact JSON on one physical line. Do not wrap it in Markdown fences.
- Escape every line break inside instructions as \n. Never place a literal newline inside a JSON string.
- Preserve good existing draft fields supplied in the user's message unless the user asks to change them.
- name is concise and suitable for a workspace list.
- description is one sentence, at most 200 characters.
- instructions are a complete Markdown system prompt describing role, workflow, output, and constraints.
- model must be empty, preserve current_draft.model, or exactly match an id explicitly listed in AVAILABLE RUNTIME MODELS. Never use a model label as the id.
- When AVAILABLE RUNTIME MODELS is null or empty, preserve current_draft.model and never invent a model id.
- skill_ids may only contain IDs explicitly listed in AVAILABLE WORKSPACE SKILLS.
- permission_scope must be private, workspace, or members. Default to private unless the user explicitly requests sharing.
- member_ids may only contain IDs explicitly listed in AVAILABLE WORKSPACE MEMBERS, and only when permission_scope is members.
- Never request, expose, or place secrets, tokens, passwords, or environment-variable values in the draft.
- Do not claim that the agent has been created. The user must review and confirm the draft in the UI.`

type CreateAgentBuilderSessionRequest struct {
	RuntimeID string `json:"runtime_id"`
	Model     string `json:"model,omitempty"`
}

type CreateAgentBuilderSessionResponse struct {
	SessionID      string `json:"session_id"`
	BuilderAgentID string `json:"builder_agent_id"`
	RuntimeID      string `json:"runtime_id"`
}

func (h *Handler) CreateAgentBuilderSession(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateAgentBuilderSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	runtimeID := strings.TrimSpace(req.RuntimeID)
	if runtimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}

	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	runtime, ok := h.resolveBuilderRuntime(w, r, workspaceID, workspaceUUID, runtimeID, "start")
	if !ok {
		return
	}

	flowID := uuid.NewString()
	ownerUUID := parseUUID(userID)
	model := strings.TrimSpace(req.Model)
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start agent builder session")
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

	builder, err := qtx.CreateAgentBuilder(r.Context(), db.CreateAgentBuilderParams{
		WorkspaceID:  workspaceUUID,
		Name:         fmt.Sprintf(".goosar-agent-builder-%s", flowID),
		RuntimeMode:  runtime.RuntimeMode,
		RuntimeID:    runtime.ID,
		OwnerID:      ownerUUID,
		Instructions: agentBuilderInstructions,
		Model:        pgtype.Text{String: model, Valid: model != ""},
		SystemKey: pgtype.Text{
			String: fmt.Sprintf("agent_builder:%s", flowID),
			Valid:  true,
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare agent builder")
		return
	}

	session, err := qtx.CreateChatSession(r.Context(), db.CreateChatSessionParams{
		WorkspaceID: workspaceUUID,
		AgentID:     builder.ID,
		CreatorID:   ownerUUID,
		Title:       "Create an agent",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create agent builder session")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit agent builder session")
		return
	}

	writeJSON(w, http.StatusCreated, CreateAgentBuilderSessionResponse{
		SessionID:      uuidToString(session.ID),
		BuilderAgentID: uuidToString(builder.ID),
		RuntimeID:      runtimeID,
	})
}

func (h *Handler) resolveBuilderRuntime(w http.ResponseWriter, r *http.Request, workspaceID string, workspaceUUID pgtype.UUID, runtimeID, verb string) (db.AgentRuntime, bool) {
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return db.AgentRuntime{}, false
	}
	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeUUID,
		WorkspaceID: workspaceUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return db.AgentRuntime{}, false
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.AgentRuntime{}, false
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can use it")
		return db.AgentRuntime{}, false
	}
	if runtime.Status != "online" {
		writeError(w, http.StatusConflict, fmt.Sprintf("runtime must be online to %s an agent builder session", verb))
		return db.AgentRuntime{}, false
	}
	return runtime, true
}

type SwitchAgentBuilderRuntimeRequest struct {
	RuntimeID string `json:"runtime_id"`
}

type SwitchAgentBuilderRuntimeResponse struct {
	RuntimeID string `json:"runtime_id"`
}

func (h *Handler) SwitchAgentBuilderRuntime(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req SwitchAgentBuilderRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	runtimeID := strings.TrimSpace(req.RuntimeID)
	if runtimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}

	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, chi.URLParam(r, "sessionId"))
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
	if !isAgentBuilderCarrier(agent) {
		writeError(w, http.StatusNotFound, "agent builder session not found")
		return
	}

	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	runtime, ok := h.resolveBuilderRuntime(w, r, workspaceID, workspaceUUID, runtimeID, "switch")
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to switch agent builder runtime")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockChatSessionForRuntimeBind(r.Context(), session.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "chat session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lock chat session")
		return
	}

	if _, err := qtx.GetPendingChatTask(r.Context(), session.ID); err == nil {
		writeError(w, http.StatusConflict, "stop the current reply before switching runtime")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to check pending builder task")
		return
	}

	updated, err := qtx.RebindAgentBuilderRuntime(r.Context(), db.RebindAgentBuilderRuntimeParams{
		ID:          agent.ID,
		RuntimeID:   runtime.ID,
		RuntimeMode: runtime.RuntimeMode,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "agent builder session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to switch agent builder runtime")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit agent builder runtime switch")
		return
	}

	writeJSON(w, http.StatusOK, SwitchAgentBuilderRuntimeResponse{
		RuntimeID: uuidToString(updated.RuntimeID),
	})
}

func isAgentBuilderCarrier(agent db.Agent) bool {
	return agent.Kind == "system" &&
		agent.SystemKey.Valid &&
		strings.HasPrefix(agent.SystemKey.String, "agent_builder:")
}
