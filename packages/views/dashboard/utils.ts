import type {
  DashboardUsageDaily,
  DashboardUsageByAgent,
  DashboardAgentRunTime,
  DashboardRunTimeDaily,
  DashboardFailureDaily,
  DashboardFailureByAgent,
} from '@goosar/core/types';
import { FAILURE_CLASSES, failureClassOf, type FailureClass } from '@goosar/core/dashboard';
import {
  addDaysIso,
  estimateCost,
  estimateCostBreakdown,
  formatShortDate,
  todayIso,
  weekStartIso,
  type DailyTokenData,
} from '../runtimes/utils';
import type {
  DailyTimeData,
  DailyTasksData,
  WeeklyTimeData,
  WeeklyTasksData,
  DailyErrorsData,
  WeeklyErrorsData,
  FailureBucketTotals,
  FailureClassCounts,
} from '../runtimes/components/charts';

export interface DailyCostStack {
  date: string;
  label: string;
  input: number;
  output: number;
  cacheWrite: number;
  total: number;
}

function formatDateLabel(d: string): string {
  const date = new Date(d + 'T00:00:00');
  return `${date.getMonth() + 1}/${date.getDate()}`;
}

export function aggregateDailyCost(usage: DashboardUsageDaily[]): DailyCostStack[] {
  const map = new Map<string, { input: number; output: number; cacheWrite: number }>();
  for (const u of usage) {
    const b = estimateCostBreakdown(u);
    const entry = map.get(u.date) ?? { input: 0, output: 0, cacheWrite: 0 };
    entry.input += b.input;
    entry.output += b.output;
    entry.cacheWrite += b.cacheWrite;
    map.set(u.date, entry);
  }
  const round = (n: number) => Math.round(n * 100) / 100;
  return Array.from(map.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([date, s]) => {
      const input = round(s.input);
      const output = round(s.output);
      const cacheWrite = round(s.cacheWrite);
      return {
        date,
        label: formatDateLabel(date),
        input,
        output,
        cacheWrite,
        total: round(input + output + cacheWrite),
      };
    });
}

export function aggregateDailyTokens(usage: DashboardUsageDaily[]): DailyTokenData[] {
  const map = new Map<
    string,
    { input: number; output: number; cacheRead: number; cacheWrite: number }
  >();
  for (const u of usage) {
    const entry = map.get(u.date) ?? {
      input: 0,
      output: 0,
      cacheRead: 0,
      cacheWrite: 0,
    };
    entry.input += u.input_tokens;
    entry.output += u.output_tokens;
    entry.cacheRead += u.cache_read_tokens;
    entry.cacheWrite += u.cache_write_tokens;
    map.set(u.date, entry);
  }
  return Array.from(map.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([date, t]) => ({
      date,
      label: formatDateLabel(date),
      input: t.input,
      output: t.output,
      cacheRead: t.cacheRead,
      cacheWrite: t.cacheWrite,
    }));
}

export interface DashboardTokenTotals {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  cost: number;
  taskCount: number;
}

export function computeDailyTotals(usage: DashboardUsageDaily[]): DashboardTokenTotals {
  return usage.reduce<DashboardTokenTotals>(
    (acc, u) => ({
      input: acc.input + u.input_tokens,
      output: acc.output + u.output_tokens,
      cacheRead: acc.cacheRead + u.cache_read_tokens,
      cacheWrite: acc.cacheWrite + u.cache_write_tokens,
      cost: acc.cost + estimateCost(u),
      taskCount: acc.taskCount + u.task_count,
    }),
    { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, cost: 0, taskCount: 0 },
  );
}

export interface AgentCostRow {
  agentId: string;
  tokens: number;
  cost: number;
  taskCount: number;
}

export function aggregateAgentTokens(rows: DashboardUsageByAgent[]): AgentCostRow[] {
  const map = new Map<string, AgentCostRow>();
  for (const r of rows) {
    const entry = map.get(r.agent_id) ?? {
      agentId: r.agent_id,
      tokens: 0,
      cost: 0,
      taskCount: 0,
    };
    entry.tokens += r.input_tokens + r.output_tokens + r.cache_read_tokens + r.cache_write_tokens;
    entry.cost += estimateCost(r);
    entry.taskCount += r.task_count;
    map.set(r.agent_id, entry);
  }
  return Array.from(map.values()).toSorted((a, b) => b.cost - a.cost);
}

export interface AgentDashboardRow {
  agentId: string;
  tokens: number;
  cost: number;
  seconds: number;
  taskCount: number;
}

export function mergeAgentDashboardRows(
  tokenRows: AgentCostRow[],
  runTimeRows: DashboardAgentRunTime[],
): AgentDashboardRow[] {
  const runTimeByAgent = new Map(runTimeRows.map((r) => [r.agent_id, r] as const));
  const merged = new Map<string, AgentDashboardRow>();
  for (const r of tokenRows) {
    const rt = runTimeByAgent.get(r.agentId);
    merged.set(r.agentId, {
      agentId: r.agentId,
      tokens: r.tokens,
      cost: r.cost,
      seconds: rt?.total_seconds ?? 0,
      taskCount: rt ? rt.task_count : r.taskCount,
    });
  }
  for (const r of runTimeRows) {
    if (merged.has(r.agent_id)) continue;
    merged.set(r.agent_id, {
      agentId: r.agent_id,
      tokens: 0,
      cost: 0,
      seconds: r.total_seconds,
      taskCount: r.task_count,
    });
  }
  return Array.from(merged.values()).toSorted((a, b) => {
    if (b.cost !== a.cost) return b.cost - a.cost;
    return b.seconds - a.seconds;
  });
}

export const DELETED_AGENTS_ROW_ID = '__deleted_agents__';

export function bucketUnknownAgentRows(
  rows: AgentDashboardRow[],
  knownAgentIds: ReadonlySet<string> | null,
): AgentDashboardRow[] {
  if (!knownAgentIds) return rows;
  const known: AgentDashboardRow[] = [];
  const bucket: AgentDashboardRow = {
    agentId: DELETED_AGENTS_ROW_ID,
    tokens: 0,
    cost: 0,
    seconds: 0,
    taskCount: 0,
  };
  let hasDeleted = false;
  for (const r of rows) {
    if (knownAgentIds.has(r.agentId)) {
      known.push(r);
      continue;
    }
    hasDeleted = true;
    bucket.tokens += r.tokens;
    bucket.cost += r.cost;
  }
  return hasDeleted ? [...known, bucket] : known;
}

interface WeekShell {
  weekStart: string;
  weekEnd: string;
  label: string;
  rangeLabel: string;
  partial: boolean;
  daysCovered: number;
}

function buildWeekShells(tz: string, weekCount: number): WeekShell[] {
  const count = Math.max(1, Math.floor(weekCount));
  const today = todayIso(tz);
  const currentWeekStart = weekStartIso(today);
  const firstWeekStart = addDaysIso(currentWeekStart, -(count - 1) * 7);
  const shells: WeekShell[] = [];
  for (let i = 0; i < count; i++) {
    const weekStart = addDaysIso(firstWeekStart, i * 7);
    const weekEnd = addDaysIso(weekStart, 6);
    const partial = today < weekEnd;
    const clampedToday = today < weekStart ? weekStart : today < weekEnd ? today : weekEnd;
    const elapsed = Math.min(7, Math.max(1, diffDaysIso(weekStart, clampedToday) + 1));
    shells.push({
      weekStart,
      weekEnd,
      label: formatShortDate(weekStart),
      rangeLabel: `${formatShortDate(weekStart)} – ${formatShortDate(weekEnd)}`,
      partial,
      daysCovered: partial ? elapsed : 7,
    });
  }
  return shells;
}

function diffDaysIso(from: string, to: string): number {
  const [y1, m1, d1] = from.split('-').map(Number);
  const [y2, m2, d2] = to.split('-').map(Number);
  const a = Date.UTC(y1 ?? 1970, (m1 ?? 1) - 1, d1 ?? 1);
  const b = Date.UTC(y2 ?? 1970, (m2 ?? 1) - 1, d2 ?? 1);
  return Math.round((b - a) / 86_400_000);
}

export function aggregateWeeklyTime(
  rows: DashboardRunTimeDaily[],
  tz: string,
  weekCount: number,
): WeeklyTimeData[] {
  const shells = buildWeekShells(tz, weekCount);
  const totals = new Map<string, number>();
  for (const shell of shells) totals.set(shell.weekStart, 0);
  for (const r of rows) {
    const wkStart = weekStartIso(r.date);
    if (!totals.has(wkStart)) continue;
    totals.set(wkStart, (totals.get(wkStart) ?? 0) + r.total_seconds);
  }
  return shells.map((s) => ({ ...s, totalSeconds: totals.get(s.weekStart) ?? 0 }));
}

export function aggregateWeeklyTasks(
  rows: DashboardRunTimeDaily[],
  tz: string,
  weekCount: number,
): WeeklyTasksData[] {
  const shells = buildWeekShells(tz, weekCount);
  const buckets = new Map<string, { completed: number; failed: number }>();
  for (const shell of shells) buckets.set(shell.weekStart, { completed: 0, failed: 0 });
  for (const r of rows) {
    const wkStart = weekStartIso(r.date);
    const bucket = buckets.get(wkStart);
    if (!bucket) continue;
    const failed = r.failed_count;
    const completed = Math.max(0, r.task_count - failed);
    bucket.completed += completed;
    bucket.failed += failed;
  }
  return shells.map((s) => {
    const b = buckets.get(s.weekStart) ?? { completed: 0, failed: 0 };
    return { ...s, completed: b.completed, failed: b.failed };
  });
}

export function aggregateDailyTime(rows: DashboardRunTimeDaily[]): DailyTimeData[] {
  return rows
    .toSorted((a, b) => a.date.localeCompare(b.date))
    .map((r) => ({
      date: r.date,
      label: formatDateLabel(r.date),
      totalSeconds: r.total_seconds,
    }));
}

export function aggregateDailyTasks(rows: DashboardRunTimeDaily[]): DailyTasksData[] {
  return rows
    .toSorted((a, b) => a.date.localeCompare(b.date))
    .map((r) => {
      const failed = r.failed_count;
      const completed = Math.max(0, r.task_count - failed);
      return {
        date: r.date,
        label: formatDateLabel(r.date),
        completed,
        failed,
      };
    });
}

export function formatDuration(seconds: number, lessThanMinuteLabel: string): string {
  if (seconds < 0 || !Number.isFinite(seconds)) return lessThanMinuteLabel;
  if (seconds < 60) {
    if (seconds < 1) return lessThanMinuteLabel;
    return `${Math.round(seconds)}s`;
  }
  const totalMinutes = Math.floor(seconds / 60);
  const hours = Math.floor(totalMinutes / 60);
  const mins = totalMinutes % 60;
  if (hours === 0) {
    const secs = Math.floor(seconds) % 60;
    return secs > 0 ? `${mins}m ${secs}s` : `${mins}m`;
  }
  if (hours >= 24) {
    const days = Math.floor(hours / 24);
    const h = hours % 24;
    return h > 0 ? `${days}d ${h}h` : `${days}d`;
  }
  return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`;
}

function emptyClassCounts(): FailureClassCounts {
  return Object.fromEntries(FAILURE_CLASSES.map((c) => [c, 0])) as FailureClassCounts;
}

function foldFailureRow(
  acc: FailureClassCounts & FailureBucketTotals,
  reason: string,
  count: number,
): void {
  acc.total += count;
  if (reason === '') return;
  acc.failed += count;
  acc[failureClassOf(reason)] += count;
}

export function aggregateDailyErrors(rows: DashboardFailureDaily[]): DailyErrorsData[] {
  const map = new Map<string, FailureClassCounts & FailureBucketTotals>();
  for (const r of rows) {
    let entry = map.get(r.date);
    if (!entry) {
      entry = { ...emptyClassCounts(), failed: 0, total: 0 };
      map.set(r.date, entry);
    }
    foldFailureRow(entry, r.failure_reason, r.task_count);
  }
  return Array.from(map.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([date, counts]) => ({
      ...counts,
      date,
      label: formatDateLabel(date),
    }));
}

export function aggregateWeeklyErrors(
  rows: DashboardFailureDaily[],
  tz: string,
  weekCount: number,
): WeeklyErrorsData[] {
  const shells = buildWeekShells(tz, weekCount);
  const buckets = new Map<string, FailureClassCounts & FailureBucketTotals>();
  for (const shell of shells) {
    buckets.set(shell.weekStart, { ...emptyClassCounts(), failed: 0, total: 0 });
  }
  for (const r of rows) {
    const bucket = buckets.get(weekStartIso(r.date));
    if (!bucket) continue;
    foldFailureRow(bucket, r.failure_reason, r.task_count);
  }
  return shells.map((s) => ({
    ...(buckets.get(s.weekStart) ?? { ...emptyClassCounts(), failed: 0, total: 0 }),
    ...s,
  }));
}

export interface FailureTotals {
  failed: number;
  total: number;
  rate: number;
}

export function computeFailureTotals(
  rows: { failure_reason: string; task_count: number }[],
): FailureTotals {
  let failed = 0;
  let total = 0;
  for (const r of rows) {
    total += r.task_count;
    if (r.failure_reason !== '') failed += r.task_count;
  }
  return { failed, total, rate: total > 0 ? failed / total : 0 };
}

export interface FailureClassRow {
  failureClass: FailureClass;
  count: number;
}

export function aggregateFailureClasses(
  rows: { failure_reason: string; task_count: number }[],
): FailureClassRow[] {
  const counts = emptyClassCounts();
  for (const r of rows) {
    if (r.failure_reason === '') continue;
    counts[failureClassOf(r.failure_reason)] += r.task_count;
  }
  return FAILURE_CLASSES.map((failureClass) => ({
    failureClass,
    count: counts[failureClass],
  }))
    .filter((r) => r.count > 0)
    .toSorted((a, b) => b.count - a.count);
}

export interface FailureReasonRow {
  reason: string;
  failureClass: FailureClass;
  count: number;
}

export function aggregateFailureReasons(
  rows: { failure_reason: string; task_count: number }[],
): FailureReasonRow[] {
  const counts = new Map<string, number>();
  for (const r of rows) {
    if (r.failure_reason === '') continue;
    counts.set(r.failure_reason, (counts.get(r.failure_reason) ?? 0) + r.task_count);
  }
  return Array.from(counts.entries())
    .map(([reason, count]) => ({
      reason,
      failureClass: failureClassOf(reason),
      count,
    }))
    .toSorted((a, b) => b.count - a.count || a.reason.localeCompare(b.reason));
}

export const UNRESOLVED_AGENTS_ROW_ID = '__unresolved_agents__';

export interface AgentFailureRow {
  agentId: string;
  failed: number;
  total: number;
  rate: number;
  classes: FailureClassCounts;
}

export function aggregateAgentFailures(rows: DashboardFailureByAgent[]): AgentFailureRow[] {
  const map = new Map<string, { failed: number; total: number; classes: FailureClassCounts }>();
  for (const r of rows) {
    let entry = map.get(r.agent_id);
    if (!entry) {
      entry = { failed: 0, total: 0, classes: emptyClassCounts() };
      map.set(r.agent_id, entry);
    }
    entry.total += r.task_count;
    if (r.failure_reason === '') continue;
    entry.failed += r.task_count;
    entry.classes[failureClassOf(r.failure_reason)] += r.task_count;
  }
  return sortAgentFailures(
    Array.from(map.entries())
      .filter(([, v]) => v.failed > 0)
      .map(([agentId, v]) => ({
        agentId,
        failed: v.failed,
        total: v.total,
        rate: v.total > 0 ? v.failed / v.total : 0,
        classes: v.classes,
      })),
    'failed',
  );
}

export type OffenderSort = 'failed' | 'rate';

export const OFFENDER_METRIC: Record<OffenderSort, (r: AgentFailureRow) => number> = {
  failed: (r) => r.failed,
  rate: (r) => r.rate,
};

export const MIN_RATE_SAMPLE = 10;

export function hasRateSample(row: AgentFailureRow): boolean {
  return row.total >= MIN_RATE_SAMPLE;
}

export function sortAgentFailures(
  rows: AgentFailureRow[],
  sortBy: OffenderSort,
): AgentFailureRow[] {
  if (sortBy === 'failed') {
    return rows.toSorted((a, b) => b.failed - a.failed || b.rate - a.rate);
  }
  const sample = (r: AgentFailureRow) => (hasRateSample(r) ? 0 : 1);
  return rows.toSorted((a, b) => sample(a) - sample(b) || b.rate - a.rate || b.failed - a.failed);
}

export function anonymizeUnresolvedAgentRows(
  rows: DashboardFailureByAgent[],
  knownAgentIds: ReadonlySet<string> | null,
): DashboardFailureByAgent[] {
  if (!rows.some((r) => !knownAgentIds?.has(r.agent_id))) return rows;
  return rows.map((r) =>
    knownAgentIds?.has(r.agent_id) ? r : { ...r, agent_id: UNRESOLVED_AGENTS_ROW_ID },
  );
}
