package export

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/adanman/goosar/server2/internal/store"
)

type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

// Job — schemas.WorkspaceExportJob.
type Job struct {
	ID          string
	WorkspaceID string
	Status      string
	Error       *string
	SizeBytes   int64
	Manifest    map[string]any
	StorageURI  *string
	CreatedAt   time.Time
	CompletedAt *time.Time
}

var errActiveJobExists = fmt.Errorf("export: активный экспорт этого пространства уже выполняется")

// ErrActiveJobExists — export: "только один активный экспорт на пространство"
// (409, partial unique index space_export_jobs_one_active_uk).
func ErrActiveJobExists() error { return errActiveJobExists }

// CreateJob вставляет pending job; конфликт с partial unique index
// (уже есть pending/running job этого пространства) возвращает
// errActiveJobExists.
func (s *Store) CreateJob(ctx context.Context, workspaceID string) (Job, error) {
	var j Job
	err := s.db.Pool.QueryRow(ctx, `
		INSERT INTO space_export_jobs (workspace_id, exp_status)
		VALUES ($1, 'pending')
		ON CONFLICT (workspace_id) WHERE exp_status IN ('pending','running') DO NOTHING
		RETURNING id, workspace_id, exp_status, exp_error, exp_size_bytes, exp_manifest, exp_storage_uri, created_at, exp_completed_at`,
		workspaceID,
	).Scan(&j.ID, &j.WorkspaceID, &j.Status, &j.Error, &j.SizeBytes, &j.Manifest, &j.StorageURI, &j.CreatedAt, &j.CompletedAt)
	if store.IsNoRows(err) {
		return Job{}, errActiveJobExists
	}
	if err != nil {
		return Job{}, fmt.Errorf("export: создание job: %w", err)
	}
	return j, nil
}

func (s *Store) GetJob(ctx context.Context, workspaceID, id string) (Job, bool, error) {
	var j Job
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, workspace_id, exp_status, exp_error, exp_size_bytes, exp_manifest, exp_storage_uri, created_at, exp_completed_at
		FROM space_export_jobs WHERE id = $1 AND workspace_id = $2`, id, workspaceID,
	).Scan(&j.ID, &j.WorkspaceID, &j.Status, &j.Error, &j.SizeBytes, &j.Manifest, &j.StorageURI, &j.CreatedAt, &j.CompletedAt)
	if store.IsNoRows(err) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	return j, true, nil
}

func (s *Store) MarkRunning(ctx context.Context, id string) error {
	_, err := s.db.Pool.Exec(ctx, `UPDATE space_export_jobs SET exp_status = 'running' WHERE id = $1`, id)
	return err
}

func (s *Store) MarkCompleted(ctx context.Context, id, storageURI string, sizeBytes int64, manifest map[string]any) error {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	_, err = s.db.Pool.Exec(ctx, `
		UPDATE space_export_jobs
		SET exp_status = 'completed', exp_storage_uri = $2, exp_size_bytes = $3, exp_manifest = $4, exp_completed_at = now()
		WHERE id = $1`, id, storageURI, sizeBytes, raw)
	return err
}

func (s *Store) MarkFailed(ctx context.Context, id, errMsg string) error {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE space_export_jobs SET exp_status = 'failed', exp_error = $2, exp_completed_at = now() WHERE id = $1`,
		id, errMsg)
	return err
}

// ClearStorageURI — download отдал 410, потому что архив уже стёрт по
// ретенции; contract сам не требует сервер стирать файл (это делает внешний
// storage-lifecycle), но раз мы решили ретеншн сами (см. decisions.md), сами
// и снимаем ссылку, чтобы не пытаться скачать заведомо отсутствующий объект
// повторно.
func (s *Store) ClearStorageURI(ctx context.Context, id string) error {
	_, err := s.db.Pool.Exec(ctx, `UPDATE space_export_jobs SET exp_storage_uri = NULL WHERE id = $1`, id)
	return err
}

// --- сбор данных для архива воркспейса ---------------------------------------

type WorkspaceRow struct {
	ID          string
	Name        string
	Slug        string
	Description *string
	IssuePrefix string
	CreatedAt   time.Time
}

func (s *Store) Workspace(ctx context.Context, id string) (WorkspaceRow, error) {
	var w WorkspaceRow
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, ws_title, ws_slug, ws_summary, ws_ticket_prefix, created_at FROM spaces WHERE id = $1`, id,
	).Scan(&w.ID, &w.Name, &w.Slug, &w.Description, &w.IssuePrefix, &w.CreatedAt)
	return w, err
}

func (s *Store) MembersJSON(ctx context.Context, workspaceID string) ([]map[string]any, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT a.id, a.acct_email, a.acct_full_name, sm.sm_role, sm.created_at
		FROM space_members sm JOIN accounts a ON a.id = sm.account_id
		WHERE sm.workspace_id = $1 ORDER BY sm.created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, email, name, role string
		var createdAt time.Time
		if err := rows.Scan(&id, &email, &name, &role, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"account_id": id, "email": email, "name": name, "role": role, "joined_at": createdAt})
	}
	return out, rows.Err()
}

func (s *Store) TicketsJSON(ctx context.Context, workspaceID string) ([]map[string]any, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, tk_display_key, tk_headline, tk_status, tk_priority, tk_creator_type, tk_creator_id, created_at, updated_at
		FROM tickets WHERE workspace_id = $1 ORDER BY tk_seq_number`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, key, headline, status, priority, creatorType, creatorID string
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &key, &headline, &status, &priority, &creatorType, &creatorID, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "display_key": key, "headline": headline, "status": status, "priority": priority,
			"creator_type": creatorType, "creator_id": creatorID, "created_at": createdAt, "updated_at": updatedAt,
		})
	}
	return out, rows.Err()
}

func (s *Store) NotesJSON(ctx context.Context, workspaceID string) ([]map[string]any, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT tn.id, tn.ticket_id, tn.tn_author_type, tn.tn_author_id, tn.tn_body, tn.created_at
		FROM ticket_notes tn JOIN tickets t ON t.id = tn.ticket_id
		WHERE t.workspace_id = $1 ORDER BY tn.created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, ticketID, authorType, authorID, body string
		var createdAt time.Time
		if err := rows.Scan(&id, &ticketID, &authorType, &authorID, &body, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "ticket_id": ticketID, "author_type": authorType, "author_id": authorID,
			"body": body, "created_at": createdAt,
		})
	}
	return out, rows.Err()
}

func (s *Store) ProjectsJSON(ctx context.Context, workspaceID string) ([]map[string]any, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, init_title, init_status, created_at FROM initiatives WHERE workspace_id = $1 ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, title, status string
		var createdAt time.Time
		if err := rows.Scan(&id, &title, &status, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "title": title, "status": status, "created_at": createdAt})
	}
	return out, rows.Err()
}

// --- me/export: данные вызывающего -------------------------------------------

func (s *Store) Profile(ctx context.Context, accountID string) (map[string]any, error) {
	var email, name string
	var bio string
	var createdAt time.Time
	err := s.db.Pool.QueryRow(ctx, `
		SELECT acct_email, acct_full_name, acct_bio, created_at FROM accounts WHERE id = $1`, accountID,
	).Scan(&email, &name, &bio, &createdAt)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": accountID, "email": email, "name": name, "bio": bio, "created_at": createdAt}, nil
}

func (s *Store) MembershipsJSON(ctx context.Context, accountID string) ([]map[string]any, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT sp.id, sp.ws_title, sp.ws_slug, sm.sm_role
		FROM space_members sm JOIN spaces sp ON sp.id = sm.workspace_id
		WHERE sm.account_id = $1 ORDER BY sm.created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, name, slug, role string
		if err := rows.Scan(&id, &name, &slug, &role); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"workspace_id": id, "name": name, "slug": slug, "role": role})
	}
	return out, rows.Err()
}

func (s *Store) TicketsCreatedByJSON(ctx context.Context, accountID string) ([]map[string]any, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, workspace_id, tk_display_key, tk_headline, created_at
		FROM tickets WHERE tk_creator_type = 'member' AND tk_creator_id = $1 ORDER BY created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, workspaceID, key, headline string
		var createdAt time.Time
		if err := rows.Scan(&id, &workspaceID, &key, &headline, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "workspace_id": workspaceID, "display_key": key, "headline": headline, "created_at": createdAt})
	}
	return out, rows.Err()
}

func (s *Store) TokensJSON(ctx context.Context, accountID string) ([]map[string]any, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, ak_title, ak_secret_prefix, ak_valid_until, ak_last_used_at, created_at
		FROM access_keys WHERE account_id = $1 ORDER BY created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, title, prefix string
		var validUntil, lastUsed *time.Time
		var createdAt time.Time
		if err := rows.Scan(&id, &title, &prefix, &validUntil, &lastUsed, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "name": title, "token_prefix": prefix, "expires_at": validUntil, "last_used_at": lastUsed, "created_at": createdAt,
		})
	}
	return out, rows.Err()
}

func (s *Store) SessionsJSON(ctx context.Context, accountID string) ([]map[string]any, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, sess_client_agent, sess_last_ping_at, created_at
		FROM login_sessions WHERE account_id = $1 AND sess_invalidated_at IS NULL ORDER BY created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, agent string
		var lastPing, createdAt time.Time
		if err := rows.Scan(&id, &agent, &lastPing, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "client_agent": agent, "last_ping_at": lastPing, "created_at": createdAt})
	}
	return out, rows.Err()
}

// --- аудит --------------------------------------------------------------------

// RecordAudit пишет platform_audit_log (011_governance.up.sql); та же схема,
// что internal/agent.Store.recordAudit уже использует для §10.3 —
// paud_source='admin' переиспользуется как единственный источник аудита на
// момент этой сессии (см. server2/docs/decisions.md).
func (s *Store) RecordAudit(ctx context.Context, workspaceID, actorAccountID, action, targetID string) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO platform_audit_log (paud_source, paud_action, paud_actor_account_id, paud_actor_type,
			paud_target_type, paud_target_id, paud_outcome, workspace_id)
		VALUES ('admin', $1, NULLIF($2,'')::uuid, 'human', 'export', $3, 'ok', NULLIF($4,'')::uuid)`,
		action, actorAccountID, targetID, workspaceID)
	if err != nil {
		return fmt.Errorf("export: запись аудита: %w", err)
	}
	return nil
}
