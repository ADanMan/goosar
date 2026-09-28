// Правка T-028: переходы жизненного цикла одной уже выданной задачи
// (daemonStartTask/daemonMarkTaskWaitingLocalDirectory/daemonCompleteTask/
// daemonFailTask/daemonPinTaskSession/daemonGetTaskStatus) — аддитивные
// методы поверх Store из T-027 (store.go), тот же приём, что claim.go.
package dispatch

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Start — daemonStartTask: dispatched/waiting_local_directory -> running.
// found=false, если задачи нет либо она уже не в подходящем статусе (вызывающий
// домен отвечает 400/404 соответственно — GetJob различит эти два случая).
func (s *Store) Start(ctx context.Context, q Querier, jobID string) (Job, bool, error) {
	row := q.QueryRow(ctx, `
		UPDATE dispatch_jobs SET dj_status = 'running', dj_started_at = COALESCE(dj_started_at, now()), updated_at = now()
		WHERE id = $1 AND dj_status = ANY($2)
		RETURNING `+jobColumns,
		jobID, statusStrings([]Status{StatusDispatched, StatusWaitingLocalDirectory}))
	return scanLifecycle(row, "старт задачи")
}

// WaitLocalDirectory — daemonMarkTaskWaitingLocalDirectory: dispatched/running -> waiting_local_directory.
func (s *Store) WaitLocalDirectory(ctx context.Context, q Querier, jobID, reason string) (Job, bool, error) {
	row := q.QueryRow(ctx, `
		UPDATE dispatch_jobs SET dj_status = 'waiting_local_directory', updated_at = now()
		WHERE id = $1 AND dj_status = ANY($2)
		RETURNING `+jobColumns,
		jobID, statusStrings([]Status{StatusDispatched, StatusRunning}))
	j, found, err := scanLifecycle(row, "ожидание локальной директории")
	if err == nil && found && reason != "" {
		_, _ = q.Exec(ctx, `UPDATE dispatch_jobs SET dj_error = $2 WHERE id = $1`, jobID, "waiting_local_directory: "+reason)
	}
	return j, found, err
}

// CompleteInput — тело daemonCompleteTask (contract: pr_url/output/session_id/work_dir/session_rollout_missing).
type CompleteInput struct {
	PRURL                 string
	Output                string
	SessionID             string
	WorkDir               string
	SessionRolloutMissing bool
}

// Complete — daemonCompleteTask: -> completed, dj_result зеркалит тело запроса
// (contract: AgentTask.result "its shape mirrors whichever of those two
// request bodies the daemon last sent").
func (s *Store) Complete(ctx context.Context, q Querier, jobID string, in CompleteInput) (Job, bool, error) {
	result, _ := json.Marshal(map[string]any{
		"pr_url": in.PRURL, "output": in.Output, "session_id": in.SessionID, "work_dir": in.WorkDir,
		"session_rollout_missing": in.SessionRolloutMissing,
	})
	row := q.QueryRow(ctx, `
		UPDATE dispatch_jobs
		SET dj_status = 'completed', dj_completed_at = now(), dj_result = $2,
			dj_prior_session_id = COALESCE(NULLIF($3,''), dj_prior_session_id),
			dj_work_dir = COALESCE(NULLIF($4,''), dj_work_dir),
			dj_claim_secret_digest = NULL, updated_at = now()
		WHERE id = $1
		RETURNING `+jobColumns,
		jobID, result, in.SessionID, in.WorkDir)
	return scanLifecycle(row, "завершение задачи")
}

// FailInput — тело daemonFailTask.
type FailInput struct {
	Error                 string
	SessionID             string
	WorkDir               string
	FailureReason         string
	SessionRolloutMissing bool
}

// Fail — daemonFailTask: -> failed.
func (s *Store) Fail(ctx context.Context, q Querier, jobID string, in FailInput) (Job, bool, error) {
	result, _ := json.Marshal(map[string]any{
		"error": in.Error, "session_id": in.SessionID, "work_dir": in.WorkDir,
		"failure_reason": in.FailureReason, "session_rollout_missing": in.SessionRolloutMissing,
	})
	row := q.QueryRow(ctx, `
		UPDATE dispatch_jobs
		SET dj_status = 'failed', dj_completed_at = now(), dj_result = $2, dj_error = NULLIF($3,''),
			dj_failure_reason = NULLIF($4,''),
			dj_work_dir = COALESCE(NULLIF($5,''), dj_work_dir),
			dj_claim_secret_digest = NULL, updated_at = now()
		WHERE id = $1
		RETURNING `+jobColumns,
		jobID, result, in.Error, in.FailureReason, in.WorkDir)
	return scanLifecycle(row, "провал задачи")
}

// PinSession — daemonPinTaskSession: запоминает id сессии рантайма и/или
// рабочую директорию текущего запуска, для будущего resume того же процесса
// (prior_session_id/prior_work_dir следующей попытки читаются из dj_parent_job_id
// цепочки, см. AgentTask-сборку в internal/daemon).
func (s *Store) PinSession(ctx context.Context, q Querier, jobID, sessionID, workDir string) (bool, error) {
	tag, err := q.Exec(ctx, `
		UPDATE dispatch_jobs SET
			dj_prior_session_id = COALESCE(NULLIF($2,''), dj_prior_session_id),
			dj_work_dir = COALESCE(NULLIF($3,''), dj_work_dir),
			updated_at = now()
		WHERE id = $1`, jobID, sessionID, workDir)
	if err != nil {
		return false, fmt.Errorf("dispatch: сохранение сессии задачи: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// StatusOf — daemonGetTaskStatus: только строка статуса, без похода за всей строкой.
func (s *Store) StatusOf(ctx context.Context, q Querier, jobID string) (string, bool, error) {
	var status string
	err := q.QueryRow(ctx, `SELECT dj_status FROM dispatch_jobs WHERE id = $1`, jobID).Scan(&status)
	if pgx.ErrNoRows == err {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("dispatch: статус задачи: %w", err)
	}
	return status, true, nil
}

func scanLifecycle(row pgx.Row, op string) (Job, bool, error) {
	j, err := scanJob(row)
	if pgx.ErrNoRows == err {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("dispatch: %s: %w", op, err)
	}
	return j, true, nil
}
