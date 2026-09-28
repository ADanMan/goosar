package deployment

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// decodeAuditCursor разбирает непрозрачный курсор (contract §7 "cursor
// переключает выдачу в режим 'вперёд от курсора'"): base64(created_at
// RFC3339Nano + "|" + id) — та же запись, что кладётся в поле cursor каждой
// отданной строки (см. toAuditEntry).
func decodeAuditCursor(raw string) (createdAt time.Time, id string, ok bool) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", false
	}
	parts := strings.SplitN(string(b), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", false
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", false
	}
	return t, parts[1], true
}

func encodeAuditCursor(t time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.Format(time.RFC3339Nano) + "|" + id))
}

func (d *Deps) handleListAudit(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.requireDeploymentAdmin(w, r); !ok {
		return
	}
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			httpapi.BadRequest(w, "limit must be an integer between 1 and 200")
			return
		}
		limit = n
	}
	source := q.Get("source")
	if source == "" {
		source = "all"
	}
	if source != "all" && source != "admin" && source != "auth" {
		httpapi.BadRequest(w, "source must be admin, auth or all")
		return
	}
	var since, until *time.Time
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			httpapi.BadRequest(w, "since must be RFC3339")
			return
		}
		since = &t
	}
	if v := q.Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			httpapi.BadRequest(w, "until must be RFC3339")
			return
		}
		until = &t
	}
	var cursorTime *time.Time
	var cursorID string
	if v := q.Get("cursor"); v != "" {
		t, id, ok := decodeAuditCursor(v)
		if !ok {
			httpapi.BadRequest(w, "invalid cursor")
			return
		}
		cursorTime, cursorID = &t, id
	}

	conds := []string{"1=1"}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if source != "all" {
		conds = append(conds, "paud_source = "+arg(source))
	}
	if action := q.Get("action"); action != "" {
		conds = append(conds, "paud_action = "+arg(action))
	}
	if actorParam := q.Get("actor"); actorParam != "" {
		conds = append(conds, "(paud_actor_account_id::text = "+arg(actorParam)+" OR paud_actor_id = "+arg(actorParam)+")")
	}
	if since != nil {
		conds = append(conds, "created_at >= "+arg(*since))
	}
	if until != nil {
		conds = append(conds, "created_at <= "+arg(*until))
	}

	var query string
	if cursorTime != nil {
		conds = append(conds, "(created_at > "+arg(*cursorTime)+" OR (created_at = "+arg(*cursorTime)+" AND id::text > "+arg(cursorID)+"))")
		query = fmt.Sprintf(`SELECT %s FROM platform_audit_log WHERE %s ORDER BY created_at ASC, id ASC LIMIT %s`,
			auditColumns, strings.Join(conds, " AND "), arg(limit))
	} else {
		query = fmt.Sprintf(`SELECT %s FROM platform_audit_log WHERE %s ORDER BY created_at DESC, id DESC LIMIT %s`,
			auditColumns, strings.Join(conds, " AND "), arg(limit))
	}

	rows, err := d.DB.Pool.Query(r.Context(), query, args...)
	if checkErr(w, err) {
		return
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		e, createdAt, id, err := scanAuditRow(rows)
		if checkErr(w, err) {
			return
		}
		e.Cursor = encodeAuditCursor(createdAt, id)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		internalError(w)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

const auditColumns = `id, paud_source, paud_action, paud_actor_account_id, paud_actor_type, paud_actor_id,
	paud_actor_role, paud_target_type, paud_target_id, paud_outcome, paud_reason, paud_before_hash,
	paud_after_hash, workspace_id, paud_request_id, paud_client_ip_digest, paud_client_agent, created_at`

type auditRowScanner interface {
	Scan(dest ...any) error
}

func scanAuditRow(row auditRowScanner) (AuditEntry, time.Time, string, error) {
	var e AuditEntry
	var createdAt time.Time
	err := row.Scan(&e.ID, &e.Source, &e.Action, &e.ActorUserID, &e.ActorType, &e.ActorID, &e.ActorRole,
		&e.TargetType, &e.TargetID, &e.Outcome, &e.Reason, &e.BeforeHash, &e.AfterHash, &e.WorkspaceID,
		&e.RequestIDHdr, &e.ClientIP, &e.UserAgent, &createdAt)
	e.CreatedAt = createdAt
	if err != nil {
		return AuditEntry{}, time.Time{}, "", err
	}
	return e, createdAt, e.ID, nil
}
