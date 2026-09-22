package metrics

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (c *BusinessSamplerCollector) queryActiveUsers(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	const stmt = `
SELECT count(DISTINCT user_id) FROM (
  SELECT cs.creator_id AS user_id
  FROM chat_session cs
  WHERE EXISTS (
    SELECT 1 FROM chat_message cm
    WHERE cm.chat_session_id = cs.id
      AND cm.created_at > now() - $1::interval
  )
  UNION ALL
  SELECT i.creator_id AS user_id
  FROM issue i
  WHERE i.creator_type = 'member'
    AND EXISTS (
      SELECT 1 FROM agent_task_queue atq
      WHERE atq.issue_id = i.id
        AND atq.created_at > now() - $1::interval
    )
) u
`
	for _, w := range samplerWindows {
		var n int64
		if err := tx.QueryRow(ctx, stmt, w.d).Scan(&n); err != nil {
			return fmt.Errorf("active_users window=%s: %w", w.label, err)
		}
		snap.activeUsers[w.label] = float64(n)
	}
	return nil
}

func (c *BusinessSamplerCollector) queryActiveWorkspaces(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	const stmt = `
SELECT count(DISTINCT workspace_id) FROM (
  SELECT cs.workspace_id
  FROM chat_session cs
  WHERE EXISTS (
    SELECT 1 FROM chat_message cm
    WHERE cm.chat_session_id = cs.id
      AND cm.created_at > now() - $1::interval
  )
  UNION ALL
  SELECT i.workspace_id
  FROM issue i
  WHERE EXISTS (
    SELECT 1 FROM agent_task_queue atq
    WHERE atq.issue_id = i.id
      AND atq.created_at > now() - $1::interval
  )
) w
`
	for _, w := range samplerWindows {
		var n int64
		if err := tx.QueryRow(ctx, stmt, w.d).Scan(&n); err != nil {
			return fmt.Errorf("active_workspaces window=%s: %w", w.label, err)
		}
		snap.activeWorkspaces[w.label] = float64(n)
	}
	return nil
}

func (c *BusinessSamplerCollector) queryTaskQueued(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	const stmt = `
SELECT
  CASE
    WHEN chat_session_id IS NOT NULL THEN 'chat'
    WHEN autopilot_run_id IS NOT NULL THEN 'autopilot'
    WHEN issue_id IS NOT NULL THEN 'issue'
    ELSE 'other'
  END AS source,
  count(*) AS n
FROM agent_task_queue
WHERE status = 'queued'
GROUP BY 1
LIMIT 100
`
	rows, err := tx.Query(ctx, stmt)
	if err != nil {
		return fmt.Errorf("task_queued: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rawSource string
		var n int64
		if err := rows.Scan(&rawSource, &n); err != nil {
			return fmt.Errorf("task_queued scan: %w", err)
		}
		snap.taskQueued[NormalizeTaskSource(rawSource)] += float64(n)
	}
	return rows.Err()
}

func (c *BusinessSamplerCollector) queryTaskRunning(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {

	const stmt = `
WITH in_flight AS (
  SELECT chat_session_id, autopilot_run_id, issue_id, runtime_id
  FROM agent_task_queue
  WHERE status = 'dispatched'
  UNION ALL
  SELECT chat_session_id, autopilot_run_id, issue_id, runtime_id
  FROM agent_task_queue
  WHERE status = 'running'
)
SELECT
  CASE
    WHEN atq.chat_session_id IS NOT NULL THEN 'chat'
    WHEN atq.autopilot_run_id IS NOT NULL THEN 'autopilot'
    WHEN atq.issue_id IS NOT NULL THEN 'issue'
    ELSE 'other'
  END AS source,
  COALESCE(ar.runtime_mode, 'unknown') AS runtime_mode,
  count(*) AS n
FROM in_flight atq
LEFT JOIN agent_runtime ar ON ar.id = atq.runtime_id
GROUP BY 1, 2
LIMIT 100
`
	rows, err := tx.Query(ctx, stmt)
	if err != nil {
		return fmt.Errorf("task_running: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rawSource, rawMode string
		var n int64
		if err := rows.Scan(&rawSource, &rawMode, &n); err != nil {
			return fmt.Errorf("task_running scan: %w", err)
		}
		key := taskRunningKey{
			source:      NormalizeTaskSource(rawSource),
			runtimeMode: NormalizeRuntimeMode(rawMode),
		}
		snap.taskRunning[key] += float64(n)
	}
	return rows.Err()
}

func (c *BusinessSamplerCollector) queryTaskStuck(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	stmt := `
SELECT
  CASE
    WHEN chat_session_id IS NOT NULL THEN 'chat'
    WHEN autopilot_run_id IS NOT NULL THEN 'autopilot'
    WHEN issue_id IS NOT NULL THEN 'issue'
    ELSE 'other'
  END AS source,
  count(*) AS n
FROM agent_task_queue
WHERE status = 'running'
  AND started_at IS NOT NULL
  AND started_at < now() - interval '` + stuckRunningInterval + `'
GROUP BY 1
LIMIT 100
`
	rows, err := tx.Query(ctx, stmt)
	if err != nil {
		return fmt.Errorf("task_stuck: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rawSource string
		var n int64
		if err := rows.Scan(&rawSource, &n); err != nil {
			return fmt.Errorf("task_stuck scan: %w", err)
		}
		snap.taskStuck[NormalizeTaskSource(rawSource)] += float64(n)
	}
	return rows.Err()
}

func (c *BusinessSamplerCollector) queryRuntimeOnline(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	const stmt = `
SELECT runtime_mode, provider, count(*) AS n
FROM agent_runtime
WHERE last_seen_at IS NOT NULL
  AND last_seen_at > now() - ($1::int * interval '1 second')
GROUP BY 1, 2
LIMIT 100
`
	rows, err := tx.Query(ctx, stmt, runtimeOnlineWindowSeconds)
	if err != nil {
		return fmt.Errorf("runtime_online: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rawMode, rawProvider string
		var n int64
		if err := rows.Scan(&rawMode, &rawProvider, &n); err != nil {
			return fmt.Errorf("runtime_online scan: %w", err)
		}
		key := runtimeOnlineKey{
			runtimeMode: NormalizeRuntimeMode(rawMode),
			provider:    NormalizeRuntimeProvider(rawProvider),
		}
		snap.runtimeOnline[key] += float64(n)
	}
	return rows.Err()
}

func (c *BusinessSamplerCollector) queryRuntimeHeartbeatAge(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	const stmt = `
SELECT runtime_mode, EXTRACT(EPOCH FROM (now() - last_seen_at))::float8 AS age
FROM agent_runtime
WHERE last_seen_at IS NOT NULL
  AND last_seen_at > now() - interval '15 minutes'
ORDER BY last_seen_at DESC
LIMIT 100
`
	rows, err := tx.Query(ctx, stmt)
	if err != nil {
		return fmt.Errorf("runtime_heartbeat_age: %w", err)
	}
	defer rows.Close()

	perMode := map[string][]float64{}
	for rows.Next() {
		var rawMode string
		var age float64
		if err := rows.Scan(&rawMode, &age); err != nil {
			return fmt.Errorf("runtime_heartbeat_age scan: %w", err)
		}
		if age < 0 {
			age = 0
		}
		mode := NormalizeRuntimeMode(rawMode)
		perMode[mode] = append(perMode[mode], age)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for mode, ages := range perMode {
		hist := samplerHistogram{
			buckets: make(map[float64]uint64, len(heartbeatAgeBuckets)),
		}
		for _, b := range heartbeatAgeBuckets {
			hist.buckets[b] = 0
		}
		for _, age := range ages {
			hist.count++
			hist.sum += age
			for _, b := range heartbeatAgeBuckets {
				if age <= b {
					hist.buckets[b]++
				}
			}
		}
		snap.heartbeatAge[mode] = hist
	}
	return nil
}

func (c *BusinessSamplerCollector) queryWorkspaceTotal(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	const stmt = `SELECT count(*) FROM workspace LIMIT 100`
	var n int64
	if err := tx.QueryRow(ctx, stmt).Scan(&n); err != nil {
		return fmt.Errorf("workspace_total: %w", err)
	}
	snap.workspaceTotal = float64(n)
	snap.workspaceTotalKnown = true
	return nil
}
