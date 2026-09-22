// Поверхность записи политики деплоя: чтение и замена документа верхнего
// уровня, который резолвер effective_config применяет поверх слоёв
// пространства и пользователя. Ключевой сценарий — глобально выключить и
// заблокировать MCP одним PUT с wildcard-записью "*". Схема — только
// перечислимые поля, без произвольных и без секретов; каждое изменение
// пишет в admin_audit хэши документа до/после и рассылается на все машины
// деплоя.
package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const deploymentPolicyLockKey = "deployment:policy"

const maxDeploymentPolicyBytes = 64 * 1024

var secretLikeKeyFragments = []string{"key", "token", "secret", "password"}

func WorkspaceIDFromPathParam(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "workspaceId")

		if strings.TrimSpace(id) == "" {
			writeError(w, http.StatusBadRequest, "workspace id is required in the path")
			return
		}
		r.Header.Set("X-Workspace-ID", id)

		r.Header.Del("X-Workspace-Slug")
		next.ServeHTTP(w, r)
	})
}

func rejectSecretLikeKeys(v any, path string) error {
	switch val := v.(type) {
	case map[string]any:

		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lower := strings.ToLower(k)
			for _, frag := range secretLikeKeyFragments {
				if strings.Contains(lower, frag) {
					at := path
					if at == "" {
						at = "policy"
					}
					return fmt.Errorf("policy must not carry secrets: field %q (in %s) looks like a credential; the policy document is stored unencrypted — put credentials into the workspace or per-user configuration layers, which are sealed", k, at)
				}
			}
			if err := rejectSecretLikeKeys(val[k], path+"/"+k); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range val {
			if err := rejectSecretLikeKeys(child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateDeploymentPolicyDoc(raw []byte) error {
	if len(raw) > maxDeploymentPolicyBytes {
		return fmt.Errorf("policy exceeds %d bytes", maxDeploymentPolicyBytes)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return errors.New("policy must be a JSON object")
	}
	if _, ok := generic.(map[string]any); !ok {
		return errors.New("policy must be a JSON object")
	}
	if err := rejectSecretLikeKeys(generic, ""); err != nil {
		return err
	}

	var doc struct {
		LLM *struct {
			BaseURL string `json:"base_url"`
			Model   string `json:"model"`
			Locked  bool   `json:"locked"`
		} `json:"llm"`
		MCP map[string]json.RawMessage `json:"mcp"`

		Session json.RawMessage `json:"session"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return errors.New("policy must be an object with only llm/mcp/session blocks (llm: base_url/model/locked, mcp: name → enabled/locked, session: idle_timeout_hours/absolute_lifetime_days/max_concurrent_sessions/require_mfa)")
	}
	if err := validateSessionPolicyBlock(doc.Session); err != nil {
		return err
	}
	if doc.LLM != nil {
		if doc.LLM.BaseURL != "" {
			if err := validateConfigBaseURL(doc.LLM.BaseURL); err != nil {
				return err
			}
		}
		if len(doc.LLM.Model) > maxConfigModelLength {
			return fmt.Errorf("llm.model exceeds %d characters", maxConfigModelLength)
		}
	}
	for name, entryRaw := range doc.MCP {
		if name == "" || len(name) > maxConfigMCPNameLength {
			return fmt.Errorf("mcp server name must be 1-%d characters", maxConfigMCPNameLength)
		}
		var entry struct {
			Enabled *bool `json:"enabled"`
			Locked  *bool `json:"locked"`
		}
		entryDec := json.NewDecoder(bytes.NewReader(entryRaw))
		entryDec.DisallowUnknownFields()
		if err := entryDec.Decode(&entry); err != nil {
			return fmt.Errorf("mcp entry %q must be an object with only enabled/locked fields — the policy layer carries no env: per-server env values belong to the sealed workspace or user configuration layers", name)
		}
		if name == mcpPolicyWildcard {

			if entry.Enabled == nil || *entry.Enabled || entry.Locked == nil || !*entry.Locked {
				return errors.New(`mcp entry "*" supports exactly one form: {"enabled": false, "locked": true} — the deployment-wide MCP kill switch; per-server values use named entries`)
			}
		}
	}
	return nil
}

func deploymentPolicyHash(doc []byte) string {
	if len(doc) == 0 {
		return ""
	}
	sum := sha256.Sum256(doc)
	return hex.EncodeToString(sum[:])
}

func (h *Handler) GetDeploymentPolicyAsAdmin(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDeploymentAdmin(w, r); !ok {
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

func (h *Handler) PutDeploymentPolicy(w http.ResponseWriter, r *http.Request) {
	actorUUID, ok := h.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		Policy json.RawMessage `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.Policy) == 0 || isJSONNull(body.Policy) {
		writeError(w, http.StatusBadRequest, "policy object is required (send {} to clear the policy)")
		return
	}
	if err := validateDeploymentPolicyDoc(body.Policy); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("deployment policy: begin tx failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update deployment policy")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockConfigLayerKey(r.Context(), deploymentPolicyLockKey); err != nil {
		slog.Error("deployment policy: lock failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update deployment policy")
		return
	}

	beforeHash := ""
	prev, err := qtx.GetDeploymentPolicy(r.Context())
	switch {
	case err == nil:
		beforeHash = deploymentPolicyHash(prev.Policy)
	case errors.Is(err, pgx.ErrNoRows):
	default:
		slog.Error("deployment policy: load current failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update deployment policy")
		return
	}

	row, err := qtx.SetDeploymentPolicy(r.Context(), db.SetDeploymentPolicyParams{
		Policy:    body.Policy,
		UpdatedBy: actorUUID,
	})
	if err != nil {
		slog.Error("deployment policy: set failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update deployment policy")
		return
	}
	if _, err := qtx.InsertAdminAudit(r.Context(), db.InsertAdminAuditParams{
		ActorUserID: actorUUID,
		Action:      adminAuditActionPolicySet,
		TargetType:  "deployment_policy",
		TargetID:    pgtype.Text{String: "singleton", Valid: true},
		BeforeHash:  pgtype.Text{String: beforeHash, Valid: beforeHash != ""},
		AfterHash:   pgtype.Text{String: deploymentPolicyHash(row.Policy), Valid: true},
		RequestID:   adminAuditRequestID(r),
	}); err != nil {
		slog.Error("deployment policy: audit insert failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update deployment policy")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("deployment policy: commit failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update deployment policy")
		return
	}

	sessionPolicies.invalidate()

	slog.Info("deployment policy updated",
		"actor_user_id", uuidToString(actorUUID),
		"policy_bytes", len(row.Policy),
	)
	writeJSON(w, http.StatusOK, DeploymentPolicyResponse{
		Policy:    json.RawMessage(row.Policy),
		UpdatedAt: optionalTime(row.UpdatedAt),
	})
}
