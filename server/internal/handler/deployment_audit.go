package handler

import (
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	auditSourceAdmin = "admin"
	auditSourceAuth  = "auth"
)

type DeploymentAuditEntry struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Action string `json:"action"`

	ActorUserID string `json:"actor_user_id,omitempty"`
	ActorType   string `json:"actor_type,omitempty"`
	ActorID     string `json:"actor_id,omitempty"`
	ActorRole   string `json:"actor_role,omitempty"`

	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id,omitempty"`

	Outcome string `json:"outcome,omitempty"`
	Reason  string `json:"reason,omitempty"`

	BeforeHash string `json:"before_hash,omitempty"`
	AfterHash  string `json:"after_hash,omitempty"`

	WorkspaceID string `json:"workspace_id,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
	ClientIP    string `json:"client_ip,omitempty"`
	UserAgent   string `json:"user_agent,omitempty"`

	CreatedAt time.Time `json:"created_at"`

	Cursor string `json:"cursor,omitempty"`
}

type auditQuery struct {
	action    pgtype.Text
	actorText pgtype.Text
	actorUUID pgtype.UUID
	since     pgtype.Timestamptz
	until     pgtype.Timestamptz
	limit     int32
	cursorAt  time.Time
	cursorID  pgtype.UUID
	forward   bool
	source    string
}

func (h *Handler) ListDeploymentAdminAudit(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDeploymentAdmin(w, r); !ok {
		return
	}
	q, ok := parseAuditQuery(w, r)
	if !ok {
		return
	}

	entries := make([]DeploymentAuditEntry, 0, q.limit*2)
	if q.source != auditSourceAuth {
		rows, err := h.readAdminAudit(r, q)
		if err != nil {
			slog.Error("deployment audit: list admin journal failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list audit entries")
			return
		}
		entries = append(entries, rows...)
	}
	if q.source != auditSourceAdmin {
		rows, err := h.readAuthAudit(r, q)
		if err != nil {
			slog.Error("deployment audit: list auth journal failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list audit entries")
			return
		}
		entries = append(entries, rows...)
	}

	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
			if q.forward {
				return entries[i].CreatedAt.Before(entries[j].CreatedAt)
			}
			return entries[i].CreatedAt.After(entries[j].CreatedAt)
		}

		if q.forward {
			return entries[i].ID < entries[j].ID
		}
		return entries[i].ID > entries[j].ID
	})
	if len(entries) > int(q.limit) {
		entries = entries[:q.limit]
	}
	writeJSON(w, http.StatusOK, entries)
}

func parseAuditQuery(w http.ResponseWriter, r *http.Request) (auditQuery, bool) {
	var q auditQuery
	query := r.URL.Query()

	q.limit = int32(defaultAdminAuditLimit)
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return q, false
		}
		q.limit = int32(min(parsed, maxAdminAuditLimit))
	}

	if action := strings.TrimSpace(query.Get("action")); action != "" {
		q.action = pgtype.Text{String: action, Valid: true}
	}
	if actor := strings.TrimSpace(query.Get("actor")); actor != "" {

		q.actorText = pgtype.Text{String: actor, Valid: true}
		if id, err := util.ParseUUID(actor); err == nil {
			q.actorUUID = id
		}
	}
	for _, spec := range []struct {
		name string
		dest *pgtype.Timestamptz
	}{{"since", &q.since}, {"until", &q.until}} {
		raw := strings.TrimSpace(query.Get(spec.name))
		if raw == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, spec.name+" must be an RFC3339 timestamp")
			return q, false
		}
		*spec.dest = pgtype.Timestamptz{Time: parsed, Valid: true}
	}

	if raw, present := query["cursor"]; present {
		q.forward = true
		value := strings.TrimSpace(strings.Join(raw, ""))
		if value == "" {

			q.cursorID = pgtype.UUID{Bytes: [16]byte{}, Valid: true}
			return q, true
		}
		at, id, err := decodeAuditCursor(value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "cursor is malformed; use the cursor field of a previously returned entry")
			return q, false
		}
		q.cursorAt, q.cursorID = at, id
	}

	switch source := strings.TrimSpace(query.Get("source")); source {
	case "", "all":
		q.source = ""
	case auditSourceAdmin, auditSourceAuth:
		q.source = source
	default:
		writeError(w, http.StatusBadRequest, `source must be "admin", "auth" or "all"`)
		return q, false
	}
	return q, true
}

func (h *Handler) readAdminAudit(r *http.Request, q auditQuery) ([]DeploymentAuditEntry, error) {
	var rows []db.AdminAudit
	var err error
	if q.forward {
		rows, err = h.Queries.ListAdminAuditForward(r.Context(), db.ListAdminAuditForwardParams{
			Action:   q.action,
			Actor:    q.actorUUID,
			Until:    q.until,
			CursorAt: pgtype.Timestamptz{Time: q.cursorAt, Valid: true},
			CursorID: q.cursorID,
			Lim:      q.limit,
		})
	} else {
		rows, err = h.Queries.ListAdminAuditRecent(r.Context(), db.ListAdminAuditRecentParams{
			Action: q.action,
			Actor:  q.actorUUID,
			Since:  q.since,
			Until:  q.until,
			Lim:    q.limit,
		})
	}
	if err != nil {
		return nil, err
	}

	if q.actorText.Valid && !q.actorUUID.Valid {
		return nil, nil
	}
	entries := make([]DeploymentAuditEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, DeploymentAuditEntry{
			ID:          uuidToString(row.ID),
			Source:      auditSourceAdmin,
			Action:      row.Action,
			ActorUserID: optionalUUIDString(row.ActorUserID),
			ActorID:     optionalUUIDString(row.ActorUserID),
			ActorType:   "user",
			TargetType:  row.TargetType,
			TargetID:    row.TargetID.String,
			BeforeHash:  row.BeforeHash.String,
			AfterHash:   row.AfterHash.String,
			RequestID:   row.RequestID.String,
			CreatedAt:   row.CreatedAt.Time,
			Cursor:      encodeAuditCursor(row.CreatedAt.Time, uuidToString(row.ID)),
		})
	}
	return entries, nil
}

func (h *Handler) readAuthAudit(r *http.Request, q auditQuery) ([]DeploymentAuditEntry, error) {
	var rows []db.AuthAudit
	var err error
	if q.forward {
		rows, err = h.Queries.ListAuthAuditForward(r.Context(), db.ListAuthAuditForwardParams{
			Action:   q.action,
			Actor:    q.actorText,
			Until:    q.until,
			CursorAt: pgtype.Timestamptz{Time: q.cursorAt, Valid: true},
			CursorID: q.cursorID,
			Lim:      q.limit,
		})
	} else {
		rows, err = h.Queries.ListAuthAuditRecent(r.Context(), db.ListAuthAuditRecentParams{
			Action: q.action,
			Actor:  q.actorText,
			Since:  q.since,
			Until:  q.until,
			Lim:    q.limit,
		})
	}
	if err != nil {
		return nil, err
	}
	entries := make([]DeploymentAuditEntry, 0, len(rows))
	for _, row := range rows {
		entry := DeploymentAuditEntry{
			ID:          uuidToString(row.ID),
			Source:      auditSourceAuth,
			Action:      row.Action,
			ActorType:   row.ActorType,
			ActorID:     row.ActorID.String,
			ActorRole:   row.ActorRole.String,
			TargetType:  row.TargetType,
			TargetID:    row.TargetID.String,
			Outcome:     row.Outcome,
			Reason:      row.Reason.String,
			WorkspaceID: optionalUUIDString(row.WorkspaceID),
			RequestID:   row.RequestID.String,
			ClientIP:    row.ClientIp.String,
			UserAgent:   row.UserAgent.String,
			CreatedAt:   row.CreatedAt.Time,
			Cursor:      encodeAuditCursor(row.CreatedAt.Time, uuidToString(row.ID)),
		}

		if row.ActorType == "user" {
			entry.ActorUserID = row.ActorID.String
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func encodeAuditCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeAuditCursor(token string) (time.Time, pgtype.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return time.Time{}, pgtype.UUID{}, err
	}
	at, id, found := strings.Cut(string(raw), "|")
	if !found {
		return time.Time{}, pgtype.UUID{}, errMalformedAuditCursor
	}
	parsedAt, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, pgtype.UUID{}, err
	}
	parsedID, err := util.ParseUUID(id)
	if err != nil {
		return time.Time{}, pgtype.UUID{}, err
	}
	return parsedAt, parsedID, nil
}

var errMalformedAuditCursor = errors.New("audit cursor: expected <timestamp>|<uuid>")
