package autopilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/store"
)

var ErrRunNotFound = errors.New("autopilot: прогон не найден")
var ErrDeliveryNotFound = errors.New("autopilot: доставка не найдена")

const runColumns = `id, sentinel_id, sentinel_trigger_id, srun_source, srun_status, ticket_id, srun_dispatch_job_id,
	srun_completed_at, srun_failure_reason, srun_reason_code, srun_trigger_payload, srun_result, created_at`

func scanRun(row pgx.Row) (Run, error) {
	var r Run
	var jobID *string
	var payload, result []byte
	if err := row.Scan(&r.ID, &r.AutopilotID, &r.TriggerID, &r.Source, &r.Status, &r.IssueID, &jobID,
		&r.CompletedAt, &r.FailureReason, &r.ReasonCode, &payload, &result, &r.CreatedAt); err != nil {
		return Run{}, err
	}
	if len(payload) > 0 {
		r.TriggerPayload = payload
	}
	if len(result) > 0 {
		r.Result = result
	}
	r.TaskID = jobID
	r.TriggeredAt = r.CreatedAt
	return r, nil
}

// CreateRunParams — вход CreateRun (admission автопилота, см. dispatcher.go).
type CreateRunParams struct {
	AutopilotID    string
	TriggerID      *string
	Source         string // schedule|manual|webhook|api
	Status         string
	IssueID        *string
	DispatchJobID  *string
	FailureReason  *string
	ReasonCode     *string
	TriggerPayload []byte
}

func (s *Store) CreateRun(ctx context.Context, p CreateRunParams) (Run, error) {
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO sentinel_runs (
			sentinel_id, sentinel_trigger_id, srun_source, srun_status, ticket_id, srun_dispatch_job_id,
			srun_failure_reason, srun_reason_code, srun_trigger_payload
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+runColumns,
		p.AutopilotID, p.TriggerID, p.Source, p.Status, p.IssueID, p.DispatchJobID,
		p.FailureReason, p.ReasonCode, p.TriggerPayload)
	r, err := scanRun(row)
	if err != nil {
		return Run{}, fmt.Errorf("autopilot: создание прогона: %w", err)
	}
	return r, nil
}

// CompleteRun обновляет статус прогона после исхода admission (issue_created/
// running/skipped/failed — completed приходил бы асинхронно от демона,
// вне рамок этой сессии, см. server2/docs/decisions.md).
func (s *Store) CompleteRun(ctx context.Context, runID, status string, completedAt *time.Time) error {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE sentinel_runs SET srun_status = $2, srun_completed_at = $3 WHERE id = $1`, runID, status, completedAt)
	return err
}

func (s *Store) GetRun(ctx context.Context, autopilotID, runID string) (Run, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+runColumns+` FROM sentinel_runs WHERE sentinel_id = $1 AND id = $2`, autopilotID, runID)
	r, err := scanRun(row)
	if store.IsNoRows(err) {
		return Run{}, ErrRunNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("autopilot: получение прогона: %w", err)
	}
	return r, nil
}

func (s *Store) ListRuns(ctx context.Context, autopilotID string, limit, offset int) ([]Run, int, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+runColumns+` FROM sentinel_runs WHERE sentinel_id = $1
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`, autopilotID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("autopilot: список прогонов: %w", err)
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var total int
	if err := s.db.Pool.QueryRow(ctx, `SELECT count(*) FROM sentinel_runs WHERE sentinel_id = $1`, autopilotID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("autopilot: подсчёт прогонов: %w", err)
	}
	return out, total, nil
}

// --- deliveries --------------------------------------------------------------

const deliveryColumns = `id, workspace_id, sentinel_id, sentinel_trigger_id, whe_provider, whe_event,
	whe_dedupe_key, whe_dedupe_source, whe_signature_status, whe_status, whe_attempt_count, whe_dispatch_attempts,
	whe_available_at, whe_content_type, whe_response_status, sentinel_run_id, whe_replayed_from_id, whe_error,
	whe_received_at, whe_last_attempt_at, created_at`

func scanDelivery(row pgx.Row) (Delivery, error) {
	var d Delivery
	if err := row.Scan(&d.ID, &d.WorkspaceID, &d.AutopilotID, &d.TriggerID, &d.Provider, &d.Event,
		&d.DedupeKey, &d.DedupeSource, &d.SignatureStatus, &d.Status, &d.AttemptCount, &d.DispatchAttempts,
		&d.AvailableAt, &d.ContentType, &d.ResponseStatus, &d.AutopilotRunID, &d.ReplayedFromDeliveryID, &d.Error,
		&d.ReceivedAt, &d.LastAttemptAt, &d.CreatedAt); err != nil {
		return Delivery{}, err
	}
	return d, nil
}

// CreateDeliveryParams — вход CreateDelivery (webhooksReceiveAutopilotTrigger).
type CreateDeliveryParams struct {
	WorkspaceID            string
	AutopilotID            string
	TriggerID              string
	Provider               string
	Event                  string
	DedupeKey              *string
	DedupeSource           *string
	SignatureStatus        string
	Status                 string
	ContentType            *string
	ResponseStatus         *int
	AutopilotRunID         *string
	ReplayedFromDeliveryID *string
	Error                  *string
	SelectedHeaders        map[string]string
	RawBody                *string
	ResponseBody           *string
}

func (s *Store) CreateDelivery(ctx context.Context, p CreateDeliveryParams) (Delivery, error) {
	headers, err := headersJSON(p.SelectedHeaders)
	if err != nil {
		return Delivery{}, err
	}
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO webhook_events (
			workspace_id, sentinel_id, sentinel_trigger_id, whe_provider, whe_event, whe_dedupe_key, whe_dedupe_source,
			whe_signature_status, whe_status, whe_content_type, whe_response_status, sentinel_run_id,
			whe_replayed_from_id, whe_error, whe_selected_headers, whe_raw_body, whe_response_body, whe_last_attempt_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17, now())
		RETURNING `+deliveryColumns,
		p.WorkspaceID, p.AutopilotID, p.TriggerID, p.Provider, p.Event, p.DedupeKey, p.DedupeSource,
		p.SignatureStatus, p.Status, p.ContentType, p.ResponseStatus, p.AutopilotRunID,
		p.ReplayedFromDeliveryID, p.Error, headers, p.RawBody, p.ResponseBody)
	d, err := scanDelivery(row)
	if err != nil {
		return Delivery{}, fmt.Errorf("autopilot: создание доставки: %w", err)
	}
	d.SelectedHeaders, d.RawBody, d.ResponseBody = p.SelectedHeaders, p.RawBody, p.ResponseBody
	return d, nil
}

func (s *Store) GetDelivery(ctx context.Context, autopilotID, deliveryID string) (Delivery, error) {
	row := s.db.Pool.QueryRow(ctx, `
		SELECT `+deliveryColumns+`, whe_selected_headers, whe_raw_body, whe_response_body
		FROM webhook_events WHERE sentinel_id = $1 AND id = $2`, autopilotID, deliveryID)
	var d Delivery
	var headersRaw []byte
	err := row.Scan(&d.ID, &d.WorkspaceID, &d.AutopilotID, &d.TriggerID, &d.Provider, &d.Event,
		&d.DedupeKey, &d.DedupeSource, &d.SignatureStatus, &d.Status, &d.AttemptCount, &d.DispatchAttempts,
		&d.AvailableAt, &d.ContentType, &d.ResponseStatus, &d.AutopilotRunID, &d.ReplayedFromDeliveryID, &d.Error,
		&d.ReceivedAt, &d.LastAttemptAt, &d.CreatedAt, &headersRaw, &d.RawBody, &d.ResponseBody)
	if store.IsNoRows(err) {
		return Delivery{}, ErrDeliveryNotFound
	}
	if err != nil {
		return Delivery{}, fmt.Errorf("autopilot: получение доставки: %w", err)
	}
	if len(headersRaw) > 0 {
		var headers map[string]string
		if err := json.Unmarshal(headersRaw, &headers); err != nil {
			return Delivery{}, fmt.Errorf("autopilot: разбор заголовков доставки: %w", err)
		}
		d.SelectedHeaders = headers
	}
	return d, nil
}

func (s *Store) ListDeliveries(ctx context.Context, autopilotID string, limit, offset int) ([]Delivery, int, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+deliveryColumns+` FROM webhook_events WHERE sentinel_id = $1
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`, autopilotID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("autopilot: список доставок: %w", err)
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var total int
	if err := s.db.Pool.QueryRow(ctx, `SELECT count(*) FROM webhook_events WHERE sentinel_id = $1`, autopilotID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("autopilot: подсчёт доставок: %w", err)
	}
	return out, total, nil
}

// FindDeliveryByDedupe — дедуп входящего вебхука по X-GitHub-Delivery/
// Idempotency-Key (contract: "повтор отдаёт тот же ответ, не создавая новый
// run").
func (s *Store) FindDeliveryByDedupe(ctx context.Context, triggerID, dedupeKey string) (Delivery, bool, error) {
	row := s.db.Pool.QueryRow(ctx, `
		SELECT `+deliveryColumns+` FROM webhook_events
		WHERE sentinel_trigger_id = $1 AND whe_dedupe_key = $2
		ORDER BY created_at LIMIT 1`, triggerID, dedupeKey)
	d, err := scanDelivery(row)
	if store.IsNoRows(err) {
		return Delivery{}, false, nil
	}
	if err != nil {
		return Delivery{}, false, fmt.Errorf("autopilot: поиск повторной доставки: %w", err)
	}
	return d, true, nil
}

func headersJSON(h map[string]string) ([]byte, error) {
	if len(h) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("autopilot: сериализация заголовков доставки: %w", err)
	}
	return b, nil
}
