// Второй канал подтверждения изменений состава deployment_admin: API лишь
// заявляет запрос на выдачу/отзыв роли и отвечает 202, а применяет его
// оператор отдельной серверной командой (list-pending/confirm/reject).
// Подтверждение заново проверяет все инварианты (роль всё ещё нужна, нельзя
// отозвать последнего администратора), и каждый шаг пишется в admin_audit.
package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	deploymentAdminPendingActionGrant  = "grant"
	deploymentAdminPendingActionRevoke = "revoke"
)

const (
	adminAuditActionGrantRequested  = "deployment_admin.grant.requested"
	adminAuditActionGrantConfirmed  = "deployment_admin.grant.confirmed"
	adminAuditActionGrantRejected   = "deployment_admin.grant.rejected"
	adminAuditActionRevokeRequested = "deployment_admin.revoke.requested"
	adminAuditActionRevokeConfirmed = "deployment_admin.revoke.confirmed"
	adminAuditActionRevokeRejected  = "deployment_admin.revoke.rejected"

	adminAuditActionGrantBreakGlass = "deployment_admin.grant.break_glass"
)

type DeploymentAdminPendingResponse struct {
	Status       string    `json:"status"`
	RequestID    string    `json:"request_id"`
	Action       string    `json:"action"`
	TargetUserID string    `json:"target_user_id"`
	TargetEmail  string    `json:"target_email,omitempty"`
	RequestedBy  string    `json:"requested_by"`
	RequestedAt  time.Time `json:"requested_at"`
	ConfirmHint  string    `json:"confirm_hint"`
}

func deploymentAdminConfirmHint(pendingID string) string {
	return fmt.Sprintf("run on the server: `docker compose exec backend ./goosar_admin confirm %s` (Kubernetes: `kubectl exec deploy/<release>-backend -- ./goosar_admin confirm %s`); reject with `./goosar_admin reject %s`, list with `./goosar_admin list-pending`", pendingID, pendingID, pendingID)
}

func deploymentAdminPendingResponse(row db.DeploymentAdminPending, targetEmail string) DeploymentAdminPendingResponse {
	id := uuidToString(row.ID)
	return DeploymentAdminPendingResponse{
		Status:       "pending",
		RequestID:    id,
		Action:       row.Action,
		TargetUserID: uuidToString(row.TargetUserID),
		TargetEmail:  targetEmail,
		RequestedBy:  uuidToString(row.RequestedBy),
		RequestedAt:  row.RequestedAt.Time,
		ConfirmHint:  deploymentAdminConfirmHint(id),
	}
}

func pendingAuditRequestAction(action string) string {
	if action == deploymentAdminPendingActionRevoke {
		return adminAuditActionRevokeRequested
	}
	return adminAuditActionGrantRequested
}

type DeploymentAdminPendingDecision struct {
	Action       string
	TargetUserID string

	AlreadyHeld bool
}

var ErrDeploymentAdminPendingNotFound = errors.New("no pending deployment-admin request with this id")

var ErrLastDeploymentAdmin = errors.New("cannot remove the last deployment administrator")

func ConfirmDeploymentAdminPending(ctx context.Context, txs txStarter, queries *db.Queries, pendingID pgtype.UUID) (*DeploymentAdminPendingDecision, error) {
	tx, err := txs.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("confirm pending: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := queries.WithTx(tx)
	if err := qtx.LockConfigLayerKey(ctx, deploymentAdminsLockKey); err != nil {
		return nil, fmt.Errorf("confirm pending: lock: %w", err)
	}
	row, err := qtx.GetDeploymentAdminPending(ctx, pendingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDeploymentAdminPendingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("confirm pending: load: %w", err)
	}

	decision := &DeploymentAdminPendingDecision{
		Action:       row.Action,
		TargetUserID: uuidToString(row.TargetUserID),
	}
	var auditAction string
	switch row.Action {
	case deploymentAdminPendingActionGrant:
		inserted, err := qtx.InsertDeploymentAdmin(ctx, db.InsertDeploymentAdminParams{
			UserID:    row.TargetUserID,
			GrantedBy: row.RequestedBy,
		})
		if err != nil {
			return nil, fmt.Errorf("confirm pending: grant: %w", err)
		}
		decision.AlreadyHeld = inserted == 0
		auditAction = adminAuditActionGrantConfirmed
	case deploymentAdminPendingActionRevoke:
		if _, err := qtx.GetDeploymentAdmin(ctx, row.TargetUserID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("target %s no longer holds the deployment administrator role — reject the request instead", uuidToString(row.TargetUserID))
			}
			return nil, fmt.Errorf("confirm pending: target lookup: %w", err)
		}
		count, err := qtx.CountDeploymentAdmins(ctx)
		if err != nil {
			return nil, fmt.Errorf("confirm pending: count: %w", err)
		}

		if count <= 1 {
			return nil, ErrLastDeploymentAdmin
		}
		if _, err := qtx.DeleteDeploymentAdmin(ctx, row.TargetUserID); err != nil {
			return nil, fmt.Errorf("confirm pending: revoke: %w", err)
		}
		auditAction = adminAuditActionRevokeConfirmed
	default:
		return nil, fmt.Errorf("confirm pending: unknown action %q", row.Action)
	}

	if _, err := qtx.DeleteDeploymentAdminPending(ctx, row.ID); err != nil {
		return nil, fmt.Errorf("confirm pending: clear request: %w", err)
	}
	if _, err := qtx.InsertAdminAudit(ctx, db.InsertAdminAuditParams{
		ActorUserID: pgtype.UUID{},
		Action:      auditAction,
		TargetType:  "user",
		TargetID:    pgtype.Text{String: uuidToString(row.TargetUserID), Valid: true},
		RequestID:   pgtype.Text{String: uuidToString(row.ID), Valid: true},
	}); err != nil {
		return nil, fmt.Errorf("confirm pending: audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("confirm pending: commit: %w", err)
	}
	return decision, nil
}

func RejectDeploymentAdminPending(ctx context.Context, txs txStarter, queries *db.Queries, pendingID pgtype.UUID) (*DeploymentAdminPendingDecision, error) {
	tx, err := txs.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("reject pending: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := queries.WithTx(tx)
	if err := qtx.LockConfigLayerKey(ctx, deploymentAdminsLockKey); err != nil {
		return nil, fmt.Errorf("reject pending: lock: %w", err)
	}
	row, err := qtx.GetDeploymentAdminPending(ctx, pendingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDeploymentAdminPendingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reject pending: load: %w", err)
	}
	if _, err := qtx.DeleteDeploymentAdminPending(ctx, row.ID); err != nil {
		return nil, fmt.Errorf("reject pending: clear request: %w", err)
	}
	auditAction := adminAuditActionGrantRejected
	if row.Action == deploymentAdminPendingActionRevoke {
		auditAction = adminAuditActionRevokeRejected
	}
	if _, err := qtx.InsertAdminAudit(ctx, db.InsertAdminAuditParams{
		ActorUserID: pgtype.UUID{},
		Action:      auditAction,
		TargetType:  "user",
		TargetID:    pgtype.Text{String: uuidToString(row.TargetUserID), Valid: true},
		RequestID:   pgtype.Text{String: uuidToString(row.ID), Valid: true},
	}); err != nil {
		return nil, fmt.Errorf("reject pending: audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("reject pending: commit: %w", err)
	}
	return &DeploymentAdminPendingDecision{
		Action:       row.Action,
		TargetUserID: uuidToString(row.TargetUserID),
	}, nil
}

func (h *Handler) fileDeploymentAdminPending(ctx context.Context, actor pgtype.UUID, action string, target pgtype.UUID, requestID pgtype.Text) (db.DeploymentAdminPending, bool, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.DeploymentAdminPending{}, false, fmt.Errorf("file pending: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockConfigLayerKey(ctx, deploymentAdminsLockKey); err != nil {
		return db.DeploymentAdminPending{}, false, fmt.Errorf("file pending: lock: %w", err)
	}
	existing, err := qtx.GetDeploymentAdminPendingByActionTarget(ctx, db.GetDeploymentAdminPendingByActionTargetParams{
		Action:       action,
		TargetUserID: target,
	})
	switch {
	case err == nil:

		return existing, false, nil
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return db.DeploymentAdminPending{}, false, fmt.Errorf("file pending: dedupe lookup: %w", err)
	}
	row, err := qtx.InsertDeploymentAdminPending(ctx, db.InsertDeploymentAdminPendingParams{
		Action:       action,
		TargetUserID: target,
		RequestedBy:  actor,
	})
	if err != nil {
		return db.DeploymentAdminPending{}, false, fmt.Errorf("file pending: insert: %w", err)
	}
	if _, err := qtx.InsertAdminAudit(ctx, db.InsertAdminAuditParams{
		ActorUserID: actor,
		Action:      pendingAuditRequestAction(action),
		TargetType:  "user",
		TargetID:    pgtype.Text{String: uuidToString(target), Valid: true},
		RequestID:   requestID,
	}); err != nil {
		return db.DeploymentAdminPending{}, false, fmt.Errorf("file pending: audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.DeploymentAdminPending{}, false, fmt.Errorf("file pending: commit: %w", err)
	}
	return row, true, nil
}

var ErrDeploymentAdminUserNotFound = errors.New("no user with this email — the role can only be granted to an existing user")

type DeploymentAdminGrantResult struct {
	UserID string
	Email  string

	Created bool
}

func GrantDeploymentAdminByEmail(ctx context.Context, txs txStarter, queries *db.Queries, rawEmail string) (*DeploymentAdminGrantResult, error) {

	email := strings.ToLower(strings.TrimSpace(rawEmail))
	if email == "" {
		return nil, errors.New("an email is required")
	}
	tx, err := txs.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("grant by email: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := queries.WithTx(tx)
	if err := qtx.LockConfigLayerKey(ctx, deploymentAdminsLockKey); err != nil {
		return nil, fmt.Errorf("grant by email: lock: %w", err)
	}
	user, err := qtx.GetUserByEmail(ctx, email)
	if isNotFound(err) {
		return nil, ErrDeploymentAdminUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("grant by email: look up %s: %w", email, err)
	}
	inserted, err := qtx.InsertDeploymentAdmin(ctx, db.InsertDeploymentAdminParams{
		UserID: user.ID,

		GrantedBy: pgtype.UUID{},
	})
	if err != nil {
		return nil, fmt.Errorf("grant by email: insert: %w", err)
	}
	result := &DeploymentAdminGrantResult{
		UserID:  uuidToString(user.ID),
		Email:   user.Email,
		Created: inserted > 0,
	}
	if !result.Created {

		return result, nil
	}
	if _, err := qtx.InsertAdminAudit(ctx, db.InsertAdminAuditParams{
		ActorUserID: pgtype.UUID{},
		Action:      adminAuditActionGrantBreakGlass,
		TargetType:  "user",
		TargetID:    pgtype.Text{String: result.UserID, Valid: true},
	}); err != nil {
		return nil, fmt.Errorf("grant by email: audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("grant by email: commit: %w", err)
	}
	return result, nil
}
