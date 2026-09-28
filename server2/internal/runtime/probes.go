// probes.go — асинхронный паттерн "заявка → опрос" (contract §5): update/
// models/local-skills/local-skills-import, таблица `executor_probes`
// (003_agents.up.sql, уже спроектирована сессией T-025 ровно под эту форму —
// см. docs/51-data-model.md, "RuntimeUpdateRequest, RuntimeModelListRequest,
// RuntimeLocalSkillListRequest, RuntimeLocalSkillImportRequest" →
// `executor_probes`). Решение T-028 (см. decisions.md): контракт описывает
// хранилище заявок как "в памяти процесса, не переживает рестарт", но
// готовая персистентная таблица проще и надёжнее (переживает рестарт
// сервера, не теряет заявку на середине heartbeat-цикла) — используется она,
// а не отдельная in-memory карта.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/adanman/goosar/server2/internal/store"
)

// ProbeKind — probe_kind.
type ProbeKind string

const (
	ProbeUpdate           ProbeKind = "self_update"
	ProbeModelList        ProbeKind = "model_list"
	ProbeLocalSkills      ProbeKind = "local_skills"
	ProbeLocalSkillImport ProbeKind = "local_skill_import"
)

// pendingWindow/runningWindow — таймауты §5 "Асинхронный паттерн" (по видам заявки).
var pendingWindow = map[ProbeKind]time.Duration{
	ProbeUpdate: 120 * time.Second, ProbeModelList: 30 * time.Second,
	ProbeLocalSkills: 3 * time.Minute, ProbeLocalSkillImport: 3 * time.Minute,
}
var runningWindow = map[ProbeKind]time.Duration{
	ProbeUpdate: 150 * time.Second, ProbeModelList: 60 * time.Second,
	ProbeLocalSkills: 60 * time.Second, ProbeLocalSkillImport: 60 * time.Second,
}

// Probe — строка executor_probes.
type Probe struct {
	ID         string
	ExecutorID string
	Kind       ProbeKind
	Status     string // pending/running/completed/failed/timeout/conflict(только import)
	Request    json.RawMessage
	Outcome    json.RawMessage
	Error      *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const probeColumns = `id, executor_id, probe_kind, probe_status, probe_request, probe_outcome, probe_error, created_at, updated_at`

func scanProbe(row interface {
	Scan(dest ...any) error
}) (Probe, error) {
	var p Probe
	var kind, status string
	var req, out []byte
	if err := row.Scan(&p.ID, &p.ExecutorID, &kind, &status, &req, &out, &p.Error, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Probe{}, err
	}
	p.Kind = ProbeKind(kind)
	p.Status = status
	if len(req) > 0 {
		p.Request = req
	} else {
		p.Request = json.RawMessage(`{}`)
	}
	if len(out) > 0 {
		p.Outcome = out
	}
	return p, nil
}

// expireStale переводит протухшие pending/running заявки одного рантайма в
// timeout — вызывается лениво перед каждым чтением/записью, а не фоновым
// таймером (нет отдельного планировщика в этом процессе).
func (s *Store) expireStale(ctx context.Context, executorID string) error {
	for kind, win := range pendingWindow {
		if _, err := s.db.Pool.Exec(ctx, `
			UPDATE executor_probes SET probe_status = 'timeout', updated_at = now()
			WHERE executor_id = $1 AND probe_kind = $2 AND probe_status = 'pending'
				AND created_at < now() - make_interval(secs => $3)`,
			executorID, string(kind), int(win.Seconds())); err != nil {
			return fmt.Errorf("runtime: устаревание заявок (pending, %s): %w", kind, err)
		}
	}
	for kind, win := range runningWindow {
		if _, err := s.db.Pool.Exec(ctx, `
			UPDATE executor_probes SET probe_status = 'timeout', updated_at = now()
			WHERE executor_id = $1 AND probe_kind = $2 AND probe_status = 'running'
				AND updated_at < now() - make_interval(secs => $3)`,
			executorID, string(kind), int(win.Seconds())); err != nil {
			return fmt.Errorf("runtime: устаревание заявок (running, %s): %w", kind, err)
		}
	}
	return nil
}

// CreatePendingUpdate — initiateRuntimeUpdate: не более одной необработанной
// заявки на обновление одновременно (409 иначе).
func (s *Store) CreatePendingUpdate(ctx context.Context, executorID, targetVersion string) (Probe, bool, error) {
	if err := s.expireStale(ctx, executorID); err != nil {
		return Probe{}, false, err
	}
	var exists bool
	if err := s.db.Pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM executor_probes WHERE executor_id = $1 AND probe_kind = $2 AND probe_status IN ('pending','running'))`,
		executorID, string(ProbeUpdate)).Scan(&exists); err != nil {
		return Probe{}, false, fmt.Errorf("runtime: проверка необработанной заявки на обновление: %w", err)
	}
	if exists {
		return Probe{}, false, nil
	}
	req, _ := json.Marshal(map[string]string{"target_version": targetVersion})
	p, err := s.createProbe(ctx, executorID, ProbeUpdate, req)
	return p, err == nil, err
}

// CreateProbe — models/local-skills/local-skills-import: не ограничены
// "только одна одновременно" (contract: "для models/local-skills такого
// ограничения нет").
func (s *Store) CreateProbe(ctx context.Context, executorID string, kind ProbeKind, request any) (Probe, error) {
	if err := s.expireStale(ctx, executorID); err != nil {
		return Probe{}, err
	}
	req, _ := json.Marshal(request)
	return s.createProbe(ctx, executorID, kind, req)
}

func (s *Store) createProbe(ctx context.Context, executorID string, kind ProbeKind, request json.RawMessage) (Probe, error) {
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO executor_probes (executor_id, probe_kind, probe_request)
		VALUES ($1, $2, $3) RETURNING `+probeColumns, executorID, string(kind), request)
	p, err := scanProbe(row)
	if err != nil {
		return Probe{}, fmt.Errorf("runtime: создание заявки: %w", err)
	}
	return p, nil
}

// PendingByKind — старейшая ожидающая заявка рантайма данного вида (для
// daemon:heartbeat_ack — daemon сам решает выполнять или нет).
func (s *Store) PendingByKind(ctx context.Context, executorID string, kind ProbeKind) (Probe, bool, error) {
	if err := s.expireStale(ctx, executorID); err != nil {
		return Probe{}, false, err
	}
	row := s.db.Pool.QueryRow(ctx, `
		SELECT `+probeColumns+` FROM executor_probes
		WHERE executor_id = $1 AND probe_kind = $2 AND probe_status = 'pending'
		ORDER BY created_at LIMIT 1`, executorID, string(kind))
	p, err := scanProbe(row)
	if store.IsNoRows(err) {
		return Probe{}, false, nil
	}
	if err != nil {
		return Probe{}, false, fmt.Errorf("runtime: ожидающая заявка: %w", err)
	}
	return p, true, nil
}

// AllPendingImports — pending_local_skill_imports (форма-массив, в отличие
// от единичных update/model_list/local_skills).
func (s *Store) AllPendingImports(ctx context.Context, executorID string) ([]Probe, error) {
	if err := s.expireStale(ctx, executorID); err != nil {
		return nil, err
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+probeColumns+` FROM executor_probes
		WHERE executor_id = $1 AND probe_kind = $2 AND probe_status = 'pending'
		ORDER BY created_at`, executorID, string(ProbeLocalSkillImport))
	if err != nil {
		return nil, fmt.Errorf("runtime: ожидающие заявки импорта: %w", err)
	}
	defer rows.Close()
	var out []Probe
	for rows.Next() {
		p, err := scanProbe(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProbe — по (executorID, id), для GET .../{requestId} и daemonReport*Result.
func (s *Store) GetProbe(ctx context.Context, executorID, id string) (Probe, error) {
	if err := s.expireStale(ctx, executorID); err != nil {
		return Probe{}, err
	}
	row := s.db.Pool.QueryRow(ctx, `SELECT `+probeColumns+` FROM executor_probes WHERE executor_id = $1 AND id = $2`, executorID, id)
	p, err := scanProbe(row)
	if store.IsNoRows(err) {
		return Probe{}, ErrProbeNotFound
	}
	if err != nil {
		return Probe{}, fmt.Errorf("runtime: получение заявки: %w", err)
	}
	return p, nil
}

var ErrProbeNotFound = errors.New("runtime: заявка не найдена")

// ReportResult — daemonReport*Result: заявка уже терминальна/неизвестна —
// принимается и игнорируется (контракт), found=false отражает именно это.
func (s *Store) ReportResult(ctx context.Context, executorID, id, status string, outcome any, probeErr string) (Probe, bool, error) {
	out, _ := json.Marshal(outcome)
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE executor_probes SET probe_status = $3, probe_outcome = $4, probe_error = NULLIF($5,''), updated_at = now()
		WHERE executor_id = $1 AND id = $2 AND probe_status IN ('pending','running')
		RETURNING `+probeColumns, executorID, id, status, out, probeErr)
	p, err := scanProbe(row)
	if store.IsNoRows(err) {
		return Probe{}, false, nil
	}
	if err != nil {
		return Probe{}, false, fmt.Errorf("runtime: запись результата заявки: %w", err)
	}
	return p, true, nil
}
