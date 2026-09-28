package deployment

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// accountRow — минимум, нужный этому файлу из accounts.
type accountRow struct {
	ID            string
	Email         string
	Name          string
	Deactivated   bool
	DeactivatedAt *time.Time
	TokenEpoch    int
}

func (d *Deps) getAccount(ctx context.Context, id string) (accountRow, error) {
	var a accountRow
	err := d.DB.Pool.QueryRow(ctx, `
		SELECT id, acct_email, acct_full_name, acct_deactivated_at IS NOT NULL, acct_deactivated_at, acct_token_epoch
		FROM accounts WHERE id = $1`, id).Scan(&a.ID, &a.Email, &a.Name, &a.Deactivated, &a.DeactivatedAt, &a.TokenEpoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return accountRow{}, ErrNotFound
	}
	if err != nil {
		return accountRow{}, fmt.Errorf("deployment: чтение аккаунта: %w", err)
	}
	return a, nil
}

// revokeLiveAccess — общая часть deactivate/delete: отзывает все PAT,
// инкрементирует token_version + удаляет хранимые сессии, переводит
// принадлежащие рантаймы offline, закрывает realtime-подключения (contract
// §7: «отзыв всех PAT/токенов раннера пользователя, принудительный offline
// его рантаймов, закрытие realtime-подключений»).
func (d *Deps) revokeLiveAccess(ctx context.Context, accountID string) (revokedTokens, closedConnections int, err error) {
	revokedPATs, err := d.Authn.Store.RevokeAllPATs(ctx, accountID)
	if err != nil {
		return 0, 0, err
	}
	if _, err := d.Authn.Store.BumpTokenEpoch(ctx, accountID); err != nil {
		return 0, 0, err
	}
	revokedSessions, err := d.Authn.Store.RevokeAllSessions(ctx, accountID)
	if err != nil {
		return 0, 0, err
	}
	if _, err := d.DB.Pool.Exec(ctx, `
		UPDATE executors SET ex_status = 'offline', updated_at = now()
		WHERE ex_owner_account_id = $1 AND ex_status <> 'offline'`, accountID); err != nil {
		return 0, 0, fmt.Errorf("deployment: перевод рантаймов offline: %w", err)
	}
	closed := d.Hub.CloseUserConnections(accountID)
	return int(revokedPATs + revokedSessions), closed, nil
}

func toDeploymentUser(a accountRow, revokedTokens, closedConnections int) DeploymentUser {
	return DeploymentUser{
		UserID: a.ID, Email: a.Email, Name: a.Name,
		Deactivated: a.Deactivated, DeactivatedAt: a.DeactivatedAt,
		TokenVersion: a.TokenEpoch, RevokedTokens: revokedTokens, ClosedConnections: closedConnections,
	}
}

func (d *Deps) handleDeactivateUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	userID := r.PathValue("userId")
	if userID == actor.UserID {
		httpapi.WriteError(w, http.StatusConflict, "cannot deactivate your own account", "self_target")
		return
	}
	acct, err := d.getAccount(r.Context(), userID)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "")
		return
	}
	if checkErr(w, err) {
		return
	}
	revoked, closed, err := d.revokeLiveAccess(r.Context(), userID)
	if checkErr(w, err) {
		return
	}
	if _, err := d.DB.Pool.Exec(r.Context(), `UPDATE accounts SET acct_deactivated_at = now() WHERE id = $1`, userID); err != nil {
		internalError(w)
		return
	}
	acct.Deactivated = true
	audit := httpAudit(r, actor, "user.deactivate")
	audit.TargetType, audit.TargetID = ptr("account"), ptr(userID)
	_ = WriteAudit(r.Context(), d.DB, audit)
	httpapi.WriteJSON(w, http.StatusOK, toDeploymentUser(acct, revoked, closed))
}

func (d *Deps) handleReactivateUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	userID := r.PathValue("userId")
	acct, err := d.getAccount(r.Context(), userID)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "")
		return
	}
	if checkErr(w, err) {
		return
	}
	if _, err := d.DB.Pool.Exec(r.Context(), `UPDATE accounts SET acct_deactivated_at = NULL WHERE id = $1`, userID); err != nil {
		internalError(w)
		return
	}
	acct.Deactivated, acct.DeactivatedAt = false, nil
	audit := httpAudit(r, actor, "user.reactivate")
	audit.TargetType, audit.TargetID = ptr("account"), ptr(userID)
	_ = WriteAudit(r.Context(), d.DB, audit)
	httpapi.WriteJSON(w, http.StatusOK, toDeploymentUser(acct, 0, 0))
}

func (d *Deps) handleRevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	userID := r.PathValue("userId")
	if _, err := d.getAccount(r.Context(), userID); errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "")
		return
	} else if err != nil {
		internalError(w)
		return
	}
	epoch, err := d.Authn.Store.BumpTokenEpoch(r.Context(), userID)
	if checkErr(w, err) {
		return
	}
	revoked, err := d.Authn.Store.RevokeAllSessions(r.Context(), userID)
	if checkErr(w, err) {
		return
	}
	audit := httpAudit(r, actor, "sessions_revoked")
	audit.TargetType, audit.TargetID = ptr("account"), ptr(userID)
	_ = WriteAudit(r.Context(), d.DB, audit)
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"revoked": revoked, "token_version": epoch})
}

func (d *Deps) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	userID := r.PathValue("userId")
	if userID == actor.UserID {
		httpapi.WriteError(w, http.StatusConflict, "cannot delete your own account", "self_target")
		return
	}
	if _, err := d.getAccount(r.Context(), userID); errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "")
		return
	} else if err != nil {
		internalError(w)
		return
	}
	isAdmin, err := IsAdmin(r.Context(), d.DB, userID)
	if checkErr(w, err) {
		return
	}
	if isAdmin {
		httpapi.WriteError(w, http.StatusConflict, "revoke the deployment-admin role first", "still_deployment_admin")
		return
	}
	revoked, closed, err := d.revokeLiveAccess(r.Context(), userID)
	if checkErr(w, err) {
		return
	}
	anonymizedName := "Deleted user"
	anonymizedEmail := fmt.Sprintf("deleted-%s@deleted.invalid", userID)
	var removedMemberships int64
	err = d.DB.WithTx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `DELETE FROM space_members WHERE account_id = $1`, userID)
		if err != nil {
			return err
		}
		removedMemberships = tag.RowsAffected()
		for _, stmt := range []string{
			`DELETE FROM auth_bindings WHERE account_id = $1`,
			`DELETE FROM mfa_recovery_codes WHERE account_id = $1`,
			`DELETE FROM mfa_factors WHERE account_id = $1`,
			`DELETE FROM space_mcp_credentials WHERE account_id = $1`,
			`DELETE FROM notification_prefs WHERE account_id = $1`,
			`DELETE FROM space_config_overrides WHERE account_id = $1`,
		} {
			if _, err := tx.Exec(r.Context(), stmt, userID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(r.Context(), `
			UPDATE accounts SET acct_full_name = $2, acct_email = $3, acct_avatar_uri = NULL, acct_bio = '',
				acct_deactivated_at = now(), acct_anonymized_at = now()
			WHERE id = $1`, userID, anonymizedName, anonymizedEmail)
		return err
	})
	if checkErr(w, err) {
		return
	}
	audit := httpAudit(r, actor, "user.delete")
	audit.TargetType, audit.TargetID = ptr("account"), ptr(userID)
	_ = WriteAudit(r.Context(), d.DB, audit)
	httpapi.WriteJSON(w, http.StatusOK, DeleteUserResponse{
		UserID: userID, Email: anonymizedEmail, Name: anonymizedName,
		RemovedMemberships: int(removedMemberships), RevokedTokens: revoked, ClosedConnections: closed,
	})
}
