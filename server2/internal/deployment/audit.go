package deployment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
)

// AuditWrite — одна запись platform_audit_log (объединяет старые admin_audit
// и auth_audit — см. 011_governance.up.sql, "paud_source"). Указатели —
// nullable-колонки; пустая строка не пишется как значение, передавайте nil.
type AuditWrite struct {
	Source         string // "admin" | "auth"
	Action         string
	ActorAccountID *string
	ActorType      *string
	ActorID        *string
	ActorRole      *string
	TargetType     *string
	TargetID       *string
	Outcome        *string
	Reason         *string
	BeforeHash     *string
	AfterHash      *string
	WorkspaceID    *string
	RequestID      *string
	ClientIPDigest *string
	ClientAgent    *string
}

// WriteAudit пишет одну строку platform_audit_log. Не транзакционна сама по
// себе — вызывающий передаёт db.Pool напрямую (обычный случай) либо
// использует WriteAuditTx внутри своей транзакции, когда запись аудита
// обязана либо примениться, либо откатиться вместе с основным изменением
// (двухканальное подтверждение заявок, см. admins.go).
func WriteAudit(ctx context.Context, db *store.Store, e AuditWrite) error {
	_, err := db.Pool.Exec(ctx, insertAuditSQL,
		e.Source, e.Action, e.ActorAccountID, e.ActorType, e.ActorID, e.ActorRole,
		e.TargetType, e.TargetID, e.Outcome, e.Reason, e.BeforeHash, e.AfterHash,
		e.WorkspaceID, e.RequestID, e.ClientIPDigest, e.ClientAgent)
	if err != nil {
		return fmt.Errorf("deployment: запись аудита: %w", err)
	}
	return nil
}

const insertAuditSQL = `
	INSERT INTO platform_audit_log
		(paud_source, paud_action, paud_actor_account_id, paud_actor_type, paud_actor_id, paud_actor_role,
		 paud_target_type, paud_target_id, paud_outcome, paud_reason, paud_before_hash, paud_after_hash,
		 workspace_id, paud_request_id, paud_client_ip_digest, paud_client_agent)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`

// internalError — общий короткий алиас на "500 internal_error", которым этот
// пакет отвечает на непредвиденные ошибки БД/сериализации, вместо того чтобы
// повторять `httpapi.WriteError(w, http.StatusInternalServerError, ...)`
// дословно в каждом обработчике.
func internalError(w http.ResponseWriter) {
	httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
}

// checkErr — общий guard "если err не nil, ответить 500 и сообщить
// вызывающему, что дальше продолжать незачем": `if checkErr(w, err) { return }`
// вместо трёхстрочного `if err != nil { internalError(w); return }`,
// повторённого во множестве обработчиков этого пакета.
func checkErr(w http.ResponseWriter, err error) bool {
	if err != nil {
		internalError(w)
		return true
	}
	return false
}

func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ipDigest(r *http.Request) *string {
	ip := httpapi.ClientIP(r)
	if ip == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(ip))
	d := hex.EncodeToString(sum[:])
	return &d
}

// httpAudit — заготовка admin-аудита из HTTP-запроса вызывающего
// deployment-admin: actor_type=human, actor_role=deployment-admin,
// request_id/client_ip_digest/user_agent — из контекста запроса.
func httpAudit(r *http.Request, actor *httpapi.Actor, action string) AuditWrite {
	return httpAuditAs(r, actor, action, "deployment-admin")
}

// httpAuditAs — то же самое, но для ручек, где вызывающий не обязан быть
// deployment-admin (например GET /api/deployment/client-secrets —
// any-authenticated) и роль в аудите должна отражать это честно.
func httpAuditAs(r *http.Request, actor *httpapi.Actor, action, role string) AuditWrite {
	human := "human"
	e := AuditWrite{
		Source:         "admin",
		Action:         action,
		ActorAccountID: ptr(actor.UserID),
		ActorType:      &human,
		ActorRole:      &role,
		ClientIPDigest: ipDigest(r),
		ClientAgent:    ptr(r.UserAgent()),
	}
	if rid := httpapi.RequestIDFrom(r.Context()); rid != "" {
		e.RequestID = &rid
	}
	return e
}

// cliAudit — заготовка admin-аудита из cmd/admin (нет HTTP-запроса, нет
// аутентифицированного актора — операторская команда исполняется напрямую
// против БД, см. server2/cmd/admin и server2/docs/decisions.md, T-029).
func cliAudit(action string) AuditWrite {
	cli := "cli"
	tool := "goosar_admin"
	return AuditWrite{Source: "admin", Action: action, ActorType: &cli, ActorID: &tool}
}

// requireDeploymentAdmin — общий пролог ручек /api/deployment/**: человек,
// голова роли deployment-admin (проверка запросом к platform_admins, не
// связана с ролью в конкретном пространстве — contract §7 вступление).
func (d *Deps) requireDeploymentAdmin(w http.ResponseWriter, r *http.Request) (*httpapi.Actor, bool) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return nil, false
	}
	isAdmin, err := IsAdmin(r.Context(), d.DB, actor.UserID)
	if err != nil {
		internalError(w)
		return nil, false
	}
	if !isAdmin {
		httpapi.Forbidden(w, "deployment-admin role required")
		return nil, false
	}
	return actor, true
}
