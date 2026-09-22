import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  aggregateAgentFailures,
  anonymizeUnresolvedAgentRows,
  UNRESOLVED_AGENTS_ROW_ID,
  aggregateAgentTokens,
  aggregateDailyCost,
  aggregateDailyErrors,
  aggregateFailureClasses,
  aggregateFailureReasons,
  aggregateWeeklyErrors,
  aggregateWeeklyTasks,
  aggregateWeeklyTime,
  bucketUnknownAgentRows,
  computeDailyTotals,
  computeFailureTotals,
  DELETED_AGENTS_ROW_ID,
  formatDuration,
  hasRateSample,
  mergeAgentDashboardRows,
  sortAgentFailures,
} from './utils';

describe('aggregateDailyCost', () => {
  it('collapses multiple rows per day into one stack and sorts by date asc', () => {
    const result = aggregateDailyCost([
      {
        date: '2026-05-10',
        provider: 'claude',
        model: 'claude-sonnet-4-6',
        input_tokens: 1_000_000,
        output_tokens: 500_000,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 3,
      },
      {
        date: '2026-05-09',
        provider: 'claude',
        model: 'claude-sonnet-4-6',
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 1,
      },
    ]);

    expect(result.map((r) => r.date)).toEqual(['2026-05-09', '2026-05-10']);
    expect(result[0]).toMatchObject({ input: 3, output: 0, cacheWrite: 0, total: 3 });
    expect(result[1]).toMatchObject({ input: 3, output: 7.5, cacheWrite: 0, total: 10.5 });
  });

  it('treats unmapped models as zero-cost', () => {
    const result = aggregateDailyCost([
      {
        date: '2026-05-10',
        provider: 'claude',
        model: 'made-up-model',
        input_tokens: 999_999_999,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 0,
      },
    ]);
    expect(result[0]?.total).toBe(0);
  });
});

describe('aggregateAgentTokens', () => {
  it('folds per-(agent, model) rows into per-agent totals and sorts by cost desc', () => {
    const rows = aggregateAgentTokens([
      {
        agent_id: 'small-spender',
        provider: 'claude',
        model: 'claude-sonnet-4-6',
        input_tokens: 100_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 1,
      },
      {
        agent_id: 'big-spender',
        provider: 'claude',
        model: 'claude-sonnet-4-6',
        input_tokens: 5_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 3,
      },
      {
        agent_id: 'big-spender',
        provider: 'claude',
        model: 'claude-haiku-4-5',
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 2,
      },
    ]);

    expect(rows.map((r) => r.agentId)).toEqual(['big-spender', 'small-spender']);
    expect(rows[0]?.taskCount).toBe(5);
    expect(rows[0]!.cost).toBeGreaterThan(rows[1]!.cost);
  });
});

describe('computeDailyTotals', () => {
  it('sums tokens across rows and adds estimated cost', () => {
    const totals = computeDailyTotals([
      {
        date: '2026-05-10',
        provider: 'claude',
        model: 'claude-sonnet-4-6',
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 2,
      },
      {
        date: '2026-05-09',
        provider: 'claude',
        model: 'claude-sonnet-4-6',
        input_tokens: 2_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 3,
      },
    ]);
    expect(totals.input).toBe(3_000_000);
    expect(totals.cost).toBe(9); 
    expect(totals.taskCount).toBe(5);
  });
});

describe('mergeAgentDashboardRows', () => {
  it("uses run-time rollup's per-agent task count, not the token sum", () => {
    const tokenRows = [
      {
        agentId: 'agent-a',
        tokens: 3_000_000,
        cost: 12,
        taskCount: 2, // overcounted because (model-1: 1) + (model-2: 1)
      },
    ];
    const runTimeRows = [
      {
        agent_id: 'agent-a',
        total_seconds: 600,
        task_count: 1, // truth: one task touched both models
        failed_count: 0,
      },
    ];
    const merged = mergeAgentDashboardRows(tokenRows, runTimeRows);
    expect(merged).toHaveLength(1);
    expect(merged[0]!.taskCount).toBe(1);
    expect(merged[0]!.seconds).toBe(600);
  });

  it('falls back to token count when no run-time row exists (in-flight task)', () => {
    const merged = mergeAgentDashboardRows(
      [{ agentId: 'agent-b', tokens: 100, cost: 0.5, taskCount: 1 }],
      [],
    );
    expect(merged[0]!.taskCount).toBe(1);
    expect(merged[0]!.seconds).toBe(0);
  });

  it('includes agents that have run-time but no tokens', () => {
    const merged = mergeAgentDashboardRows(
      [],
      [{ agent_id: 'agent-c', total_seconds: 30, task_count: 1, failed_count: 1 }],
    );
    expect(merged).toHaveLength(1);
    expect(merged[0]!.tokens).toBe(0);
    expect(merged[0]!.cost).toBe(0);
    expect(merged[0]!.taskCount).toBe(1);
  });

  it('sorts by cost desc with run-time as a tiebreaker', () => {
    const merged = mergeAgentDashboardRows(
      [
        { agentId: 'low', tokens: 100, cost: 1, taskCount: 1 },
        { agentId: 'high', tokens: 100, cost: 9, taskCount: 1 },
        { agentId: 'zero-cost-long', tokens: 0, cost: 0, taskCount: 0 },
      ],
      [{ agent_id: 'zero-cost-long', total_seconds: 1000, task_count: 5, failed_count: 0 }],
    );
    expect(merged.map((r) => r.agentId)).toEqual(['high', 'low', 'zero-cost-long']);
  });
});

describe('bucketUnknownAgentRows', () => {
  const live = { agentId: 'live', tokens: 100, cost: 1, seconds: 10, taskCount: 1 };
  const archived = {
    agentId: 'archived',
    tokens: 80,
    cost: 0.8,
    seconds: 8,
    taskCount: 2,
  };
  const deletedA = {
    agentId: 'deleted-a',
    tokens: 50,
    cost: 0.5,
    seconds: 5,
    taskCount: 1,
  };
  const deletedB = {
    agentId: 'deleted-b',
    tokens: 30,
    cost: 0.25,
    seconds: 3,
    taskCount: 4,
  };

  it('folds every hard-deleted agent into one aggregated bucket row', () => {
    const out = bucketUnknownAgentRows([live, deletedA, deletedB], new Set(['live']));
    expect(out.map((r) => r.agentId)).toEqual(['live', DELETED_AGENTS_ROW_ID]);
    const bucket = out.find((r) => r.agentId === DELETED_AGENTS_ROW_ID)!;
    expect(bucket.tokens).toBe(80);
    expect(bucket.cost).toBeCloseTo(0.75);
    expect(bucket.seconds).toBe(0);
    expect(bucket.taskCount).toBe(0);
  });

  it('keeps the bucket total reconciled with the top-line spend', () => {
    const out = bucketUnknownAgentRows([live, deletedA, deletedB], new Set(['live']));
    const visibleCost = out.reduce((s, r) => s + r.cost, 0);
    const kpiCost = [live, deletedA, deletedB].reduce((s, r) => s + r.cost, 0);
    expect(visibleCost).toBeCloseTo(kpiCost);
  });

  it('keeps archived agents as themselves, never in the bucket', () => {
    const out = bucketUnknownAgentRows([live, archived, deletedA], new Set(['live', 'archived']));
    expect(out.map((r) => r.agentId)).toEqual(['live', 'archived', DELETED_AGENTS_ROW_ID]);
  });

  it('adds no bucket row when every agent is known', () => {
    const out = bucketUnknownAgentRows([live, archived], new Set(['live', 'archived']));
    expect(out.map((r) => r.agentId)).toEqual(['live', 'archived']);
  });

  it('keeps every row untouched while the agent list is still loading (null set)', () => {
    const out = bucketUnknownAgentRows([live, deletedA], null);
    expect(out.map((r) => r.agentId)).toEqual(['live', 'deleted-a']);
  });
});

describe('formatDuration', () => {
  it('formats seconds-only durations', () => {
    expect(formatDuration(45, '<1m')).toBe('45s');
  });
  it('formats minutes and seconds when under one hour', () => {
    expect(formatDuration(150, '<1m')).toBe('2m 30s');
    expect(formatDuration(60, '<1m')).toBe('1m');
  });
  it('formats hours and minutes when under one day', () => {
    expect(formatDuration(3 * 3600 + 17 * 60, '<1m')).toBe('3h 17m');
    expect(formatDuration(3600, '<1m')).toBe('1h');
  });
  it('formats days and hours when more than 24 hours', () => {
    expect(formatDuration(2 * 86400 + 5 * 3600, '<1m')).toBe('2d 5h');
  });
  it('falls back to the supplied label for sub-second durations', () => {
    expect(formatDuration(0, '<1m')).toBe('<1m');
    expect(formatDuration(0.4, '<1m')).toBe('<1m');
  });
});

describe('aggregateWeeklyTime', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('folds per-day run-time rows into Mon-anchored weekly totals', () => {
    vi.setSystemTime(new Date('2026-05-19T12:00:00Z'));
    const rows = [
      { date: '2026-05-11', total_seconds: 100, task_count: 0, failed_count: 0 },
      { date: '2026-05-17', total_seconds: 50, task_count: 0, failed_count: 0 },
      { date: '2026-05-18', total_seconds: 25, task_count: 0, failed_count: 0 },
    ];
    const result = aggregateWeeklyTime(rows, 'UTC', 2);
    expect(result).toHaveLength(2);
    expect(result[0]).toMatchObject({
      weekStart: '2026-05-11',
      weekEnd: '2026-05-17',
      totalSeconds: 150,
      partial: false,
      daysCovered: 7,
    });
    expect(result[1]).toMatchObject({
      weekStart: '2026-05-18',
      totalSeconds: 25,
      partial: true,
      daysCovered: 2, // Mon + Tue
    });
  });

  it('drops rows that fall outside the trailing window and keeps empty buckets', () => {
    vi.setSystemTime(new Date('2026-05-19T12:00:00Z'));
    const rows = [
      { date: '2026-04-13', total_seconds: 999, task_count: 0, failed_count: 0 },
    ];
    const result = aggregateWeeklyTime(rows, 'UTC', 5);
    expect(result.map((w) => w.weekStart)).toEqual([
      '2026-04-20',
      '2026-04-27',
      '2026-05-04',
      '2026-05-11',
      '2026-05-18',
    ]);
    for (const w of result) expect(w.totalSeconds).toBe(0);
  });
});

describe('aggregateWeeklyTasks', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('splits completed and failed counts per calendar week', () => {
    vi.setSystemTime(new Date('2026-05-19T12:00:00Z'));
    const rows = [
      { date: '2026-05-12', total_seconds: 0, task_count: 5, failed_count: 1 },
      { date: '2026-05-18', total_seconds: 0, task_count: 3, failed_count: 0 },
    ];
    const result = aggregateWeeklyTasks(rows, 'UTC', 2);
    expect(result[0]).toMatchObject({
      weekStart: '2026-05-11',
      completed: 4,
      failed: 1,
    });
    expect(result[1]).toMatchObject({
      weekStart: '2026-05-18',
      completed: 3,
      failed: 0,
      partial: true,
    });
  });
});

describe('aggregateDailyErrors', () => {
  it('stacks failures by class and keeps the succeeded rows as the denominator', () => {
    const result = aggregateDailyErrors([
      { date: '2026-05-10', failure_reason: '', task_count: 8 },
      {
        date: '2026-05-10',
        failure_reason: 'agent_error.provider_auth_or_access',
        task_count: 2,
      },
      { date: '2026-05-10', failure_reason: 'timeout', task_count: 1 },
      { date: '2026-05-09', failure_reason: '', task_count: 4 },
    ]);

    expect(result.map((r) => r.date)).toEqual(['2026-05-09', '2026-05-10']);
    expect(result[1]).toMatchObject({
      auth: 2,
      timeout: 1,
      rate_limit: 0,
      failed: 3,
      total: 11,
    });
    expect(result[0]).toMatchObject({ failed: 0, total: 4 });
  });

  it("folds a reason this build has never seen into 'other' rather than dropping it", () => {
    const [row] = aggregateDailyErrors([
      { date: '2026-05-10', failure_reason: 'agent_error.from_the_future', task_count: 5 },
    ]);
    expect(row).toMatchObject({ other: 5, failed: 5, total: 5 });
  });
});

describe('aggregateWeeklyErrors', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('buckets per calendar week and pre-zeroes weeks with no terminal tasks', () => {
    vi.setSystemTime(new Date('2026-05-19T12:00:00Z'));
    const result = aggregateWeeklyErrors(
      [
        { date: '2026-05-12', failure_reason: 'runtime_offline', task_count: 2 },
        { date: '2026-05-12', failure_reason: '', task_count: 6 },
      ],
      'UTC',
      2,
    );

    expect(result[0]).toMatchObject({
      weekStart: '2026-05-11',
      runtime: 2,
      failed: 2,
      total: 8,
    });
    expect(result[1]).toMatchObject({
      weekStart: '2026-05-18',
      failed: 0,
      total: 0,
      partial: true,
    });
  });
});

describe('computeFailureTotals', () => {
  it('excludes the succeeded bucket from the numerator but not the denominator', () => {
    expect(
      computeFailureTotals([
        { failure_reason: '', task_count: 9 },
        { failure_reason: 'timeout', task_count: 1 },
      ]),
    ).toEqual({ failed: 1, total: 10, rate: 0.1 });
  });

  it('reports rate 0 rather than dividing by zero on an empty window', () => {
    expect(computeFailureTotals([])).toEqual({ failed: 0, total: 0, rate: 0 });
  });
});

describe('aggregateFailureClasses / aggregateFailureReasons', () => {
  const rows = [
    { failure_reason: '', task_count: 20 },
    { failure_reason: 'agent_error.provider_quota_limit', task_count: 3 },
    { failure_reason: 'agent_error.provider_capacity_or_rate_limit', task_count: 4 },
    { failure_reason: 'timeout', task_count: 5 },
  ];

  it('merges reasons that share a class and ranks by count desc', () => {
    expect(aggregateFailureClasses(rows)).toEqual([
      { failureClass: 'rate_limit', count: 7 },
      { failureClass: 'timeout', count: 5 },
    ]);
  });

  it('keeps raw reasons separate so an operator can search the exact string', () => {
    expect(aggregateFailureReasons(rows)).toEqual([
      { reason: 'timeout', failureClass: 'timeout', count: 5 },
      {
        reason: 'agent_error.provider_capacity_or_rate_limit',
        failureClass: 'rate_limit',
        count: 4,
      },
      {
        reason: 'agent_error.provider_quota_limit',
        failureClass: 'rate_limit',
        count: 3,
      },
    ]);
  });
});

describe('aggregateAgentFailures', () => {
  it('ranks by failure count, carries the rate, and splits failures by class', () => {
    const result = aggregateAgentFailures([
      { agent_id: 'a', failure_reason: '', task_count: 90 },
      { agent_id: 'a', failure_reason: 'timeout', task_count: 10 },
      { agent_id: 'b', failure_reason: '', task_count: 1 },
      { agent_id: 'b', failure_reason: 'runtime_offline', task_count: 3 },
      { agent_id: 'b', failure_reason: 'timeout', task_count: 1 },
    ]);

    expect(result.map((r) => [r.agentId, r.failed, r.total, r.rate])).toEqual([
      ['a', 10, 100, 0.1],
      ['b', 4, 5, 0.8],
    ]);
    expect(result[1]?.classes).toMatchObject({ runtime: 3, timeout: 1, auth: 0 });
  });

  it('drops agents with no failures — the list is triage, not a census', () => {
    expect(
      aggregateAgentFailures([{ agent_id: 'clean', failure_reason: '', task_count: 42 }]),
    ).toEqual([]);
  });
});

describe('sortAgentFailures', () => {
  const rows = aggregateAgentFailures([
    { agent_id: 'busy', failure_reason: '', task_count: 900 },
    { agent_id: 'busy', failure_reason: 'timeout', task_count: 100 },
    { agent_id: 'flaky', failure_reason: '', task_count: 80 },
    { agent_id: 'flaky', failure_reason: 'runtime_offline', task_count: 20 },
    { agent_id: 'once', failure_reason: 'timeout', task_count: 1 },
  ]);

  it('ranks by absolute failures by default', () => {
    expect(sortAgentFailures(rows, 'failed').map((r) => r.agentId)).toEqual([
      'busy',
      'flaky',
      'once',
    ]);
  });

  it('ranks by rate, with too-small samples demoted rather than dropped', () => {
    expect(sortAgentFailures(rows, 'rate').map((r) => r.agentId)).toEqual([
      'flaky',
      'busy',
      'once',
    ]);
  });

  it('marks which rows have enough runs for their rate to mean anything', () => {
    expect(rows.map((r) => hasRateSample(r))).toEqual([true, true, false]);
  });

  it('leaves the input array untouched', () => {
    const before = rows.map((r) => r.agentId);
    sortAgentFailures(rows, 'rate');
    expect(rows.map((r) => r.agentId)).toEqual(before);
  });
});

describe('anonymizeUnresolvedAgentRows', () => {
  const rows = [
    { agent_id: 'visible', failure_reason: '', task_count: 5 },
    { agent_id: 'visible', failure_reason: 'timeout', task_count: 5 },
    {
      agent_id: 'private-a',
      failure_reason: 'agent_error.provider_auth_or_access',
      task_count: 6,
    },
    { agent_id: 'private-a', failure_reason: 'timeout', task_count: 5 },
    { agent_id: 'private-b', failure_reason: 'timeout', task_count: 10 },
  ];

  it('rewrites unresolvable ids to the sentinel and leaves resolvable ones alone', () => {
    const result = anonymizeUnresolvedAgentRows(rows, new Set(['visible']));

    expect(result.map((r) => r.agent_id)).toEqual([
      'visible',
      'visible',
      UNRESOLVED_AGENTS_ROW_ID,
      UNRESOLVED_AGENTS_ROW_ID,
      UNRESOLVED_AGENTS_ROW_ID,
    ]);
    expect(result.map((r) => r.task_count)).toEqual([5, 5, 6, 5, 10]);
  });

  it("keeps the bucket's class split honest across merged agents", () => {
    const bucket = aggregateAgentFailures(
      anonymizeUnresolvedAgentRows(rows, new Set(['visible'])),
    ).find((r) => r.agentId === UNRESOLVED_AGENTS_ROW_ID);

    expect(bucket).toMatchObject({ failed: 21, total: 21 });
    expect(bucket?.classes).toMatchObject({ timeout: 15, auth: 6 });
  });

  it('anonymizes everything while the agent list is still loading', () => {
    const result = anonymizeUnresolvedAgentRows(rows, null);

    expect(new Set(result.map((r) => r.agent_id))).toEqual(new Set([UNRESOLVED_AGENTS_ROW_ID]));
  });

  it('returns the input untouched when every agent resolves', () => {
    const known = new Set(['visible', 'private-a', 'private-b']);
    expect(anonymizeUnresolvedAgentRows(rows, known)).toBe(rows);
  });
});
