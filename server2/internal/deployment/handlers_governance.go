// handlers_governance.go — двухканальное подтверждение роли deployment-admin
// (`/api/deployment/admins*`) и документ политики деплоя
// (`/api/deployment/policy`, `/api/deployment-policy`): обе группы — часть
// одного и того же «Приложение. CLI администратора деплоя» на стороне HTTP
// (contract §7), собраны в одном файле, а не раскиданы по двум почти
// одинаковым по объёму файлам.
package deployment

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/seal"
	"github.com/adanman/goosar/server2/internal/store"
)

// --- роли deployment-admin: список, заявки, двухканальное подтверждение ------

func (d *Deps) handleListAdmins(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.requireDeploymentAdmin(w, r); !ok {
		return
	}
	admins, err := ListAdmins(r.Context(), d.DB)
	if checkErr(w, err) {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, admins)
}

func (d *Deps) handleListPendingRequests(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.requireDeploymentAdmin(w, r); !ok {
		return
	}
	pending, err := ListPendingRequests(r.Context(), d.DB)
	if checkErr(w, err) {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, pending)
}

func (d *Deps) findAccountByEmail(r *http.Request, email string) (id string, found bool, err error) {
	err = d.DB.Pool.QueryRow(r.Context(), `SELECT id FROM accounts WHERE lower(acct_email) = lower($1)`, email).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, err
	default:
		return id, true, nil
	}
}

func (d *Deps) handleRequestGrant(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := httpapi.DecodeJSON(r, &body); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	email := strings.TrimSpace(body.Email)
	if email == "" {
		httpapi.BadRequest(w, "email is required")
		return
	}
	accountID, found, err := d.findAccountByEmail(r, email)
	if checkErr(w, err) {
		return
	}
	if !found {
		httpapi.NotFound(w, "no user with this email")
		return
	}
	if admin, alreadyAdmin, ok := d.respondIfAlreadyAdmin(w, r, accountID); !ok {
		return
	} else if alreadyAdmin {
		httpapi.WriteJSON(w, http.StatusOK, admin)
		return
	}
	existing, has, err := FindPendingRequestForTarget(r.Context(), d.DB, "grant", accountID, "")
	if checkErr(w, err) {
		return
	}
	if has {
		httpapi.WriteJSON(w, http.StatusAccepted, existing)
		return
	}
	audit := httpAudit(r, actor, "deployment_admin.grant.requested")
	pending, err := CreatePendingRequest(r.Context(), d.DB, "grant", &accountID, nil, actor.UserID, audit)
	if checkErr(w, err) {
		return
	}
	httpapi.WriteJSON(w, http.StatusAccepted, pending)
}

// respondIfAlreadyAdmin — ok=false означает, что обработчик уже ответил (500)
// и вызывающему остаётся только вернуться; иначе alreadyAdmin сообщает,
// нашёлся ли accountID среди текущих deployment-admin.
func (d *Deps) respondIfAlreadyAdmin(w http.ResponseWriter, r *http.Request, accountID string) (admin DeploymentAdmin, alreadyAdmin, ok bool) {
	isAdmin, err := IsAdmin(r.Context(), d.DB, accountID)
	if checkErr(w, err) {
		return DeploymentAdmin{}, false, false
	}
	if !isAdmin {
		return DeploymentAdmin{}, false, true
	}
	admin, err = getAdminRow(r.Context(), d.DB, accountID)
	if checkErr(w, err) {
		return DeploymentAdmin{}, false, false
	}
	return admin, true, true
}

func (d *Deps) handleRequestRevoke(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	userID := r.PathValue("userId")
	isAdmin, err := IsAdmin(r.Context(), d.DB, userID)
	if checkErr(w, err) {
		return
	}
	if !isAdmin {
		httpapi.NotFound(w, "user is not a deployment-admin")
		return
	}
	count, err := CountAdmins(r.Context(), d.DB)
	if checkErr(w, err) {
		return
	}
	if count <= 1 {
		httpapi.WriteError(w, http.StatusConflict, "cannot revoke the last deployment-admin", "last_admin")
		return
	}
	existing, has, err := FindPendingRequestForTarget(r.Context(), d.DB, "revoke", userID, "")
	if checkErr(w, err) {
		return
	}
	if has {
		httpapi.WriteJSON(w, http.StatusAccepted, existing)
		return
	}
	audit := httpAudit(r, actor, "deployment_admin.revoke.requested")
	pending, err := CreatePendingRequest(r.Context(), d.DB, "revoke", &userID, nil, actor.UserID, audit)
	if checkErr(w, err) {
		return
	}
	httpapi.WriteJSON(w, http.StatusAccepted, pending)
}

// --- документ политики деплоя (platform_policy) -------------------------------

// readPolicy — текущий документ политики деплоя (platform_policy, ровно одна
// строка id=1; contract: «пустая политика ({}), если ничего не сохранено» —
// 011_governance.up.sql не заводит начальную строку, поэтому "нет строки"
// трактуется как пустой документ, а не ошибка).
func (d *Deps) readPolicy(r *http.Request) (PolicyDocument, error) {
	var raw []byte
	var updatedAt *time.Time
	err := d.DB.Pool.QueryRow(r.Context(), `SELECT pp_body, pp_updated_at FROM platform_policy WHERE id = 1`).
		Scan(&raw, &updatedAt)
	if err != nil {
		if store.IsNoRows(err) {
			return PolicyDocument{Policy: PolicyBody{}}, nil
		}
		return PolicyDocument{}, err
	}
	var doc PolicyDocument
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &doc.Policy)
	}
	doc.UpdatedAt = updatedAt
	return doc, nil
}

func (d *Deps) handleGetPolicyAdmin(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.requireDeploymentAdmin(w, r); !ok {
		return
	}
	d.respondWithPolicy(w, r)
}

// handleGetPolicyMember — GET /api/deployment-policy (§13): owner/admin
// пространства ИЛИ deployment-admin, тот же документ.
func (d *Deps) handleGetPolicyMember(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := d.requireConfigAccess(w, r); !ok {
		return
	}
	d.respondWithPolicy(w, r)
}

func (d *Deps) respondWithPolicy(w http.ResponseWriter, r *http.Request) {
	doc, err := d.readPolicy(r)
	if checkErr(w, err) {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, doc)
}

func (d *Deps) handlePutPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	raw, err := httpapi.ReadBody(r)
	if err != nil {
		httpapi.BadRequest(w, "invalid body")
		return
	}
	var envelope struct {
		Policy json.RawMessage `json:"policy"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope.Policy) == 0 {
		httpapi.BadRequest(w, "policy is required")
		return
	}
	if msg, ok := validatePolicyBody(envelope.Policy); !ok {
		httpapi.BadRequest(w, msg)
		return
	}

	before, _ := d.readPolicy(r)
	var body PolicyBody
	_ = json.Unmarshal(envelope.Policy, &body)
	bodyJSON, _ := json.Marshal(body)

	_, err = d.DB.Pool.Exec(r.Context(), `
		INSERT INTO platform_policy (id, pp_body, pp_updated_at) VALUES (1, $1, now())
		ON CONFLICT (id) DO UPDATE SET pp_body = EXCLUDED.pp_body, pp_updated_at = now()`, bodyJSON)
	if checkErr(w, err) {
		return
	}

	audit := httpAudit(r, actor, "deployment_policy.set")
	audit.BeforeHash, audit.AfterHash = ptr(seal.HashJSON(before.Policy)), ptr(seal.HashJSON(body))
	_ = WriteAudit(r.Context(), d.DB, audit)

	d.respondWithPolicy(w, r)
}
