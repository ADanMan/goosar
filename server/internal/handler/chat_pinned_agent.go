package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const maxChatPinnedAgents = 5

type ChatPinnedAgentResponse struct {
	AgentID  string  `json:"agent_id"`
	Position float64 `json:"position"`
}

func (h *Handler) resolveChatAgentAccess(w http.ResponseWriter, r *http.Request, userID, workspaceID string) (map[string]struct{}, bool) {
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return nil, false
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return nil, false
	}
	return allowed, true
}

func (h *Handler) ListChatPinnedAgents(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	allowed, ok := h.resolveChatAgentAccess(w, r, userID, workspaceID)
	if !ok {
		return
	}

	rows, err := h.Queries.ListChatPinnedAgents(r.Context(), db.ListChatPinnedAgentsParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pinned agents")
		return
	}

	resp := make([]ChatPinnedAgentResponse, 0, len(rows))
	for _, row := range rows {
		agentID := uuidToString(row.AgentID)
		if _, ok := allowed[agentID]; !ok {
			continue
		}
		resp = append(resp, ChatPinnedAgentResponse{AgentID: agentID, Position: row.Position})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) PinChatAgent(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}

	allowed, ok := h.resolveChatAgentAccess(w, r, userID, workspaceID)
	if !ok {
		return
	}
	if _, ok := allowed[req.AgentID]; !ok {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	existing, err := h.Queries.ListChatPinnedAgents(r.Context(), db.ListChatPinnedAgentsParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pinned agents")
		return
	}
	alreadyPinned := false
	for _, e := range existing {
		if uuidToString(e.AgentID) == req.AgentID {
			alreadyPinned = true
			break
		}
	}
	if !alreadyPinned && len(existing) >= maxChatPinnedAgents {
		writeError(w, http.StatusBadRequest, "pinned agent limit reached")
		return
	}

	maxPos, err := h.Queries.GetMaxChatPinnedAgentPosition(r.Context(), db.GetMaxChatPinnedAgentPositionParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get position")
		return
	}

	row, err := h.Queries.CreateChatPinnedAgent(r.Context(), db.CreateChatPinnedAgentParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		AgentID:     agentUUID,
		Position:    maxPos + 1,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to pin agent")
		return
	}
	writeJSON(w, http.StatusOK, ChatPinnedAgentResponse{
		AgentID:  uuidToString(row.AgentID),
		Position: row.Position,
	})
}

func (h *Handler) UnpinChatAgent(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	agentUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "agentId"), "agentId")
	if !ok {
		return
	}

	if err := h.Queries.DeleteChatPinnedAgent(r.Context(), db.DeleteChatPinnedAgentParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		AgentID:     agentUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unpin agent")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
