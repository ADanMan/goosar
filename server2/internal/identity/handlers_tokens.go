package identity

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/authn"
	"github.com/adanman/goosar/server2/internal/httpapi"
)

// handleIssueCliToken выпускает свежий сессионный JWT (POST /api/cli-token),
// создавая новую строку сессии — как и обычный вход, но без похода за кодом.
func (d *Deps) handleIssueCliToken(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	acct, err := d.Authn.Store.FindAccountByID(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	sess, err := d.Authn.Store.CreateSession(r.Context(), acct.ID, "cli-token")
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	token, err := d.Authn.Signer.Sign(authn.Claims{
		Sub: acct.ID, Email: acct.Email, Name: acct.Name, TV: acct.TokenEpoch, SID: sess.ID,
	}, 30*24*time.Hour)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"token": token})
}

type createPATRequest struct {
	Name          string `json:"name"`
	ExpiresInDays *int   `json:"expires_in_days"`
}

func (d *Deps) handleListPATs(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	pats, err := d.Authn.Store.ListPATs(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	out := make([]map[string]any, 0, len(pats))
	for _, p := range pats {
		out = append(out, patJSON(p))
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleCreatePAT(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	var req createPATRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		httpapi.BadRequest(w, "name is required")
		return
	}
	days := 0
	if req.ExpiresInDays != nil && *req.ExpiresInDays > 0 {
		days = *req.ExpiresInDays
	}
	pat, secret, err := d.Authn.Store.CreatePAT(r.Context(), actor.UserID, req.Name, days)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	body := patJSON(pat)
	body["token"] = secret
	httpapi.WriteJSON(w, http.StatusCreated, body)
}

func (d *Deps) handleRevokePAT(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := d.Authn.Store.RevokePAT(r.Context(), id, actor.UserID); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) handleRenewCurrentPAT(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	if actor.Source != httpapi.SourcePAT {
		httpapi.WriteError(w, http.StatusBadRequest, "this endpoint requires a PAT-authenticated request", "not_a_pat_request")
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	pat, err := d.Authn.Store.FindPATByToken(r.Context(), token)
	if errors.Is(err, authn.ErrNotFound) {
		httpapi.Unauthorized(w, "token is no longer valid")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if pat.AccountID != actor.UserID {
		httpapi.Unauthorized(w, "token belongs to another user")
		return
	}
	expiresAt, renewed, err := d.Authn.Store.RenewPAT(r.Context(), pat.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	resp := map[string]any{"renewed": renewed, "expires_at": ""}
	if expiresAt != nil {
		resp["expires_at"] = expiresAt.Format("2006-01-02T15:04:05Z07:00")
	}
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

func patJSON(p authn.PAT) map[string]any {
	return map[string]any{
		"id":            p.ID,
		"name":          p.Name,
		"token_prefix":  p.Prefix,
		"expires_at":    p.ExpiresAt,
		"last_used_at":  p.LastUsedAt,
		"created_at":    p.CreatedAt,
	}
}

// workspaceTemplates — статичный список шаблонов ролей пространства.
// docs/51-data-model.md/50-api-contract.* не описывают конкретный набор
// шаблонов сервера — задокументировано как пробел в decisions.md: возвращаем
// пустой список (валиден по схеме WorkspaceTemplateSummary[], template_key
// в CreateWorkspaceRequest остаётся необязательным и ни на что не влияет,
// пока набор шаблонов не появится).
func (d *Deps) handleListWorkspaceTemplates(w http.ResponseWriter, r *http.Request) {
	if _, ok := httpapi.RequireActor(w, r); !ok {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, []any{})
}
