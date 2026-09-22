package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/logger"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

const envSentinel = "****"

const (
	agentEnvActivityRevealed = "agent_env_revealed"
	agentEnvActivityListed   = "agent_env_listed"
	agentEnvActivityUpdated  = "agent_env_updated"

	agentEnvActivityUpdateRefused = "agent_env_update_refused"
)

type AgentEnvResponse struct {
	AgentID   string            `json:"agent_id"`
	CustomEnv map[string]string `json:"custom_env"`

	ValuesMasked bool `json:"values_masked"`
}

type UpdateAgentEnvRequest struct {
	CustomEnv map[string]string `json:"custom_env"`
}

func (h *Handler) authorizeAgentEnv(w http.ResponseWriter, r *http.Request) (db.Agent, db.Member, bool, bool) {
	agentID := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return db.Agent{}, db.Member{}, false, false
	}

	workspaceID := uuidToString(agent.WorkspaceID)
	userID := requestUserID(r)

	actorType, _ := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents may not access env management endpoints")
		return db.Agent{}, db.Member{}, false, false
	}

	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "agent not found", "owner", "admin", "member")
	if !ok {
		return db.Agent{}, db.Member{}, false, false
	}
	isAgentOwner := canViewAgentSecrets(agent, userID)
	if !isAgentOwner && !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.Agent{}, db.Member{}, false, false
	}

	return agent, member, isAgentOwner, true
}

func (h *Handler) GetAgentEnv(w http.ResponseWriter, r *http.Request) {
	agent, member, isAgentOwner, ok := h.authorizeAgentEnv(w, r)
	if !ok {
		return
	}

	customEnv := unmarshalCustomEnv(agent)
	keys := sortedKeys(customEnv)

	if !isAgentOwner {
		details, _ := json.Marshal(map[string]any{
			"agent_id":    uuidToString(agent.ID),
			"agent_name":  agent.Name,
			"listed_keys": keys,
			"key_count":   len(keys),
			"masked":      true,
		})
		if _, err := h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
			WorkspaceID: agent.WorkspaceID,
			IssueID:     pgtype.UUID{},
			ActorType:   pgtype.Text{String: "member", Valid: true},
			ActorID:     parseUUID(uuidToString(member.UserID)),
			Action:      agentEnvActivityListed,
			Details:     details,
		}); err != nil {
			slog.Error("agent_env_listed audit write failed; refusing to serve the key listing",
				append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
			writeError(w, http.StatusInternalServerError, "audit log write failed; refusing to list env keys without a recorded read")
			return
		}
		writeJSON(w, http.StatusOK, AgentEnvResponse{
			AgentID:      uuidToString(agent.ID),
			CustomEnv:    maskEnvValues(customEnv),
			ValuesMasked: true,
		})
		return
	}

	details, _ := json.Marshal(map[string]any{
		"agent_id":      uuidToString(agent.ID),
		"agent_name":    agent.Name,
		"revealed_keys": keys,
		"key_count":     len(keys),
		"masked":        false,
	})
	if _, err := h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: agent.WorkspaceID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     parseUUID(uuidToString(member.UserID)),
		Action:      agentEnvActivityRevealed,
		Details:     details,
	}); err != nil {
		slog.Error("agent_env_revealed audit write failed; refusing to serve plaintext",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "audit log write failed; refusing to serve env without a recorded reveal")
		return
	}

	writeJSON(w, http.StatusOK, AgentEnvResponse{
		AgentID:   uuidToString(agent.ID),
		CustomEnv: customEnv,
	})
}

func maskEnvValues(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k := range env {
		out[k] = envSentinel
	}
	return out
}

func (h *Handler) UpdateAgentEnv(w http.ResponseWriter, r *http.Request) {
	agent, member, isAgentOwner, ok := h.authorizeAgentEnv(w, r)
	if !ok {
		return
	}

	var req UpdateAgentEnvRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CustomEnv == nil {
		req.CustomEnv = map[string]string{}
	}

	if !isAgentOwner {
		var injected []string
		for k, v := range req.CustomEnv {

			if !strings.HasPrefix(v, envSentinel) {
				injected = append(injected, k)
			}
		}
		if len(injected) > 0 {
			sort.Strings(injected)
			h.recordRefusedEnvWrite(r, agent, member, injected)
			writeError(w, http.StatusForbidden, fmt.Sprintf(
				"only the agent owner can add or change an env value (%s): values run on the owner's runtime. You can remove keys, or keep them with the %q placeholder",
				strings.Join(injected, ", "), envSentinel))
			return
		}
	}

	existing := unmarshalCustomEnv(agent)
	merged, audit, err := mergeAgentEnv(existing, req.CustomEnv)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	envBytes, err := json.Marshal(merged)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode env")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("agent_env update: begin tx failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update env")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	updated, err := qtx.UpdateAgentCustomEnv(r.Context(), db.UpdateAgentCustomEnvParams{
		ID:        agent.ID,
		CustomEnv: envBytes,
	})
	if err != nil {
		slog.Warn("update agent custom_env failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update env")
		return
	}

	auditDetails := map[string]any{
		"agent_id":       uuidToString(agent.ID),
		"agent_name":     agent.Name,
		"added_keys":     audit.added,
		"removed_keys":   audit.removed,
		"changed_keys":   audit.changed,
		"preserved_keys": audit.preserved,
	}
	details, _ := json.Marshal(auditDetails)
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: agent.WorkspaceID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     parseUUID(uuidToString(member.UserID)),
		Action:      agentEnvActivityUpdated,
		Details:     details,
	}); err != nil {
		slog.Error("agent_env_updated audit write failed; rolling back update",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "audit log write failed; env update rolled back")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("agent_env update: tx commit failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update env")
		return
	}

	resp := h.agentToResponse(updated)
	if err := h.attachAgentSkills(r.Context(), &resp, updated.ID); err != nil {
		slog.Warn("load agent skills after env update failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(updated.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}
	workspaceID := uuidToString(updated.WorkspaceID)
	h.publish(protocol.EventAgentStatus, workspaceID, "member", uuidToString(member.UserID), map[string]any{"agent": broadcastAgentResponse(resp)})

	respEnv := merged
	if !isAgentOwner {
		respEnv = maskEnvValues(merged)
	}
	writeJSON(w, http.StatusOK, AgentEnvResponse{
		AgentID:      uuidToString(updated.ID),
		CustomEnv:    respEnv,
		ValuesMasked: !isAgentOwner,
	})
}

func (h *Handler) recordRefusedEnvWrite(r *http.Request, agent db.Agent, member db.Member, attemptedKeys []string) {
	details, _ := json.Marshal(map[string]any{
		"agent_id":       uuidToString(agent.ID),
		"agent_name":     agent.Name,
		"attempted_keys": attemptedKeys,
		"refused":        true,
	})
	if _, err := h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: agent.WorkspaceID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     parseUUID(uuidToString(member.UserID)),
		Action:      agentEnvActivityUpdateRefused,
		Details:     details,
	}); err != nil {
		slog.Error("agent_env_update_refused audit write failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
	}
}

type envAudit struct {
	added     []string
	removed   []string
	changed   []string
	preserved []string
}

func mergeAgentEnv(existing, request map[string]string) (map[string]string, envAudit, error) {
	merged := make(map[string]string, len(request))
	audit := envAudit{}
	var unresolved, corrupted []string

	for k, v := range request {
		if v == envSentinel {
			if old, ok := existing[k]; ok {
				merged[k] = old
				audit.preserved = append(audit.preserved, k)
			} else {
				unresolved = append(unresolved, k)
			}
			continue
		}
		if strings.HasPrefix(v, envSentinel) {
			corrupted = append(corrupted, k)
			continue
		}
		if old, ok := existing[k]; ok {
			if old == v {
				merged[k] = v
				continue
			}
			merged[k] = v
			audit.changed = append(audit.changed, k)
			continue
		}
		merged[k] = v
		audit.added = append(audit.added, k)
	}

	for k := range existing {
		if _, ok := request[k]; !ok {
			audit.removed = append(audit.removed, k)
		}
	}

	sort.Strings(audit.added)
	sort.Strings(audit.removed)
	sort.Strings(audit.changed)
	sort.Strings(audit.preserved)

	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		return nil, envAudit{}, fmt.Errorf(
			"%q is the marker for a hidden value, not a value: no value is stored under %s. Type the real value, or remove the variable",
			envSentinel, strings.Join(unresolved, ", "))
	}
	if len(corrupted) > 0 {
		sort.Strings(corrupted)
		return nil, envAudit{}, fmt.Errorf(
			"the value for %s starts with the %q marker, which happens when a new secret is typed onto the end of the hidden-value placeholder. Clear the field first, then type the value",
			strings.Join(corrupted, ", "), envSentinel)
	}
	return merged, audit, nil
}

func unmarshalCustomEnv(a db.Agent) map[string]string {
	out := map[string]string{}
	if len(a.CustomEnv) == 0 {
		return out
	}
	if err := json.Unmarshal(a.CustomEnv, &out); err != nil {
		slog.Warn("failed to unmarshal agent custom_env", "agent_id", uuidToString(a.ID), "error", err)
		return map[string]string{}
	}
	if out == nil {
		return map[string]string{}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
