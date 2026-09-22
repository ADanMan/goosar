// Ручки чтения/записи двух редактируемых слоёв конфигурации, которые
// использует резолвер effective_config: конфиг рабочего пространства,
// персональные override'ы пользователя и слой политики деплоя (только
// чтение). Доступ — только владелец/админ пространства, чтения тоже
// закрыты для агентов, работающих от имени владельца. Ключ LLM и
// MCP-значения — write-only, ответы возвращают только признак наличия;
// секретные поля хранятся запечатанными, а запись без настроенного ключа
// шифрования отказывает с 503.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	maxConfigBaseURLLength = 2048
	maxConfigModelLength   = 256
	maxConfigAPIKeyLength  = 4096
	maxConfigMCPDocBytes   = 64 * 1024
	maxConfigMCPNameLength = 128
)

type WorkspaceConfigResponse struct {
	LlmBaseURL   string          `json:"llm_base_url,omitempty"`
	LlmModel     string          `json:"llm_model,omitempty"`
	HasLlmAPIKey bool            `json:"has_llm_api_key"`
	McpDefaults  json.RawMessage `json:"mcp_defaults,omitempty"`
	UpdatedAt    *time.Time      `json:"updated_at,omitempty"`
	UpdatedBy    string          `json:"updated_by,omitempty"`
}

type UserConfigOverrideResponse struct {
	UserID       string          `json:"user_id"`
	LlmBaseURL   string          `json:"llm_base_url,omitempty"`
	LlmModel     string          `json:"llm_model,omitempty"`
	HasLlmAPIKey bool            `json:"has_llm_api_key"`
	McpOverrides json.RawMessage `json:"mcp_overrides,omitempty"`
	UpdatedAt    *time.Time      `json:"updated_at,omitempty"`
	UpdatedBy    string          `json:"updated_by,omitempty"`
}

type DeploymentPolicyResponse struct {
	Policy    json.RawMessage `json:"policy"`
	UpdatedAt *time.Time      `json:"updated_at,omitempty"`
}

type putConfigLayerRequest struct {
	LlmBaseURL *string         `json:"llm_base_url"`
	LlmModel   *string         `json:"llm_model"`
	LlmAPIKey  *string         `json:"llm_api_key"`
	McpDoc     json.RawMessage `json:"-"`
}

type putWorkspaceConfigBody struct {
	putConfigLayerRequest
	McpDefaults json.RawMessage `json:"mcp_defaults"`
}

type putUserConfigOverrideBody struct {
	putConfigLayerRequest
	McpOverrides json.RawMessage `json:"mcp_overrides"`
}

type configLayerState struct {
	baseURL      string
	model        string
	apiKeySealed []byte
	mcpSealed    []byte
}

func (h *Handler) requireWorkspaceConfigAdmin(w http.ResponseWriter, r *http.Request) (member db.Member, workspaceID string, viaDeploymentRole bool, ok bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.Member{}, "", false, false
	}
	workspaceID = h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Member{}, "", false, false
	}
	member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err == nil && isWorkspaceOwnerOrAdmin(member.Role) {
		return member, workspaceID, false, true
	}

	if userUUID, parseErr := util.ParseUUID(userID); parseErr == nil {
		isAdmin, adminErr := h.isDeploymentAdmin(r.Context(), userUUID)
		if adminErr != nil {
			slog.Error("workspace config gate: deployment admin lookup failed", "error", adminErr)
		} else if isAdmin {
			return db.Member{UserID: userUUID}, workspaceID, true, true
		}
	}
	if err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return db.Member{}, "", false, false
	}
	writeError(w, http.StatusForbidden, "only workspace owners and admins can manage workspace configuration")
	return db.Member{}, "", false, false
}

type configLayerAuditDoc struct {
	LlmBaseURL   string          `json:"llm_base_url,omitempty"`
	LlmModel     string          `json:"llm_model,omitempty"`
	HasLlmAPIKey bool            `json:"has_llm_api_key"`
	MCP          json.RawMessage `json:"mcp,omitempty"`
}

func (h *Handler) configLayerAuditHash(state configLayerState) (string, error) {
	if state.baseURL == "" && state.model == "" && len(state.apiKeySealed) == 0 && len(state.mcpSealed) == 0 {
		return "", nil
	}
	masked, err := h.maskConfigDocument(state.mcpSealed)
	if err != nil {
		return "", err
	}
	doc := configLayerAuditDoc{
		LlmBaseURL:   state.baseURL,
		LlmModel:     state.model,
		HasLlmAPIKey: len(state.apiKeySealed) > 0,
		MCP:          masked,
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("config audit hash: %w", err)
	}
	return deploymentPolicyHash(raw), nil
}

func auditDeploymentConfigOp(ctx context.Context, q *db.Queries, r *http.Request, actor pgtype.UUID, action, targetType, targetID, beforeHash, afterHash string) error {
	_, err := q.InsertAdminAudit(ctx, db.InsertAdminAuditParams{
		ActorUserID: actor,
		Action:      action,
		TargetType:  targetType,
		TargetID:    pgtype.Text{String: targetID, Valid: true},
		BeforeHash:  pgtype.Text{String: beforeHash, Valid: beforeHash != ""},
		AfterHash:   pgtype.Text{String: afterHash, Valid: afterHash != ""},
		RequestID:   adminAuditRequestID(r),
	})
	return err
}

func overrideAuditTargetID(workspaceID string, userUUID pgtype.UUID) string {
	return workspaceID + "/" + uuidToString(userUUID)
}

func validateConfigBaseURL(raw string) error {
	if len(raw) > maxConfigBaseURLLength {
		return fmt.Errorf("llm_base_url exceeds %d characters", maxConfigBaseURLLength)
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("llm_base_url must be an absolute http(s) URL")
	}
	return nil
}

func (h *Handler) mergeConfigMCPDoc(currentSealed []byte, patch json.RawMessage) ([]byte, int, string) {
	if len(patch) > maxConfigMCPDocBytes {
		return nil, http.StatusBadRequest, fmt.Sprintf("mcp document exceeds %d bytes", maxConfigMCPDocBytes)
	}
	var patchEntries map[string]json.RawMessage
	if err := json.Unmarshal(patch, &patchEntries); err != nil {
		return nil, http.StatusBadRequest, "mcp document must be a JSON object of server name to entry"
	}

	currentDoc, err := h.openConfigDocument(currentSealed)
	if err != nil {
		if errors.Is(err, errConfigSealedKeyUnset) {
			return nil, http.StatusServiceUnavailable, "cannot edit mcp configuration: stored document is sealed and GOOSAR_MCP_SECRET_KEY is not configured on this server"
		}
		return nil, http.StatusInternalServerError, "failed to read stored mcp configuration"
	}
	current, err := parseMCPLayerDoc(currentDoc)
	if err != nil {
		return nil, http.StatusInternalServerError, "failed to parse stored mcp configuration"
	}
	if current == nil {
		current = map[string]configMCPLayerEntry{}
	}

	for name, entryRaw := range patchEntries {
		if name == "" || len(name) > maxConfigMCPNameLength {
			return nil, http.StatusBadRequest, fmt.Sprintf("mcp server name must be 1-%d characters", maxConfigMCPNameLength)
		}
		if isJSONNull(entryRaw) {
			delete(current, name)
			continue
		}
		var entry struct {
			Enabled *bool              `json:"enabled"`
			Env     map[string]*string `json:"env"`
		}
		dec := json.NewDecoder(bytes.NewReader(entryRaw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&entry); err != nil {
			return nil, http.StatusBadRequest, fmt.Sprintf("mcp entry %q must be an object with only enabled/env fields (env values: string sets, null deletes)", name)
		}
		merged := current[name]
		if entry.Enabled != nil {
			merged.Enabled = entry.Enabled
		}
		for envKey, envVal := range entry.Env {
			if envKey == "" {
				return nil, http.StatusBadRequest, fmt.Sprintf("mcp entry %q has an empty env variable name", name)
			}
			if envVal == nil {
				delete(merged.Env, envKey)
				continue
			}
			if merged.Env == nil {
				merged.Env = map[string]string{}
			}
			merged.Env[envKey] = *envVal
		}
		if len(merged.Env) == 0 {
			merged.Env = nil
		}
		current[name] = merged
	}

	if len(current) == 0 {
		return nil, 0, ""
	}
	mergedDoc, err := json.Marshal(current)
	if err != nil {
		return nil, http.StatusInternalServerError, "failed to store mcp configuration"
	}
	if len(mergedDoc) > maxConfigMCPDocBytes {
		return nil, http.StatusBadRequest, fmt.Sprintf("mcp document exceeds %d bytes", maxConfigMCPDocBytes)
	}
	sealed, err := h.sealConfigDocument(mergedDoc)
	if err != nil {
		if errors.Is(err, errConfigSecretKeyUnset) {
			return nil, http.StatusServiceUnavailable, "cannot store secrets: GOOSAR_MCP_SECRET_KEY is not configured on this server"
		}
		return nil, http.StatusInternalServerError, "failed to store mcp configuration"
	}
	return sealed, 0, ""
}

func isJSONNull(raw []byte) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

func (h *Handler) applyConfigLayerPatch(current configLayerState, patch putConfigLayerRequest) (configLayerState, int, string) {
	next := current

	if patch.LlmBaseURL != nil {
		if *patch.LlmBaseURL == "" {
			next.baseURL = ""
		} else {
			if err := validateConfigBaseURL(*patch.LlmBaseURL); err != nil {
				return configLayerState{}, http.StatusBadRequest, err.Error()
			}
			next.baseURL = *patch.LlmBaseURL
		}
	}
	if patch.LlmModel != nil {
		if len(*patch.LlmModel) > maxConfigModelLength {
			return configLayerState{}, http.StatusBadRequest, fmt.Sprintf("llm_model exceeds %d characters", maxConfigModelLength)
		}
		next.model = *patch.LlmModel
	}
	if patch.LlmAPIKey != nil {
		if len(*patch.LlmAPIKey) > maxConfigAPIKeyLength {
			return configLayerState{}, http.StatusBadRequest, fmt.Sprintf("llm_api_key exceeds %d characters", maxConfigAPIKeyLength)
		}
		sealed, err := h.sealConfigSecret(*patch.LlmAPIKey)
		if err != nil {
			if errors.Is(err, errConfigSecretKeyUnset) {
				return configLayerState{}, http.StatusServiceUnavailable, "cannot store secrets: GOOSAR_MCP_SECRET_KEY is not configured on this server"
			}
			return configLayerState{}, http.StatusInternalServerError, "failed to store llm_api_key"
		}
		next.apiKeySealed = sealed
	}
	if len(patch.McpDoc) > 0 {
		if isJSONNull(patch.McpDoc) {

			next.mcpSealed = nil
		} else {
			sealed, status, msg := h.mergeConfigMCPDoc(current.mcpSealed, patch.McpDoc)
			if status != 0 {
				return configLayerState{}, status, msg
			}
			next.mcpSealed = sealed
		}
	}
	return next, 0, ""
}

func optionalTime(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

func optionalUUIDString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuidToString(u)
}

type maskedMCPServerEntry struct {
	Enabled *bool           `json:"enabled,omitempty"`
	Env     map[string]bool `json:"env,omitempty"`
}

func (h *Handler) maskConfigDocument(stored []byte) (json.RawMessage, error) {
	doc, err := h.openConfigDocument(stored)
	if err != nil {
		return nil, err
	}
	entries, err := parseMCPLayerDoc(doc)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	masked := make(map[string]maskedMCPServerEntry, len(entries))
	for name, entry := range entries {
		out := maskedMCPServerEntry{Enabled: entry.Enabled}
		if len(entry.Env) > 0 {
			out.Env = make(map[string]bool, len(entry.Env))
			for envKey := range entry.Env {
				out.Env[envKey] = true
			}
		}
		masked[name] = out
	}
	raw, err := json.Marshal(masked)
	if err != nil {
		return nil, fmt.Errorf("mask config document: %w", err)
	}
	return raw, nil
}

func (h *Handler) maskConfigDocumentForResponse(w http.ResponseWriter, stored []byte, what string) (json.RawMessage, bool) {
	doc, err := h.maskConfigDocument(stored)
	if err != nil {
		slog.Error("workspace config: failed to open sealed document", "field", what, "error", err)
		writeError(w, http.StatusServiceUnavailable, "stored configuration is sealed and GOOSAR_MCP_SECRET_KEY is not available")
		return nil, false
	}
	return doc, true
}

func (h *Handler) GetWorkspaceConfig(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, viaDeploymentRole, ok := h.requireWorkspaceConfigAdmin(w, r)
	if !ok {
		return
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	state := configLayerState{}
	found := false
	row, err := h.Queries.GetWorkspaceConfig(r.Context(), wsUUID)
	switch {
	case err == nil:
		state = configLayerState{
			baseURL:      row.LlmBaseUrl.String,
			model:        row.LlmModel.String,
			apiKeySealed: row.LlmApiKey,
			mcpSealed:    row.McpDefaults,
		}
		found = true
	case errors.Is(err, pgx.ErrNoRows):

	default:
		slog.Error("workspace config: failed to load", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load workspace configuration")
		return
	}

	if viaDeploymentRole {
		hash, hashErr := h.configLayerAuditHash(state)
		if hashErr != nil {
			slog.Error("workspace config: audit hash failed", "workspace_id", workspaceID, "error", hashErr)
			writeError(w, http.StatusServiceUnavailable, "stored configuration is sealed and GOOSAR_MCP_SECRET_KEY is not available")
			return
		}
		if auditErr := auditDeploymentConfigOp(r.Context(), h.Queries, r, member.UserID,
			adminAuditActionConfigRead, "workspace", workspaceID, hash, hash); auditErr != nil {
			slog.Error("workspace config: audit insert failed", "workspace_id", workspaceID, "error", auditErr)
			writeError(w, http.StatusInternalServerError, "failed to journal configuration access")
			return
		}
	}
	if !found {
		writeJSON(w, http.StatusOK, WorkspaceConfigResponse{})
		return
	}
	mcpDoc, ok := h.maskConfigDocumentForResponse(w, row.McpDefaults, "mcp_defaults")
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, WorkspaceConfigResponse{
		LlmBaseURL:   row.LlmBaseUrl.String,
		LlmModel:     row.LlmModel.String,
		HasLlmAPIKey: len(row.LlmApiKey) > 0,
		McpDefaults:  mcpDoc,
		UpdatedAt:    optionalTime(row.UpdatedAt),
		UpdatedBy:    optionalUUIDString(row.UpdatedBy),
	})
}

func (h *Handler) PutWorkspaceConfig(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, viaDeploymentRole, ok := h.requireWorkspaceConfigAdmin(w, r)
	if !ok {
		return
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	var body putWorkspaceConfigBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	patch := body.putConfigLayerRequest
	patch.McpDoc = body.McpDefaults

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("workspace config: failed to begin tx", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update workspace configuration")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockConfigLayerKey(r.Context(), "config:"+workspaceID); err != nil {
		slog.Error("workspace config: failed to lock layer", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update workspace configuration")
		return
	}

	current := configLayerState{}
	existing, err := qtx.GetWorkspaceConfig(r.Context(), wsUUID)
	switch {
	case err == nil:
		current = configLayerState{
			baseURL:      existing.LlmBaseUrl.String,
			model:        existing.LlmModel.String,
			apiKeySealed: existing.LlmApiKey,
			mcpSealed:    existing.McpDefaults,
		}
	case errors.Is(err, pgx.ErrNoRows):
	default:
		slog.Error("workspace config: failed to load current row", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update workspace configuration")
		return
	}

	next, status, msg := h.applyConfigLayerPatch(current, patch)
	if status != 0 {
		writeError(w, status, msg)
		return
	}

	if viaDeploymentRole {
		beforeHash, hashErr := h.configLayerAuditHash(current)
		if hashErr == nil {
			var afterHash string
			afterHash, hashErr = h.configLayerAuditHash(next)
			if hashErr == nil {
				hashErr = auditDeploymentConfigOp(r.Context(), qtx, r, member.UserID,
					adminAuditActionWorkspaceConfigSet, "workspace", workspaceID, beforeHash, afterHash)
			}
		}
		if hashErr != nil {
			slog.Error("workspace config: audit failed", "workspace_id", workspaceID, "error", hashErr)
			writeError(w, http.StatusInternalServerError, "failed to journal configuration change")
			return
		}
	}

	row, err := qtx.UpsertWorkspaceConfig(r.Context(), db.UpsertWorkspaceConfigParams{
		WorkspaceID: wsUUID,
		LlmBaseUrl:  pgtype.Text{String: next.baseURL, Valid: next.baseURL != ""},
		LlmModel:    pgtype.Text{String: next.model, Valid: next.model != ""},
		LlmApiKey:   next.apiKeySealed,
		McpDefaults: next.mcpSealed,
		UpdatedBy:   member.UserID,
	})
	if err != nil {
		slog.Error("workspace config: failed to upsert", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update workspace configuration")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("workspace config: failed to commit", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update workspace configuration")
		return
	}

	mcpDoc, ok := h.maskConfigDocumentForResponse(w, row.McpDefaults, "mcp_defaults")
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, WorkspaceConfigResponse{
		LlmBaseURL:   row.LlmBaseUrl.String,
		LlmModel:     row.LlmModel.String,
		HasLlmAPIKey: len(row.LlmApiKey) > 0,
		McpDefaults:  mcpDoc,
		UpdatedAt:    optionalTime(row.UpdatedAt),
		UpdatedBy:    optionalUUIDString(row.UpdatedBy),
	})
}

func (h *Handler) resolveOverrideTarget(w http.ResponseWriter, r *http.Request, workspaceID string) (pgtype.UUID, bool) {
	targetUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "userId"), "userId")
	if !ok {
		return pgtype.UUID{}, false
	}
	if _, err := h.getWorkspaceMember(r.Context(), uuidToString(targetUUID), workspaceID); err != nil {
		writeError(w, http.StatusNotFound, "user is not a member of this workspace")
		return pgtype.UUID{}, false
	}
	return targetUUID, true
}

func (h *Handler) GetWorkspaceUserConfigOverride(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, viaDeploymentRole, ok := h.requireWorkspaceConfigAdmin(w, r)
	if !ok {
		return
	}
	targetUUID, ok := h.resolveOverrideTarget(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	row, err := h.Queries.GetUserConfigOverride(r.Context(), db.GetUserConfigOverrideParams{
		WorkspaceID: wsUUID,
		UserID:      targetUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no configuration override for this user")
		return
	}
	if err != nil {
		slog.Error("user config override: failed to load", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load configuration override")
		return
	}

	if viaDeploymentRole {
		hash, hashErr := h.configLayerAuditHash(configLayerState{
			baseURL:      row.LlmBaseUrl.String,
			model:        row.LlmModel.String,
			apiKeySealed: row.LlmApiKey,
			mcpSealed:    row.McpOverrides,
		})
		if hashErr == nil {
			hashErr = auditDeploymentConfigOp(r.Context(), h.Queries, r, member.UserID,
				adminAuditActionConfigRead, "workspace_user", overrideAuditTargetID(workspaceID, targetUUID), hash, hash)
		}
		if hashErr != nil {
			slog.Error("user config override: audit failed", "workspace_id", workspaceID, "error", hashErr)
			writeError(w, http.StatusInternalServerError, "failed to journal configuration access")
			return
		}
	}
	mcpDoc, ok := h.maskConfigDocumentForResponse(w, row.McpOverrides, "mcp_overrides")
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, UserConfigOverrideResponse{
		UserID:       uuidToString(row.UserID),
		LlmBaseURL:   row.LlmBaseUrl.String,
		LlmModel:     row.LlmModel.String,
		HasLlmAPIKey: len(row.LlmApiKey) > 0,
		McpOverrides: mcpDoc,
		UpdatedAt:    optionalTime(row.UpdatedAt),
		UpdatedBy:    optionalUUIDString(row.UpdatedBy),
	})
}

func (h *Handler) ListWorkspaceUserConfigOverrides(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, viaDeploymentRole, ok := h.requireWorkspaceConfigAdmin(w, r)
	if !ok {
		return
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	if _, err := h.Queries.GetWorkspace(r.Context(), wsUUID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		slog.Error("user config overrides: workspace lookup failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load workspace")
		return
	}
	rows, err := h.Queries.ListUserConfigOverrides(r.Context(), wsUUID)
	if err != nil {
		slog.Error("user config overrides: failed to list", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load configuration overrides")
		return
	}

	if viaDeploymentRole {
		hash, hashErr := h.overrideListAuditHash(rows)
		if hashErr == nil {
			hashErr = auditDeploymentConfigOp(r.Context(), h.Queries, r, member.UserID,
				adminAuditActionConfigRead, "workspace_overrides", workspaceID, hash, hash)
		}
		if hashErr != nil {
			slog.Error("user config overrides: audit failed", "workspace_id", workspaceID, "error", hashErr)
			writeError(w, http.StatusInternalServerError, "failed to journal configuration access")
			return
		}
	}
	entries := make([]UserConfigOverrideResponse, 0, len(rows))
	for _, row := range rows {
		mcpDoc, ok := h.maskConfigDocumentForResponse(w, row.McpOverrides, "mcp_overrides")
		if !ok {
			return
		}
		entries = append(entries, UserConfigOverrideResponse{
			UserID:       uuidToString(row.UserID),
			LlmBaseURL:   row.LlmBaseUrl.String,
			LlmModel:     row.LlmModel.String,
			HasLlmAPIKey: len(row.LlmApiKey) > 0,
			McpOverrides: mcpDoc,
			UpdatedAt:    optionalTime(row.UpdatedAt),
			UpdatedBy:    optionalUUIDString(row.UpdatedBy),
		})
	}
	writeJSON(w, http.StatusOK, entries)
}

func (h *Handler) overrideListAuditHash(rows []db.UserConfigOverride) (string, error) {
	type overrideListAuditEntry struct {
		UserID string              `json:"user_id"`
		Layer  configLayerAuditDoc `json:"layer"`
	}
	entries := make([]overrideListAuditEntry, 0, len(rows))
	for _, row := range rows {
		masked, err := h.maskConfigDocument(row.McpOverrides)
		if err != nil {
			return "", err
		}
		entries = append(entries, overrideListAuditEntry{
			UserID: uuidToString(row.UserID),
			Layer: configLayerAuditDoc{
				LlmBaseURL:   row.LlmBaseUrl.String,
				LlmModel:     row.LlmModel.String,
				HasLlmAPIKey: len(row.LlmApiKey) > 0,
				MCP:          masked,
			},
		})
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("override list audit hash: %w", err)
	}
	return deploymentPolicyHash(raw), nil
}

func (h *Handler) PutWorkspaceUserConfigOverride(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, viaDeploymentRole, ok := h.requireWorkspaceConfigAdmin(w, r)
	if !ok {
		return
	}
	targetUUID, ok := h.resolveOverrideTarget(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	var body putUserConfigOverrideBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	patch := body.putConfigLayerRequest
	patch.McpDoc = body.McpOverrides

	current := configLayerState{}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("user config override: failed to begin tx", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update configuration override")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockConfigLayerKey(r.Context(), "config:"+workspaceID+":"+uuidToString(targetUUID)); err != nil {
		slog.Error("user config override: failed to lock layer", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update configuration override")
		return
	}

	existing, err := qtx.GetUserConfigOverride(r.Context(), db.GetUserConfigOverrideParams{
		WorkspaceID: wsUUID,
		UserID:      targetUUID,
	})
	switch {
	case err == nil:
		current = configLayerState{
			baseURL:      existing.LlmBaseUrl.String,
			model:        existing.LlmModel.String,
			apiKeySealed: existing.LlmApiKey,
			mcpSealed:    existing.McpOverrides,
		}
	case errors.Is(err, pgx.ErrNoRows):
	default:
		slog.Error("user config override: failed to load current row", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update configuration override")
		return
	}

	next, status, msg := h.applyConfigLayerPatch(current, patch)
	if status != 0 {
		writeError(w, status, msg)
		return
	}

	if viaDeploymentRole {
		beforeHash, hashErr := h.configLayerAuditHash(current)
		if hashErr == nil {
			var afterHash string
			afterHash, hashErr = h.configLayerAuditHash(next)
			if hashErr == nil {
				hashErr = auditDeploymentConfigOp(r.Context(), qtx, r, member.UserID,
					adminAuditActionUserOverrideSet, "workspace_user", overrideAuditTargetID(workspaceID, targetUUID), beforeHash, afterHash)
			}
		}
		if hashErr != nil {
			slog.Error("user config override: audit failed", "workspace_id", workspaceID, "error", hashErr)
			writeError(w, http.StatusInternalServerError, "failed to journal configuration change")
			return
		}
	}

	row, err := qtx.UpsertUserConfigOverride(r.Context(), db.UpsertUserConfigOverrideParams{
		WorkspaceID:  wsUUID,
		UserID:       targetUUID,
		LlmBaseUrl:   pgtype.Text{String: next.baseURL, Valid: next.baseURL != ""},
		LlmModel:     pgtype.Text{String: next.model, Valid: next.model != ""},
		LlmApiKey:    next.apiKeySealed,
		McpOverrides: next.mcpSealed,
		UpdatedBy:    member.UserID,
	})
	if err != nil {
		slog.Error("user config override: failed to upsert", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update configuration override")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("user config override: failed to commit", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update configuration override")
		return
	}

	mcpDoc, ok := h.maskConfigDocumentForResponse(w, row.McpOverrides, "mcp_overrides")
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, UserConfigOverrideResponse{
		UserID:       uuidToString(row.UserID),
		LlmBaseURL:   row.LlmBaseUrl.String,
		LlmModel:     row.LlmModel.String,
		HasLlmAPIKey: len(row.LlmApiKey) > 0,
		McpOverrides: mcpDoc,
		UpdatedAt:    optionalTime(row.UpdatedAt),
		UpdatedBy:    optionalUUIDString(row.UpdatedBy),
	})
}

func (h *Handler) DeleteWorkspaceUserConfigOverride(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, viaDeploymentRole, ok := h.requireWorkspaceConfigAdmin(w, r)
	if !ok {
		return
	}
	targetUUID, ok := h.resolveOverrideTarget(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("user config override: failed to begin tx", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete configuration override")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockConfigLayerKey(r.Context(), "config:"+workspaceID+":"+uuidToString(targetUUID)); err != nil {
		slog.Error("user config override: failed to lock layer", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete configuration override")
		return
	}

	if viaDeploymentRole {
		before := configLayerState{}
		existing, loadErr := qtx.GetUserConfigOverride(r.Context(), db.GetUserConfigOverrideParams{
			WorkspaceID: wsUUID,
			UserID:      targetUUID,
		})
		switch {
		case loadErr == nil:
			before = configLayerState{
				baseURL:      existing.LlmBaseUrl.String,
				model:        existing.LlmModel.String,
				apiKeySealed: existing.LlmApiKey,
				mcpSealed:    existing.McpOverrides,
			}
		case errors.Is(loadErr, pgx.ErrNoRows):

		default:
			slog.Error("user config override: failed to load for audit", "workspace_id", workspaceID, "error", loadErr)
			writeError(w, http.StatusInternalServerError, "failed to delete configuration override")
			return
		}
		beforeHash, hashErr := h.configLayerAuditHash(before)
		if hashErr == nil {
			hashErr = auditDeploymentConfigOp(r.Context(), qtx, r, member.UserID,
				adminAuditActionUserOverrideDelete, "workspace_user", overrideAuditTargetID(workspaceID, targetUUID), beforeHash, "")
		}
		if hashErr != nil {
			slog.Error("user config override: audit failed", "workspace_id", workspaceID, "error", hashErr)
			writeError(w, http.StatusInternalServerError, "failed to journal configuration change")
			return
		}
	}
	if _, err := qtx.DeleteUserConfigOverride(r.Context(), db.DeleteUserConfigOverrideParams{
		WorkspaceID: wsUUID,
		UserID:      targetUUID,
	}); err != nil {
		slog.Error("user config override: failed to delete", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete configuration override")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("user config override: failed to commit delete", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete configuration override")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetDeploymentPolicy(w http.ResponseWriter, r *http.Request) {

	_, _, _, ok := h.requireWorkspaceConfigAdmin(w, r)
	if !ok {
		return
	}
	row, err := h.Queries.GetDeploymentPolicy(r.Context())
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, DeploymentPolicyResponse{Policy: json.RawMessage("{}")})
		return
	}
	if err != nil {
		slog.Error("deployment policy: failed to load", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load deployment policy")
		return
	}
	policy := json.RawMessage(row.Policy)
	if len(policy) == 0 {
		policy = json.RawMessage("{}")
	}
	writeJSON(w, http.StatusOK, DeploymentPolicyResponse{
		Policy:    policy,
		UpdatedAt: optionalTime(row.UpdatedAt),
	})
}
