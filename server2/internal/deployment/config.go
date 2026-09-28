package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/seal"
	"github.com/adanman/goosar/server2/internal/store"
)

// requireConfigAccess — общий пролог всех ручек слоя конфигурации воркспейса,
// резолвящих пространство по заголовку/query (contract §10 "Доступ:
// owner/admin пространства ИЛИ deployment-admin"): человек, пространство
// найдено, и вызывающий либо owner/admin этого пространства, либо
// deployment-admin (тогда viaAdmin=true — хендлер обязан дописать
// config.read/*.set в admin-аудит).
func (d *Deps) requireConfigAccess(w http.ResponseWriter, r *http.Request) (actor *httpapi.Actor, workspaceID string, viaAdmin bool, ok bool) {
	a, aok := httpapi.RequireHuman(w, r)
	if !aok {
		return nil, "", false, false
	}
	wsID, wok := d.resolveWorkspaceID(r, a)
	if !wok {
		httpapi.BadRequest(w, "workspace_id/slug is required")
		return nil, "", false, false
	}
	return d.authorizeConfigAccess(w, r, a, wsID)
}

// requireConfigAccessByID — то же самое, но пространство уже известно из
// пути ({workspaceId}), используется административным входом
// /api/deployment/workspaces/{workspaceId}/config**.
func (d *Deps) requireConfigAccessByID(w http.ResponseWriter, r *http.Request, workspaceID string) (actor *httpapi.Actor, viaAdmin bool, ok bool) {
	a, aok := httpapi.RequireHuman(w, r)
	if !aok {
		return nil, false, false
	}
	_, _, viaAdmin, ok = d.authorizeConfigAccess(w, r, a, workspaceID)
	return a, viaAdmin, ok
}

func (d *Deps) authorizeConfigAccess(w http.ResponseWriter, r *http.Request, actor *httpapi.Actor, workspaceID string) (*httpapi.Actor, string, bool, bool) {
	role, memberOK, err := d.Workspace.HTTPAPIMembership().MemberRole(r.Context(), workspaceID, actor.UserID)
	if err != nil {
		internalError(w)
		return nil, "", false, false
	}
	if memberOK && httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin) {
		return actor, workspaceID, false, true
	}
	isAdmin, err := IsAdmin(r.Context(), d.DB, actor.UserID)
	if err != nil {
		internalError(w)
		return nil, "", false, false
	}
	if isAdmin {
		return actor, workspaceID, true, true
	}
	httpapi.Forbidden(w, "owner/admin of this workspace or deployment-admin required")
	return nil, "", false, false
}

// --- слой конфигурации (space_config) -----------------------------------------

func (d *Deps) readConfigLayer(ctx context.Context, workspaceID string) (ConfigLayer, error) {
	var layer ConfigLayer
	var baseURL, model *string
	var sealedKey []byte
	var mcpRaw []byte
	var updatedBy *string
	err := d.DB.Pool.QueryRow(ctx, `
		SELECT cfg_llm_base_url, cfg_llm_model, cfg_llm_api_key_sealed, cfg_mcp_defaults, cfg_updated_by, updated_at
		FROM space_config WHERE workspace_id = $1`, workspaceID,
	).Scan(&baseURL, &model, &sealedKey, &mcpRaw, &updatedBy, &layer.UpdatedAt)
	if store.IsNoRows(err) {
		layer.MCPDefaults = map[string]any{}
		return layer, nil
	}
	if err != nil {
		return ConfigLayer{}, err
	}
	if baseURL != nil {
		layer.LLMBaseURL = *baseURL
	}
	if model != nil {
		layer.LLMModel = *model
	}
	layer.HasLLMAPIKey = len(sealedKey) > 0
	layer.UpdatedBy = updatedBy
	var mcp map[string]any
	if len(mcpRaw) > 0 {
		_ = json.Unmarshal(mcpRaw, &mcp)
	}
	layer.MCPDefaults = maskMcpDocument(mcp)
	return layer, nil
}

// maskMcpDocument — contract §10: "mcp_defaults возвращается маскированным:
// значения env заменены на true/false (есть/нет)".
func maskMcpDocument(doc map[string]any) map[string]any {
	out := map[string]any{}
	for name, v := range doc {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		masked := map[string]any{}
		if enabled, has := entry["enabled"]; has {
			masked["enabled"] = enabled
		}
		if envRaw, has := entry["env"].(map[string]any); has {
			env := map[string]any{}
			for k := range envRaw {
				env[k] = true
			}
			masked["env"] = env
		}
		out[name] = masked
	}
	return out
}

// mergeMcpDocument — mcp_defaults/mcp_overrides — {имя_сервера: {enabled?,
// env?}}: значение null на верхнем уровне поля полностью очищает документ
// (обрабатывается вызывающим кодом до вызова этой функции); здесь —
// поэлементное слияние: null у сервера удаляет его целиком, null у значения
// env удаляет переменную, отсутствующий ключ сервера/env не трогается.
func mergeMcpDocument(current map[string]any, patchRaw json.RawMessage) (map[string]any, error) {
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(patchRaw, &patch); err != nil {
		return nil, err
	}
	out := map[string]any{}
	for k, v := range current {
		out[k] = v
	}
	for name, rawEntry := range patch {
		if string(rawEntry) == "null" {
			delete(out, name)
			continue
		}
		var patchEntry struct {
			Enabled *bool              `json:"enabled"`
			Env     map[string]*string `json:"env"`
		}
		if err := json.Unmarshal(rawEntry, &patchEntry); err != nil {
			return nil, err
		}
		existing, _ := out[name].(map[string]any)
		merged := map[string]any{}
		for k, v := range existing {
			merged[k] = v
		}
		if patchEntry.Enabled != nil {
			merged["enabled"] = *patchEntry.Enabled
		}
		if patchEntry.Env != nil {
			env, _ := merged["env"].(map[string]any)
			if env == nil {
				env = map[string]any{}
			}
			for k, v := range patchEntry.Env {
				if v == nil {
					delete(env, k)
				} else {
					env[k] = *v
				}
			}
			merged["env"] = env
		}
		out[name] = merged
	}
	return out, nil
}

// decodePutConfigRaw — разбирает тело PUT дважды: строго типизированную часть
// (llm_base_url/model/api_key) и присутствие/форму mcp_defaults как raw JSON,
// чтобы отличить "поле отсутствует" (не трогать) от "поле передано как null"
// (очистить документ целиком) — контракт явно требует эту трёхвариантность.
func decodePutConfigRaw(body []byte, mcpFieldName string) (req PutConfigRequest, mcpPresent bool, mcpNull bool, mcpRaw json.RawMessage, err error) {
	if err = json.Unmarshal(body, &req); err != nil {
		return
	}
	var generic map[string]json.RawMessage
	if err = json.Unmarshal(body, &generic); err != nil {
		return
	}
	raw, has := generic[mcpFieldName]
	mcpPresent = has
	if has {
		mcpNull = string(raw) == "null"
		mcpRaw = raw
	}
	return
}

func validatePutConfigRequest(req PutConfigRequest) (msg string, ok bool) {
	if req.LLMBaseURL != nil && *req.LLMBaseURL != "" {
		if len(*req.LLMBaseURL) > maxLLMBaseURLLen || !isAbsoluteHTTPURL(*req.LLMBaseURL) {
			return "llm_base_url must be an absolute http(s) URL", false
		}
	}
	if req.LLMModel != nil && len(*req.LLMModel) > maxLLMModelLen {
		return "llm_model too long", false
	}
	if req.LLMAPIKey != nil && len(*req.LLMAPIKey) > maxLLMAPIKeyLen {
		return "llm_api_key too long", false
	}
	return "", true
}

// writeConfigLayer — общая реализация PUT /api/workspace-config и её
// deployment-admin входа: частичный патч по (workspaceID) либо, если
// accountID задан, по override того участника.
func (d *Deps) writeConfigLayer(r *http.Request, workspaceID, overrideAccountID, actorID string, req PutConfigRequest, mcpFieldName string, mcpPresent, mcpNull bool, mcpRaw json.RawMessage) (ConfigLayer, error) {
	needsKey := (req.LLMAPIKey != nil && *req.LLMAPIKey != "") || (mcpPresent && !mcpNull)
	if needsKey && !seal.Available(d.Config.McpSecretKey) {
		return ConfigLayer{}, errEncryptionUnavailable
	}

	var currentMcpRaw []byte
	var currentBaseURL, currentModel *string
	var currentKeySealed []byte
	if overrideAccountID == "" {
		_ = d.DB.Pool.QueryRow(r.Context(), `SELECT cfg_llm_base_url, cfg_llm_model, cfg_llm_api_key_sealed, cfg_mcp_defaults FROM space_config WHERE workspace_id = $1`, workspaceID).
			Scan(&currentBaseURL, &currentModel, &currentKeySealed, &currentMcpRaw)
	} else {
		_ = d.DB.Pool.QueryRow(r.Context(), `SELECT cfgo_llm_base_url, cfgo_llm_model, cfgo_llm_api_key_sealed, cfgo_mcp_overrides FROM space_config_overrides WHERE workspace_id = $1 AND account_id = $2`, workspaceID, overrideAccountID).
			Scan(&currentBaseURL, &currentModel, &currentKeySealed, &currentMcpRaw)
	}

	var currentMcp map[string]any
	if len(currentMcpRaw) > 0 {
		_ = json.Unmarshal(currentMcpRaw, &currentMcp)
	}
	newMcp := currentMcp
	if mcpPresent {
		if mcpNull {
			newMcp = map[string]any{}
		} else {
			merged, err := mergeMcpDocument(currentMcp, mcpRaw)
			if err != nil {
				return ConfigLayer{}, errBadMcpDocument
			}
			newMcp = merged
		}
	}
	if newMcp == nil {
		newMcp = map[string]any{}
	}
	newMcpJSON, _ := json.Marshal(newMcp)
	if len(newMcpJSON) > maxConfigDocBytes {
		return ConfigLayer{}, errDocumentTooLarge
	}

	newBaseURL := currentBaseURL
	if req.LLMBaseURL != nil {
		if *req.LLMBaseURL == "" {
			newBaseURL = nil
		} else {
			newBaseURL = req.LLMBaseURL
		}
	}
	newModel := currentModel
	if req.LLMModel != nil {
		if *req.LLMModel == "" {
			newModel = nil
		} else {
			newModel = req.LLMModel
		}
	}
	newKeySealed := currentKeySealed
	if req.LLMAPIKey != nil {
		if *req.LLMAPIKey == "" {
			newKeySealed = nil
		} else {
			sealed, err := seal.Seal(d.Config.McpSecretKey, []byte(*req.LLMAPIKey))
			if err != nil {
				return ConfigLayer{}, errEncryptionUnavailable
			}
			newKeySealed = sealed
		}
	}

	var updatedAt time.Time
	if overrideAccountID == "" {
		err := d.DB.Pool.QueryRow(r.Context(), `
			INSERT INTO space_config (workspace_id, cfg_llm_base_url, cfg_llm_model, cfg_llm_api_key_sealed, cfg_mcp_defaults, cfg_updated_by, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())
			ON CONFLICT (workspace_id) DO UPDATE SET
				cfg_llm_base_url = EXCLUDED.cfg_llm_base_url, cfg_llm_model = EXCLUDED.cfg_llm_model,
				cfg_llm_api_key_sealed = EXCLUDED.cfg_llm_api_key_sealed, cfg_mcp_defaults = EXCLUDED.cfg_mcp_defaults,
				cfg_updated_by = EXCLUDED.cfg_updated_by, updated_at = now()
			RETURNING updated_at`, workspaceID, newBaseURL, newModel, newKeySealed, newMcpJSON, actorID,
		).Scan(&updatedAt)
		if err != nil {
			return ConfigLayer{}, err
		}
	} else {
		err := d.DB.Pool.QueryRow(r.Context(), `
			INSERT INTO space_config_overrides (workspace_id, account_id, cfgo_llm_base_url, cfgo_llm_model, cfgo_llm_api_key_sealed, cfgo_mcp_overrides, cfgo_updated_by, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, now())
			ON CONFLICT (workspace_id, account_id) DO UPDATE SET
				cfgo_llm_base_url = EXCLUDED.cfgo_llm_base_url, cfgo_llm_model = EXCLUDED.cfgo_llm_model,
				cfgo_llm_api_key_sealed = EXCLUDED.cfgo_llm_api_key_sealed, cfgo_mcp_overrides = EXCLUDED.cfgo_mcp_overrides,
				cfgo_updated_by = EXCLUDED.cfgo_updated_by, updated_at = now()
			RETURNING updated_at`, workspaceID, overrideAccountID, newBaseURL, newModel, newKeySealed, newMcpJSON, actorID,
		).Scan(&updatedAt)
		if err != nil {
			return ConfigLayer{}, err
		}
	}

	layer := ConfigLayer{HasLLMAPIKey: len(newKeySealed) > 0, MCPDefaults: maskMcpDocument(newMcp), UpdatedAt: &updatedAt, UpdatedBy: ptr(actorID)}
	if newBaseURL != nil {
		layer.LLMBaseURL = *newBaseURL
	}
	if newModel != nil {
		layer.LLMModel = *newModel
	}
	return layer, nil
}

var (
	errEncryptionUnavailable = errors.New("GOOSAR_MCP_SECRET_KEY is not configured")
	errBadMcpDocument        = errors.New("mcp_defaults/mcp_overrides must be an object of {enabled?, env?}")
	errDocumentTooLarge      = errors.New("config document exceeds 64 KB")
)

func writeConfigError(w http.ResponseWriter, err error) {
	switch err {
	case errEncryptionUnavailable:
		httpapi.WriteError(w, http.StatusServiceUnavailable, err.Error(), "encryption_unavailable")
	case errBadMcpDocument, errDocumentTooLarge:
		httpapi.BadRequest(w, err.Error())
	default:
		internalError(w)
	}
}

// --- HTTP: /api/workspace-config ----------------------------------------------

func (d *Deps) handleGetWorkspaceConfig(w http.ResponseWriter, r *http.Request) {
	actor, workspaceID, viaAdmin, ok := d.requireConfigAccess(w, r)
	if !ok {
		return
	}
	layer, err := d.readConfigLayer(r.Context(), workspaceID)
	if checkErr(w, err) {
		return
	}
	if viaAdmin {
		audit := httpAudit(r, actor, "config.read")
		audit.WorkspaceID, audit.TargetType, audit.TargetID = ptr(workspaceID), ptr("workspace_config"), ptr(workspaceID)
		audit.AfterHash = ptr(seal.HashJSON(layer))
		_ = WriteAudit(r.Context(), d.DB, audit)
	}
	httpapi.WriteJSON(w, http.StatusOK, layer)
}

func (d *Deps) handlePutWorkspaceConfig(w http.ResponseWriter, r *http.Request) {
	actor, workspaceID, viaAdmin, ok := d.requireConfigAccess(w, r)
	if !ok {
		return
	}
	d.putWorkspaceConfigCommon(w, r, actor, workspaceID, viaAdmin)
}

func (d *Deps) putWorkspaceConfigCommon(w http.ResponseWriter, r *http.Request, actor *httpapi.Actor, workspaceID string, viaAdmin bool) {
	body, err := httpapi.ReadBody(r)
	if err != nil {
		httpapi.BadRequest(w, "invalid body")
		return
	}
	req, present, isNull, raw, err := decodePutConfigRaw(body, "mcp_defaults")
	if err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if msg, ok := validatePutConfigRequest(req); !ok {
		httpapi.BadRequest(w, msg)
		return
	}
	before, _ := d.readConfigLayer(r.Context(), workspaceID)
	layer, err := d.writeConfigLayer(r, workspaceID, "", actor.UserID, req, "mcp_defaults", present, isNull, raw)
	if err != nil {
		writeConfigError(w, err)
		return
	}
	if viaAdmin {
		audit := httpAudit(r, actor, "workspace_config.set")
		audit.WorkspaceID, audit.TargetType, audit.TargetID = ptr(workspaceID), ptr("workspace_config"), ptr(workspaceID)
		audit.BeforeHash, audit.AfterHash = ptr(seal.HashJSON(before)), ptr(seal.HashJSON(layer))
		_ = WriteAudit(r.Context(), d.DB, audit)
	}
	httpapi.WriteJSON(w, http.StatusOK, layer)
}

// --- overrides -----------------------------------------------------------------

func (d *Deps) readConfigOverride(ctx context.Context, workspaceID, userID string) (UserConfigOverride, bool, error) {
	var o UserConfigOverride
	var baseURL, model *string
	var sealedKey []byte
	var mcpRaw []byte
	err := d.DB.Pool.QueryRow(ctx, `
		SELECT cfgo_llm_base_url, cfgo_llm_model, cfgo_llm_api_key_sealed, cfgo_mcp_overrides, cfgo_updated_by, updated_at
		FROM space_config_overrides WHERE workspace_id = $1 AND account_id = $2`, workspaceID, userID,
	).Scan(&baseURL, &model, &sealedKey, &mcpRaw, &o.UpdatedBy, &o.UpdatedAt)
	if store.IsNoRows(err) {
		return UserConfigOverride{}, false, nil
	}
	if err != nil {
		return UserConfigOverride{}, false, err
	}
	o.UserID = userID
	if baseURL != nil {
		o.LLMBaseURL = *baseURL
	}
	if model != nil {
		o.LLMModel = *model
	}
	o.HasLLMAPIKey = len(sealedKey) > 0
	var mcp map[string]any
	if len(mcpRaw) > 0 {
		_ = json.Unmarshal(mcpRaw, &mcp)
	}
	o.MCPOverrides = maskMcpDocument(mcp)
	return o, true, nil
}

// rawConfigLayer — форма space_config/space_config_overrides без маскировки,
// только для внутреннего наложения слоёв (effective_config.go) — никогда не
// сериализуется в HTTP-ответ напрямую.
type rawConfigLayer struct {
	baseURL, model *string
	keySealed      []byte
	mcp            map[string]any
}

func (d *Deps) readConfigLayerRaw(ctx context.Context, workspaceID string) (rawConfigLayer, error) {
	var c rawConfigLayer
	var mcpRaw []byte
	err := d.DB.Pool.QueryRow(ctx, `
		SELECT cfg_llm_base_url, cfg_llm_model, cfg_llm_api_key_sealed, cfg_mcp_defaults
		FROM space_config WHERE workspace_id = $1`, workspaceID).Scan(&c.baseURL, &c.model, &c.keySealed, &mcpRaw)
	if store.IsNoRows(err) {
		return rawConfigLayer{mcp: map[string]any{}}, nil
	}
	if err != nil {
		return rawConfigLayer{}, err
	}
	if len(mcpRaw) > 0 {
		_ = json.Unmarshal(mcpRaw, &c.mcp)
	}
	if c.mcp == nil {
		c.mcp = map[string]any{}
	}
	return c, nil
}

func (d *Deps) readConfigOverrideRaw(ctx context.Context, workspaceID, userID string) (rawConfigLayer, bool, error) {
	var c rawConfigLayer
	var mcpRaw []byte
	err := d.DB.Pool.QueryRow(ctx, `
		SELECT cfgo_llm_base_url, cfgo_llm_model, cfgo_llm_api_key_sealed, cfgo_mcp_overrides
		FROM space_config_overrides WHERE workspace_id = $1 AND account_id = $2`, workspaceID, userID).
		Scan(&c.baseURL, &c.model, &c.keySealed, &mcpRaw)
	if store.IsNoRows(err) {
		return rawConfigLayer{}, false, nil
	}
	if err != nil {
		return rawConfigLayer{}, false, err
	}
	if len(mcpRaw) > 0 {
		_ = json.Unmarshal(mcpRaw, &c.mcp)
	}
	if c.mcp == nil {
		c.mcp = map[string]any{}
	}
	return c, true, nil
}

func (d *Deps) isWorkspaceMember(r *http.Request, workspaceID, userID string) bool {
	_, ok, err := d.Workspace.HTTPAPIMembership().MemberRole(r.Context(), workspaceID, userID)
	return err == nil && ok
}

func (d *Deps) handleGetWorkspaceUserOverride(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, _, ok := d.requireConfigAccess(w, r)
	if !ok {
		return
	}
	userID := r.PathValue("userId")
	if !d.isWorkspaceMember(r, workspaceID, userID) {
		httpapi.NotFound(w, "not a member of this workspace")
		return
	}
	o, found, err := d.readConfigOverride(r.Context(), workspaceID, userID)
	if checkErr(w, err) {
		return
	}
	if !found {
		httpapi.NotFound(w, "override not set")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, o)
}

func (d *Deps) handlePutWorkspaceUserOverride(w http.ResponseWriter, r *http.Request) {
	actor, workspaceID, viaAdmin, ok := d.requireConfigAccess(w, r)
	if !ok {
		return
	}
	userID := r.PathValue("userId")
	if !d.isWorkspaceMember(r, workspaceID, userID) {
		httpapi.NotFound(w, "target user is not a member of this workspace")
		return
	}
	body, err := httpapi.ReadBody(r)
	if err != nil {
		httpapi.BadRequest(w, "invalid body")
		return
	}
	req, present, isNull, raw, err := decodePutConfigRaw(body, "mcp_overrides")
	if err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if msg, ok := validatePutConfigRequest(req); !ok {
		httpapi.BadRequest(w, msg)
		return
	}
	layer, err := d.writeConfigLayer(r, workspaceID, userID, actor.UserID, req, "mcp_overrides", present, isNull, raw)
	if err != nil {
		writeConfigError(w, err)
		return
	}
	o := UserConfigOverride{UserID: userID, LLMBaseURL: layer.LLMBaseURL, LLMModel: layer.LLMModel,
		HasLLMAPIKey: layer.HasLLMAPIKey, MCPOverrides: layer.MCPDefaults, UpdatedAt: layer.UpdatedAt, UpdatedBy: layer.UpdatedBy}
	if viaAdmin {
		audit := httpAudit(r, actor, "user_config_override.set")
		audit.WorkspaceID, audit.TargetType, audit.TargetID = ptr(workspaceID), ptr("account"), ptr(userID)
		_ = WriteAudit(r.Context(), d.DB, audit)
	}
	httpapi.WriteJSON(w, http.StatusOK, o)
}

func (d *Deps) handleDeleteWorkspaceUserOverride(w http.ResponseWriter, r *http.Request) {
	actor, workspaceID, viaAdmin, ok := d.requireConfigAccess(w, r)
	if !ok {
		return
	}
	userID := r.PathValue("userId")
	if !d.isWorkspaceMember(r, workspaceID, userID) {
		httpapi.NotFound(w, "target user is not a member of this workspace")
		return
	}
	if _, err := d.DB.Pool.Exec(r.Context(), `DELETE FROM space_config_overrides WHERE workspace_id = $1 AND account_id = $2`, workspaceID, userID); err != nil {
		internalError(w)
		return
	}
	if viaAdmin {
		audit := httpAudit(r, actor, "user_config_override.delete")
		audit.WorkspaceID, audit.TargetType, audit.TargetID = ptr(workspaceID), ptr("account"), ptr(userID)
		_ = WriteAudit(r.Context(), d.DB, audit)
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- административный вход по /api/deployment/workspaces/{workspaceId}/... ---

func (d *Deps) handleGetWorkspaceConfigByID(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, viaAdmin, ok := d.requireConfigAccessByID(w, r, workspaceID)
	if !ok {
		return
	}
	layer, err := d.readConfigLayer(r.Context(), workspaceID)
	if checkErr(w, err) {
		return
	}
	if viaAdmin {
		audit := httpAudit(r, actor, "config.read")
		audit.WorkspaceID, audit.TargetType, audit.TargetID = ptr(workspaceID), ptr("workspace_config"), ptr(workspaceID)
		_ = WriteAudit(r.Context(), d.DB, audit)
	}
	httpapi.WriteJSON(w, http.StatusOK, layer)
}

func (d *Deps) handlePutWorkspaceConfigByID(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, viaAdmin, ok := d.requireConfigAccessByID(w, r, workspaceID)
	if !ok {
		return
	}
	d.putWorkspaceConfigCommon(w, r, actor, workspaceID, viaAdmin)
}

func (d *Deps) handleListWorkspaceConfigOverridesByID(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	if _, _, ok := d.requireConfigAccessByID(w, r, workspaceID); !ok {
		return
	}
	members, err := d.Workspace.ListMembers(r.Context(), workspaceID)
	if checkErr(w, err) {
		return
	}
	out := []UserConfigOverride{}
	for _, m := range members {
		if o, found, err := d.readConfigOverride(r.Context(), workspaceID, m.UserID); err == nil && found {
			out = append(out, o)
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleGetWorkspaceUserOverrideByID(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	if _, _, ok := d.requireConfigAccessByID(w, r, workspaceID); !ok {
		return
	}
	userID := r.PathValue("userId")
	o, found, err := d.readConfigOverride(r.Context(), workspaceID, userID)
	if checkErr(w, err) {
		return
	}
	if !found {
		httpapi.NotFound(w, "override not set")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, o)
}

func (d *Deps) handlePutWorkspaceUserOverrideByID(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, viaAdmin, ok := d.requireConfigAccessByID(w, r, workspaceID)
	if !ok {
		return
	}
	userID := r.PathValue("userId")
	if !d.isWorkspaceMember(r, workspaceID, userID) {
		httpapi.NotFound(w, "target user is not a member of this workspace")
		return
	}
	body, err := httpapi.ReadBody(r)
	if err != nil {
		httpapi.BadRequest(w, "invalid body")
		return
	}
	req, present, isNull, raw, err := decodePutConfigRaw(body, "mcp_overrides")
	if err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if msg, ok := validatePutConfigRequest(req); !ok {
		httpapi.BadRequest(w, msg)
		return
	}
	layer, err := d.writeConfigLayer(r, workspaceID, userID, actor.UserID, req, "mcp_overrides", present, isNull, raw)
	if err != nil {
		writeConfigError(w, err)
		return
	}
	o := UserConfigOverride{UserID: userID, LLMBaseURL: layer.LLMBaseURL, LLMModel: layer.LLMModel,
		HasLLMAPIKey: layer.HasLLMAPIKey, MCPOverrides: layer.MCPDefaults, UpdatedAt: layer.UpdatedAt, UpdatedBy: layer.UpdatedBy}
	if viaAdmin {
		audit := httpAudit(r, actor, "user_config_override.set")
		audit.WorkspaceID, audit.TargetType, audit.TargetID = ptr(workspaceID), ptr("account"), ptr(userID)
		_ = WriteAudit(r.Context(), d.DB, audit)
	}
	httpapi.WriteJSON(w, http.StatusOK, o)
}

func (d *Deps) handleDeleteWorkspaceUserOverrideByID(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, viaAdmin, ok := d.requireConfigAccessByID(w, r, workspaceID)
	if !ok {
		return
	}
	userID := r.PathValue("userId")
	if _, err := d.DB.Pool.Exec(r.Context(), `DELETE FROM space_config_overrides WHERE workspace_id = $1 AND account_id = $2`, workspaceID, userID); err != nil {
		internalError(w)
		return
	}
	if viaAdmin {
		audit := httpAudit(r, actor, "user_config_override.delete")
		audit.WorkspaceID, audit.TargetType, audit.TargetID = ptr(workspaceID), ptr("account"), ptr(userID)
		_ = WriteAudit(r.Context(), d.DB, audit)
	}
	w.WriteHeader(http.StatusNoContent)
}
