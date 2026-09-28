package deployment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/store"
)

// ErrNotFound — строка не найдена (заявка, MCP-сервер деплоя, override, ...).
var ErrNotFound = errors.New("deployment: не найдено")

// ErrConflict — операция нарушает инвариант (последний admin, дубликат имени, ...).
var ErrConflict = errors.New("deployment: конфликт")

// IsAdmin — держит ли accountID роль deployment-admin.
func IsAdmin(ctx context.Context, db *store.Store, accountID string) (bool, error) {
	return db.RowExists(ctx, `SELECT EXISTS(SELECT 1 FROM platform_admins WHERE account_id = $1)`, accountID)
}

// CountAdmins — сколько держателей роли сейчас (правило "нельзя остаться без
// единого администратора").
func CountAdmins(ctx context.Context, db *store.Store) (int, error) {
	var n int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM platform_admins`).Scan(&n); err != nil {
		return 0, fmt.Errorf("deployment: подсчёт admin: %w", err)
	}
	return n, nil
}

// ListAdmins — components/schemas/DeploymentAdmin[], самые новые первыми.
func ListAdmins(ctx context.Context, db *store.Store) ([]DeploymentAdmin, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT a.id, a.acct_email, a.acct_full_name, pa.pa_granted_by, pa.pa_granted_at
		FROM platform_admins pa JOIN accounts a ON a.id = pa.account_id
		ORDER BY pa.pa_granted_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("deployment: список admin: %w", err)
	}
	defer rows.Close()
	out := []DeploymentAdmin{}
	for rows.Next() {
		var a DeploymentAdmin
		if err := rows.Scan(&a.UserID, &a.Email, &a.Name, &a.GrantedBy, &a.GrantedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// grantAdminTx выдаёт роль внутри tx (используется и прямой CLI-выдачей, и
// применением заявки при confirm) — идемпотентно по PK account_id.
func grantAdminTx(ctx context.Context, tx pgx.Tx, accountID, grantedBy string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO platform_admins (account_id, pa_granted_by)
		VALUES ($1, $2)
		ON CONFLICT (account_id) DO NOTHING`, accountID, grantedBy)
	if err != nil {
		return fmt.Errorf("deployment: выдача роли: %w", err)
	}
	return nil
}

func revokeAdminTx(ctx context.Context, tx pgx.Tx, accountID string) (int64, error) {
	tag, err := tx.Exec(ctx, `DELETE FROM platform_admins WHERE account_id = $1`, accountID)
	if err != nil {
		return 0, fmt.Errorf("deployment: отзыв роли: %w", err)
	}
	return tag.RowsAffected(), nil
}

// GrantAdminDirect — аварийная выдача роли (CLI `grant <email>`), минуя
// заявку. Целевой пользователь должен уже существовать (contract: «роль
// выдаётся только существующему»). grantedBy — accountID, который
// записывается в pa_granted_by; у CLI нет аутентифицированного оператора,
// поэтому пустой grantedBy трактуется как «сам целевой аккаунт» (аварийная
// самовыдача первому администратору деплоя) — решение зафиксировано в
// server2/docs/decisions.md, раздел T-029.
func GrantAdminDirect(ctx context.Context, db *store.Store, email, grantedBy string) (DeploymentAdmin, bool, error) {
	var accountID string
	err := db.Pool.QueryRow(ctx, `SELECT id FROM accounts WHERE lower(acct_email) = lower($1)`, email).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeploymentAdmin{}, false, ErrNotFound
	}
	if err != nil {
		return DeploymentAdmin{}, false, fmt.Errorf("deployment: поиск аккаунта по email: %w", err)
	}
	if grantedBy == "" {
		grantedBy = accountID
	}
	alreadyAdmin, err := IsAdmin(ctx, db, accountID)
	if err != nil {
		return DeploymentAdmin{}, false, err
	}
	if !alreadyAdmin {
		if err := db.WithTx(ctx, func(tx pgx.Tx) error {
			if err := grantAdminTx(ctx, tx, accountID, grantedBy); err != nil {
				return err
			}
			granted := "granted"
			return writeAuditTx(ctx, tx, AuditWrite{
				Source: "admin", Action: "deployment_admin.grant.direct",
				ActorAccountID: ptr(grantedBy), TargetType: ptr("account"), TargetID: ptr(accountID),
				Outcome: &granted, Reason: ptr("cmd/admin grant: break-glass, bypassing the pending-request queue"),
			})
		}); err != nil {
			return DeploymentAdmin{}, false, err
		}
	}
	admin, err := getAdminRow(ctx, db, accountID)
	if err != nil {
		return DeploymentAdmin{}, false, err
	}
	return admin, alreadyAdmin, nil
}

func getAdminRow(ctx context.Context, db *store.Store, accountID string) (DeploymentAdmin, error) {
	var a DeploymentAdmin
	err := db.Pool.QueryRow(ctx, `
		SELECT a.id, a.acct_email, a.acct_full_name, pa.pa_granted_by, pa.pa_granted_at
		FROM platform_admins pa JOIN accounts a ON a.id = pa.account_id
		WHERE pa.account_id = $1`, accountID,
	).Scan(&a.UserID, &a.Email, &a.Name, &a.GrantedBy, &a.GrantedAt)
	if err != nil {
		return DeploymentAdmin{}, fmt.Errorf("deployment: чтение admin: %w", err)
	}
	return a, nil
}

// --- pending requests (двухканальное подтверждение) --------------------------

type pendingRow struct {
	ID           string
	Action       string
	TargetAcctID *string
	TargetEmail  *string
	RequestedBy  string
	Status       string
	RequestedAt  time.Time
	ResolvedAt   *time.Time
}

func scanPending(row pgx.Row) (pendingRow, error) {
	var p pendingRow
	err := row.Scan(&p.ID, &p.Action, &p.TargetAcctID, &p.TargetEmail, &p.RequestedBy,
		&p.Status, &p.RequestedAt, &p.ResolvedAt)
	return p, err
}

const pendingColumns = `id, par_action, par_target_account_id, par_target_email, par_requested_by,
	par_status, par_requested_at, par_resolved_at`

func toPendingRequest(p pendingRow) PendingRequest {
	confirmHint := fmt.Sprintf(
		"Подтвердить или отклонить заявку %s: `goosar_admin confirm %s` / `goosar_admin reject %s` (серверная команда деплоя, вне HTTP API).",
		p.ID, p.ID, p.ID)
	return PendingRequest{
		Status:       p.Status,
		RequestID:    p.ID,
		Action:       p.Action,
		TargetUserID: p.TargetAcctID,
		TargetEmail:  p.TargetEmail,
		RequestedBy:  p.RequestedBy,
		RequestedAt:  p.RequestedAt,
		ConfirmHint:  confirmHint,
	}
}

// FindPendingRequestForTarget ищет уже существующую pending-заявку того же
// действия на ту же цель (contract: «повторная заявка... не дублируется —
// возвращается уже существующая»). targetAccountID имеет приоритет, если
// известен (аккаунт уже существует); иначе матчится по email.
func FindPendingRequestForTarget(ctx context.Context, db *store.Store, action, targetAccountID, targetEmail string) (PendingRequest, bool, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT `+pendingColumns+` FROM platform_admin_requests
		WHERE par_status = 'pending' AND par_action = $1
		  AND (($2 <> '' AND par_target_account_id = $2::uuid) OR ($2 = '' AND lower(par_target_email) = lower($3)))
		ORDER BY par_requested_at DESC LIMIT 1`, action, targetAccountID, targetEmail)
	p, err := scanPending(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return PendingRequest{}, false, nil
	}
	if err != nil {
		return PendingRequest{}, false, fmt.Errorf("deployment: поиск заявки: %w", err)
	}
	return toPendingRequest(p), true, nil
}

// CreatePendingRequest создаёт заявку grant/revoke и пишет admin-аудит
// requested в одной транзакции.
func CreatePendingRequest(ctx context.Context, db *store.Store, action string, targetAccountID, targetEmail *string, requestedBy string, audit AuditWrite) (PendingRequest, error) {
	var out PendingRequest
	err := db.WithTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO platform_admin_requests (par_action, par_target_account_id, par_target_email, par_requested_by)
			VALUES ($1, $2, $3, $4)
			RETURNING `+pendingColumns, action, targetAccountID, targetEmail, requestedBy)
		p, err := scanPending(row)
		if err != nil {
			return fmt.Errorf("deployment: создание заявки: %w", err)
		}
		out = toPendingRequest(p)
		audit.TargetType = ptr("deployment_admin_request")
		audit.TargetID = ptr(out.RequestID)
		return writeAuditTx(ctx, tx, audit)
	})
	return out, err
}

func writeAuditTx(ctx context.Context, tx pgx.Tx, e AuditWrite) error {
	_, err := tx.Exec(ctx, insertAuditSQL,
		e.Source, e.Action, e.ActorAccountID, e.ActorType, e.ActorID, e.ActorRole,
		e.TargetType, e.TargetID, e.Outcome, e.Reason, e.BeforeHash, e.AfterHash,
		e.WorkspaceID, e.RequestID, e.ClientIPDigest, e.ClientAgent)
	if err != nil {
		return fmt.Errorf("deployment: запись аудита: %w", err)
	}
	return nil
}

// ListPendingRequests — заявки, ждущие подтверждения оператором.
func ListPendingRequests(ctx context.Context, db *store.Store) ([]PendingRequest, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT `+pendingColumns+` FROM platform_admin_requests
		WHERE par_status = 'pending' ORDER BY par_requested_at`)
	if err != nil {
		return nil, fmt.Errorf("deployment: список заявок: %w", err)
	}
	defer rows.Close()
	out := []PendingRequest{}
	for rows.Next() {
		p, err := scanPending(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, toPendingRequest(p))
	}
	return out, rows.Err()
}

func getPendingByID(ctx context.Context, db *store.Store, id string) (pendingRow, error) {
	row := db.Pool.QueryRow(ctx, `SELECT `+pendingColumns+` FROM platform_admin_requests WHERE id = $1`, id)
	p, err := scanPending(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return pendingRow{}, ErrNotFound
	}
	if err != nil {
		return pendingRow{}, fmt.Errorf("deployment: чтение заявки: %w", err)
	}
	return p, nil
}

// ConfirmPendingRequest применяет одну ожидающую заявку (второй канал
// подтверждения, contract §7 "Двухканальное подтверждение"): повторно
// проверяет все инварианты (роль ещё нужна, нельзя остаться без единого
// администратора) и пишет ещё одну запись аудита. operatorAccountID —
// pa_granted_by для grant-заявок; у CLI нет отдельного оператора-аккаунта,
// поэтому вызывающий передаёт requested_by исходной заявки (тот, кто её
// подал) — см. server2/docs/decisions.md, T-029.
func ConfirmPendingRequest(ctx context.Context, db *store.Store, id string) (result string, err error) {
	p, err := getPendingByID(ctx, db, id)
	if err != nil {
		return "", err
	}
	if p.Status != "pending" {
		return "", fmt.Errorf("%w: заявка уже %s", ErrConflict, p.Status)
	}
	err = db.WithTx(ctx, func(tx pgx.Tx) error {
		var accountID string
		if p.TargetAcctID != nil {
			accountID = *p.TargetAcctID
		} else if p.TargetEmail != nil {
			if scanErr := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE lower(acct_email) = lower($1)`, *p.TargetEmail).Scan(&accountID); scanErr != nil {
				if errors.Is(scanErr, pgx.ErrNoRows) {
					return fmt.Errorf("%w: пользователь с этим email больше не существует", ErrConflict)
				}
				return scanErr
			}
		}
		switch p.Action {
		case "grant":
			isAdmin, checkErr := isAdminTx(ctx, tx, accountID)
			if checkErr != nil {
				return checkErr
			}
			if !isAdmin {
				if grantErr := grantAdminTx(ctx, tx, accountID, p.RequestedBy); grantErr != nil {
					return grantErr
				}
				result = "granted"
			} else {
				result = "already_admin"
			}
		case "revoke":
			count, countErr := countAdminsTx(ctx, tx)
			if countErr != nil {
				return countErr
			}
			isAdmin, checkErr := isAdminTx(ctx, tx, accountID)
			if checkErr != nil {
				return checkErr
			}
			if isAdmin && count <= 1 {
				return fmt.Errorf("%w: нельзя отозвать последнего deployment-admin", ErrConflict)
			}
			if isAdmin {
				if _, revokeErr := revokeAdminTx(ctx, tx, accountID); revokeErr != nil {
					return revokeErr
				}
				result = "revoked"
			} else {
				result = "already_not_admin"
			}
		default:
			return fmt.Errorf("deployment: неизвестное действие заявки %q", p.Action)
		}
		if _, execErr := tx.Exec(ctx, `
			UPDATE platform_admin_requests SET par_status = 'confirmed', par_resolved_at = now() WHERE id = $1`, id); execErr != nil {
			return execErr
		}
		action := "deployment_admin." + p.Action + ".confirmed"
		return writeAuditTx(ctx, tx, AuditWrite{
			Source: "admin", Action: action, ActorAccountID: &p.RequestedBy,
			TargetType: ptr("account"), TargetID: ptr(accountID), Outcome: ptr(result),
			RequestID: ptr(id),
		})
	})
	if err != nil {
		return "", err
	}
	return result, nil
}

// RejectPendingRequest отклоняет заявку, ничего не меняя в ролях.
func RejectPendingRequest(ctx context.Context, db *store.Store, id string) error {
	p, err := getPendingByID(ctx, db, id)
	if err != nil {
		return err
	}
	if p.Status != "pending" {
		return fmt.Errorf("%w: заявка уже %s", ErrConflict, p.Status)
	}
	return db.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE platform_admin_requests SET par_status = 'rejected', par_resolved_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		return writeAuditTx(ctx, tx, AuditWrite{
			Source: "admin", Action: "deployment_admin." + p.Action + ".rejected",
			ActorAccountID: &p.RequestedBy, RequestID: ptr(id),
		})
	})
}

func isAdminTx(ctx context.Context, tx pgx.Tx, accountID string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM platform_admins WHERE account_id = $1)`, accountID).Scan(&ok)
	return ok, err
}

func countAdminsTx(ctx context.Context, tx pgx.Tx) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM platform_admins`).Scan(&n)
	return n, err
}
