// Правка T-028 (daemon-протокол, internal/daemon): claim/lifecycle-операции
// демона поверх dispatch_jobs — аддитивные методы Store, не меняющие ничего
// из claim.go/lifecycle.go/usage.go/messages.go в T-027 (store.go). Домен
// daemon — единственный вызывающий; сам dispatch по-прежнему не знает о
// демон-протоколе (auth_token/lease и т.п.), только даёт SQL-примитивы над
// своей таблицей.
package dispatch

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// qualifiedJobColumns — jobColumns с явным алиасом таблицы перед каждым
// именем: нужно там, где RETURNING идёт после `UPDATE ... AS alias FROM
// candidate` — без алиаса "id" неоднозначен (совпадает и в dispatch_jobs, и
// в CTE candidate), что Postgres отклоняет `column reference "id" is
// ambiguous` уже на этапе выполнения, не планирования (обнаружено при живой
// проверке демон-протокола, см. отчёт).
func qualifiedJobColumns(alias string) string {
	cols := strings.Split(jobColumns, ",")
	for i, c := range cols {
		cols[i] = alias + "." + strings.TrimSpace(c)
	}
	return strings.Join(cols, ", ")
}

// ClaimNext — один захват задачи из очереди рантайма (contract:
// daemonClaimTaskForRuntime/daemonClaimTasksBatch). Форма запроса — ровно
// та, что рекомендует docs/51-data-model.md, раздел «Очередь задач агентов»:
// `SELECT ... FOR UPDATE SKIP LOCKED` в CTE, затем `UPDATE ... FROM
// candidate` — один атомарный оператор, конкурентные вызовы для одного и
// того же executorID никогда не берут одну строку и не блокируют друг
// друга. claimSecretDigest — sha256 короткоживущего mat_-токена (auth_token),
// который демон получит в ответе claim; сохраняется здесь же, чтобы токен и
// перевод в dispatched были одной транзакцией на стороне вызывающего (q
// может быть pgx.Tx).
func (s *Store) ClaimNext(ctx context.Context, q Querier, executorID, claimSecretDigest string) (Job, bool, error) {
	row := q.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id FROM dispatch_jobs
			WHERE executor_id = $1 AND dj_status = 'queued'
			ORDER BY dj_priority DESC, created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE dispatch_jobs AS dj
		SET dj_status = 'dispatched', dj_dispatched_at = now(), dj_claim_secret_digest = $2, updated_at = now()
		FROM candidate
		WHERE dj.id = candidate.id
		RETURNING `+qualifiedJobColumns("dj"),
		executorID, claimSecretDigest)
	j, err := scanJob(row)
	if pgx.ErrNoRows == err {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("dispatch: захват задачи: %w", err)
	}
	return j, true, nil
}

// FindByClaimSecretDigest ищет строку по хэшу выданного при claim
// mat_-токена — так authn.TaskActorLookup (см. internal/daemon) резолвит
// Actor для последующих /api/daemon/tasks/{id}/** без похода в саму задачу
// по id. found=false, если токен неизвестен, отозван (см. RevokeClaimSecret)
// или задача уже терминальна (токен по контракту живёт, пока задача активна).
func (s *Store) FindByClaimSecretDigest(ctx context.Context, q Querier, digest string) (Job, bool, error) {
	row := q.QueryRow(ctx, `SELECT `+jobColumns+` FROM dispatch_jobs
		WHERE dj_claim_secret_digest = $1 AND dj_status = ANY($2)`,
		digest, statusStrings(append(append([]Status{}, activeStatuses...), StatusCompleted, StatusFailed)))
	j, err := scanJob(row)
	if pgx.ErrNoRows == err {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("dispatch: поиск задачи по токену: %w", err)
	}
	return j, true, nil
}

// RevokeClaimSecret стирает сохранённый хэш токена (contract: complete/fail
// "отзывает mat_... токен задачи") — дальнейшие вызовы FindByClaimSecretDigest
// с этим же токеном больше не находят строку.
func (s *Store) RevokeClaimSecret(ctx context.Context, q Querier, jobID string) error {
	_, err := q.Exec(ctx, `UPDATE dispatch_jobs SET dj_claim_secret_digest = NULL WHERE id = $1`, jobID)
	if err != nil {
		return fmt.Errorf("dispatch: отзыв токена задачи: %w", err)
	}
	return nil
}

// ListDispatchedOrRunning — задачи рантайма, уже выданные/выполняющиеся
// (daemonListPendingTasksByRuntime — восстановление состояния демона после
// рестарта, и первый шаг RecoverOrphans).
func (s *Store) ListDispatchedOrRunning(ctx context.Context, q Querier, executorID string) ([]Job, error) {
	rows, err := q.Query(ctx, `SELECT `+jobColumns+` FROM dispatch_jobs
		WHERE executor_id = $1 AND dj_status = ANY($2)
		ORDER BY dj_dispatched_at NULLS LAST, created_at`,
		executorID, statusStrings([]Status{StatusDispatched, StatusWaitingLocalDirectory, StatusRunning}))
	if err != nil {
		return nil, fmt.Errorf("dispatch: список выданных задач рантайма: %w", err)
	}
	return collectJobs(rows)
}

// RecoverOrphans — daemonRecoverOrphanedTasks: рантайм только что
// переподключился, у него не может быть настоящих dispatched/running
// запусков от предыдущей жизни процесса — они либо возвращаются в очередь
// (dj_attempt ещё не исчерпан), либо проваливаются (лимит попыток исчерпан).
// Возвращает (найдено orphaned, из них возвращено в очередь).
func (s *Store) RecoverOrphans(ctx context.Context, q Querier, executorID string) (orphaned, retried int, err error) {
	orphans, err := s.ListDispatchedOrRunning(ctx, q, executorID)
	if err != nil {
		return 0, 0, err
	}
	for _, j := range orphans {
		orphaned++
		if j.Attempt < j.MaxAttempts {
			_, execErr := q.Exec(ctx, `
				UPDATE dispatch_jobs
				SET dj_status = 'queued', dj_attempt = dj_attempt + 1,
					dj_dispatched_at = NULL, dj_started_at = NULL, dj_claim_secret_digest = NULL,
					updated_at = now()
				WHERE id = $1`, j.ID)
			if execErr != nil {
				return orphaned, retried, fmt.Errorf("dispatch: возврат осиротевшей задачи в очередь: %w", execErr)
			}
			retried++
			continue
		}
		_, execErr := q.Exec(ctx, `
			UPDATE dispatch_jobs
			SET dj_status = 'failed', dj_completed_at = now(), dj_error = 'runtime reconnected: orphaned task',
				dj_failure_reason = 'orphaned', dj_claim_secret_digest = NULL, updated_at = now()
			WHERE id = $1`, j.ID)
		if execErr != nil {
			return orphaned, retried, fmt.Errorf("dispatch: провал осиротевшей задачи: %w", execErr)
		}
	}
	return orphaned, retried, nil
}

// TouchLease — daemonExtendTaskPrepareLease: контракт не фиксирует числовую
// длину окна «готовлюсь к старту» отдельной колонкой (см. decisions.md,
// раздел T-028) — здесь это сводится к обновлению updated_at на ещё не
// стартовавшей (dispatched/waiting_local_directory) задаче, чтобы
// GetJob/ListDispatchedOrRunning отражали, что демон недавно подавал
// признаки жизни по этой задаче. found=false — задача не в подходящем
// статусе или не принадлежит executorID.
func (s *Store) TouchLease(ctx context.Context, q Querier, executorID, jobID string) (Job, bool, error) {
	row := q.QueryRow(ctx, `
		UPDATE dispatch_jobs SET updated_at = now()
		WHERE id = $1 AND executor_id = $2 AND dj_status = ANY($3)
		RETURNING `+jobColumns,
		jobID, executorID, statusStrings([]Status{StatusDispatched, StatusWaitingLocalDirectory}))
	j, err := scanJob(row)
	if pgx.ErrNoRows == err {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("dispatch: продление lease: %w", err)
	}
	return j, true, nil
}

// CancelActiveForExecutor отменяет все активные запуски рантайма
// (archiveAgentsAndDeleteRuntime/deleteRuntime — контракт требует отменить
// задачи архивируемых агентов и задачи самого рантайма).
func (s *Store) CancelActiveForExecutor(ctx context.Context, q Querier, executorID string) ([]Job, error) {
	rows, err := q.Query(ctx, `
		UPDATE dispatch_jobs SET dj_status = 'cancelled', updated_at = now()
		WHERE executor_id = $1 AND dj_status = ANY($2)
		RETURNING `+jobColumns, executorID, statusStrings(activeStatuses))
	if err != nil {
		return nil, fmt.Errorf("dispatch: отмена активных запусков рантайма: %w", err)
	}
	return collectJobs(rows)
}
