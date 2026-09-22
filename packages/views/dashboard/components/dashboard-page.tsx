'use client';

import { useMemo, useState } from 'react';
import { BarChart3, FolderKanban, Trash2 } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { Skeleton } from '@goosar/ui/components/ui/skeleton';
import {
  CompactNumberFlow,
  CurrencyNumberFlow,
  NumberFlow,
  NumberFlowGroup,
} from '@goosar/ui/components/ui/number-flow';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@goosar/ui/components/ui/select';
import { useWorkspaceId } from '@goosar/core/hooks';
import type { Agent } from '@goosar/core/types';
import { agentListOptions } from '@goosar/core/workspace/queries';
import { projectListOptions } from '@goosar/core/projects/queries';
import {
  dashboardUsageDailyOptions,
  dashboardUsageByAgentOptions,
  dashboardAgentRunTimeOptions,
  dashboardRunTimeDailyOptions,
  dashboardFailuresDailyOptions,
  dashboardFailuresByAgentOptions,
  FAILURE_CLASSES,
  type FailureClass,
} from '@goosar/core/dashboard';
import { useWorkspacePaths } from '@goosar/core/paths';
import { useCustomPricingStore } from '@goosar/core/runtimes/custom-pricing-store';
import { useViewingTimezone } from '../../common/use-viewing-timezone';
import { PageHeader } from '../../layout/page-header';
import { KpiCard } from '../../runtimes/components/shared';
import {
  DailyCostChart,
  DailyTokensChart,
  DailyTimeChart,
  DailyTasksChart,
  DailyErrorsChart,
  WeeklyCostChart,
  WeeklyTokensChart,
  WeeklyTimeChart,
  WeeklyTasksChart,
  WeeklyErrorsChart,
  FAILURE_CLASS_COLOR,
  formatRate,
} from '../../runtimes/components/charts';
import { ProjectIcon } from '../../projects/components/project-icon';
import { ActorAvatar } from '../../common/actor-avatar';
import { AppLink } from '../../navigation';
import { addDaysIso, aggregateByWeek, formatTokens, todayIso } from '../../runtimes/utils';
import { useT } from '../../i18n';
import {
  aggregateAgentFailures,
  aggregateAgentTokens,
  aggregateDailyCost,
  aggregateDailyErrors,
  aggregateDailyTasks,
  aggregateDailyTime,
  aggregateDailyTokens,
  aggregateFailureClasses,
  aggregateFailureReasons,
  aggregateWeeklyErrors,
  aggregateWeeklyTasks,
  aggregateWeeklyTime,
  bucketUnknownAgentRows,
  anonymizeUnresolvedAgentRows,
  computeDailyTotals,
  computeFailureTotals,
  DELETED_AGENTS_ROW_ID,
  formatDuration,
  hasRateSample,
  mergeAgentDashboardRows,
  MIN_RATE_SAMPLE,
  OFFENDER_METRIC,
  sortAgentFailures,
  type AgentDashboardRow,
  type AgentFailureRow,
  type FailureClassRow,
  type FailureReasonRow,
  type FailureTotals,
  type OffenderSort,
} from '../utils';

const TIME_RANGES = [
  { label: '1d', days: 1, dims: ['daily'] as const },
  { label: '7d', days: 7, dims: ['daily'] as const },
  { label: '30d', days: 30, dims: ['daily', 'weekly'] as const },
  { label: '90d', days: 90, dims: ['daily', 'weekly'] as const },
  { label: '180d', days: 180, dims: ['weekly'] as const },
] as const;
type TimeRange = (typeof TIME_RANGES)[number]['days'];
type Dim = 'daily' | 'weekly';

const DEFAULT_DAYS_BY_DIM: Record<Dim, TimeRange> = {
  daily: 30,
  weekly: 90,
};

function rangesForDim(dim: Dim) {
  return TIME_RANGES.filter((r) => (r.dims as readonly string[]).includes(dim));
}

const ALL_PROJECTS = '__all__';

const EMPTY_DAILY: import('@goosar/core/types').DashboardUsageDaily[] = [];
const EMPTY_BY_AGENT: import('@goosar/core/types').DashboardUsageByAgent[] = [];
const EMPTY_RUNTIME: import('@goosar/core/types').DashboardAgentRunTime[] = [];
const EMPTY_RUNTIME_DAILY: import('@goosar/core/types').DashboardRunTimeDaily[] = [];
const EMPTY_FAILURE_DAILY: import('@goosar/core/types').DashboardFailureDaily[] = [];
const EMPTY_FAILURE_BY_AGENT: import('@goosar/core/types').DashboardFailureByAgent[] = [];
const EMPTY_AGENTS: Agent[] = [];

function Segmented<T extends string | number>({
  value,
  onChange,
  options,
  label,
}: {
  value: T;
  onChange: (v: T) => void;
  options: readonly { label: string; value: T }[];
  label: string;
}) {
  return (
    <div
      role="group"
      aria-label={label}
      className="inline-flex items-center gap-0.5 rounded-md bg-muted p-0.5"
    >
      {options.map((o) => (
        <button
          key={String(o.value)}
          type="button"
          aria-pressed={o.value === value}
          onClick={() => onChange(o.value)}
          className={`rounded-sm px-2.5 py-1 text-xs font-medium transition-colors ${
            o.value === value
              ? 'bg-background text-foreground shadow-sm'
              : 'text-muted-foreground hover:text-foreground'
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

function DurationNumberFlow({
  seconds,
  lessThanMinuteLabel,
  locales,
}: {
  seconds: number;
  lessThanMinuteLabel: string;
  locales?: Intl.LocalesArgument;
}) {
  const label = formatDuration(seconds, lessThanMinuteLabel);
  const parts = Array.from(label.matchAll(/(\d+)([a-z]+)/gi), (match) => ({
    value: Number(match[1]),
    unit: match[2] ?? '',
  }));

  if (parts.length === 0) return label;

  return (
    <>
      <span className="sr-only">{label}</span>
      <NumberFlowGroup>
        <span aria-hidden className="inline-flex items-baseline gap-1">
          {parts.map((part) => (
            <NumberFlow
              key={part.unit}
              value={part.value}
              locales={locales}
              suffix={part.unit}
              format={{ maximumFractionDigits: 0, useGrouping: false }}
            />
          ))}
        </span>
      </NumberFlowGroup>
    </>
  );
}

export function DashboardPage() {
  const { t, i18n } = useT('usage');
  const wsId = useWorkspaceId();
  const viewTZ = useViewingTimezone();
  const locales = i18n.resolvedLanguage ?? i18n.language;
  const [dim, setDim] = useState<Dim>('daily');
  const [days, setDays] = useState<TimeRange>(30);
  const [projectValue, setProjectValue] = useState<string>(ALL_PROJECTS);

  const allowedRanges = rangesForDim(dim);
  const handleDimChange = (next: Dim) => {
    setDim(next);
    const stillAllowed = (rangesForDim(next) as readonly { days: number }[]).some(
      (r) => r.days === days,
    );
    if (!stillAllowed) setDays(DEFAULT_DAYS_BY_DIM[next]);
  };

  useCustomPricingStore((s) => s.pricings);

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const agentsQuery = useQuery(agentListOptions(wsId));
  const agents = agentsQuery.data ?? EMPTY_AGENTS;

  const projectId = useMemo(() => {
    if (projectValue === ALL_PROJECTS) return null;
    return projects.some((p) => p.id === projectValue) ? projectValue : null;
  }, [projectValue, projects]);

  const weekCount = Math.max(1, Math.ceil(days / 7));
  const chartFetchDays = dim === 'weekly' ? weekCount * 7 : days;

  const dailyQuery = useQuery(dashboardUsageDailyOptions(wsId, chartFetchDays, projectId, viewTZ));
  const byAgentQuery = useQuery(dashboardUsageByAgentOptions(wsId, days, projectId, viewTZ));
  const runTimeQuery = useQuery(dashboardAgentRunTimeOptions(wsId, days, projectId, viewTZ));
  const runTimeDailyQuery = useQuery(
    dashboardRunTimeDailyOptions(wsId, chartFetchDays, projectId, viewTZ),
  );
  const failuresDailyQuery = useQuery(
    dashboardFailuresDailyOptions(wsId, chartFetchDays, projectId, viewTZ),
  );
  const failuresByAgentQuery = useQuery(
    dashboardFailuresByAgentOptions(wsId, days, projectId, viewTZ),
  );

  const dailyUsage = dailyQuery.data ?? EMPTY_DAILY;
  const byAgentUsage = byAgentQuery.data ?? EMPTY_BY_AGENT;
  const runTimeRows = runTimeQuery.data ?? EMPTY_RUNTIME;
  const runTimeDailyRows = runTimeDailyQuery.data ?? EMPTY_RUNTIME_DAILY;
  const failureDailyRows = failuresDailyQuery.data ?? EMPTY_FAILURE_DAILY;
  const failureByAgentRows = failuresByAgentQuery.data ?? EMPTY_FAILURE_BY_AGENT;

  const dailyCutoffIso = useMemo(() => addDaysIso(todayIso(viewTZ), -(days - 1)), [days, viewTZ]);
  const dailyUsageInWindow = useMemo(
    () => dailyUsage.filter((u) => u.date >= dailyCutoffIso),
    [dailyUsage, dailyCutoffIso],
  );
  const runTimeDailyInWindow = useMemo(
    () => runTimeDailyRows.filter((r) => r.date >= dailyCutoffIso),
    [runTimeDailyRows, dailyCutoffIso],
  );
  const failureDailyInWindow = useMemo(
    () => failureDailyRows.filter((r) => r.date >= dailyCutoffIso),
    [failureDailyRows, dailyCutoffIso],
  );

  const isLoading =
    dailyQuery.isLoading ||
    byAgentQuery.isLoading ||
    runTimeQuery.isLoading ||
    runTimeDailyQuery.isLoading ||
    failuresDailyQuery.isLoading ||
    failuresByAgentQuery.isLoading;

  const hasNoData =
    !isLoading &&
    dailyUsage.length === 0 &&
    byAgentUsage.length === 0 &&
    runTimeRows.length === 0 &&
    runTimeDailyRows.length === 0 &&
    failureDailyRows.length === 0 &&
    failureByAgentRows.length === 0;

  const totals = useMemo(() => computeDailyTotals(dailyUsageInWindow), [dailyUsageInWindow]);
  const dailyCost = useMemo(() => aggregateDailyCost(dailyUsageInWindow), [dailyUsageInWindow]);
  const dailyTokens = useMemo(() => aggregateDailyTokens(dailyUsageInWindow), [dailyUsageInWindow]);
  const dailyTime = useMemo(() => aggregateDailyTime(runTimeDailyInWindow), [runTimeDailyInWindow]);
  const dailyTasks = useMemo(
    () => aggregateDailyTasks(runTimeDailyInWindow),
    [runTimeDailyInWindow],
  );
  const dailyErrors = useMemo(
    () => aggregateDailyErrors(failureDailyInWindow),
    [failureDailyInWindow],
  );

  const failureTotals = useMemo(
    () => computeFailureTotals(failureDailyInWindow),
    [failureDailyInWindow],
  );
  const failureClassRows = useMemo(
    () => aggregateFailureClasses(failureDailyInWindow),
    [failureDailyInWindow],
  );
  const failureReasonRows = useMemo(
    () => aggregateFailureReasons(failureDailyInWindow),
    [failureDailyInWindow],
  );
  const knownAgentIds = useMemo(
    () => (agentsQuery.isSuccess ? new Set(agents.map((a) => a.id)) : null),
    [agentsQuery.isSuccess, agents],
  );

  const agentFailureRows = useMemo(
    () => aggregateAgentFailures(anonymizeUnresolvedAgentRows(failureByAgentRows, knownAgentIds)),
    [failureByAgentRows, knownAgentIds],
  );

  const weekly = useMemo(
    () => aggregateByWeek(dailyUsage, viewTZ, weekCount),
    [dailyUsage, viewTZ, weekCount],
  );
  const weeklyCost = weekly.weeklyCostStack;
  const weeklyTokens = weekly.weeklyTokens;
  const weeklyTime = useMemo(
    () => aggregateWeeklyTime(runTimeDailyRows, viewTZ, weekCount),
    [runTimeDailyRows, viewTZ, weekCount],
  );
  const weeklyTasks = useMemo(
    () => aggregateWeeklyTasks(runTimeDailyRows, viewTZ, weekCount),
    [runTimeDailyRows, viewTZ, weekCount],
  );
  const weeklyErrors = useMemo(
    () => aggregateWeeklyErrors(failureDailyRows, viewTZ, weekCount),
    [failureDailyRows, viewTZ, weekCount],
  );
  const agentTokenRows = useMemo(() => aggregateAgentTokens(byAgentUsage), [byAgentUsage]);

  const runTimeTotals = useMemo(() => {
    let totalSeconds = 0;
    let taskCount = 0;
    let failedCount = 0;
    for (const r of runTimeRows) {
      totalSeconds += r.total_seconds;
      taskCount += r.task_count;
      failedCount += r.failed_count;
    }
    return { totalSeconds, taskCount, failedCount };
  }, [runTimeRows]);

  const agentRows = useMemo(
    () => mergeAgentDashboardRows(agentTokenRows, runTimeRows),
    [agentTokenRows, runTimeRows],
  );

  const visibleAgentRows = useMemo(
    () => bucketUnknownAgentRows(agentRows, knownAgentIds),
    [agentRows, knownAgentIds],
  );
  const deletedAgentCount = useMemo(
    () => (knownAgentIds ? agentRows.filter((r) => !knownAgentIds.has(r.agentId)).length : 0),
    [agentRows, knownAgentIds],
  );

  return (
    <div className="flex h-full flex-col">
      {/* h-auto + min-h-12 + flex-wrap: the toolbar (project filter,
          dimension switch, range switch) wraps on narrow viewports so every
          control stays reachable. Wider viewports still render the original
          single row. */}
      <PageHeader className="h-auto min-h-12 flex-wrap justify-between gap-y-1.5 px-5 py-1.5 sm:py-0">
        <div className="flex min-w-0 items-center gap-2">
          <BarChart3 className="h-4 w-4 shrink-0 text-muted-foreground" />
          <h1 className="truncate text-sm font-medium">{t(($) => $.title)}</h1>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <ProjectFilter projects={projects} value={projectValue} onChange={setProjectValue} />
          <Segmented
            label={t(($) => $.dim.label)}
            value={dim}
            onChange={handleDimChange}
            options={[
              { label: t(($) => $.dim.daily), value: 'daily' as const },
              { label: t(($) => $.dim.weekly), value: 'weekly' as const },
            ]}
          />
          <Segmented
            label={t(($) => $.filter.period_label)}
            value={days}
            onChange={setDays}
            options={allowedRanges.map((r) => ({ label: r.label, value: r.days }))}
          />
        </div>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-6xl space-y-5 p-6">
          <p className="text-xs text-muted-foreground">{t(($) => $.subtitle)}</p>

          {isLoading ? (
            <DashboardSkeleton />
          ) : hasNoData ? (
            <DashboardEmpty />
          ) : (
            <>
              {/* KPI row — same 3-divide-x card grid the runtime usage
                  section uses, expanded to four tiles. */}
              <div className="grid grid-cols-1 divide-y rounded-lg border bg-card sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-4">
                <KpiCard
                  label={t(($) => $.kpi.cost_label, { days })}
                  value={<CurrencyNumberFlow value={totals.cost} locales={locales} />}
                />
                <KpiCard
                  label={t(($) => $.kpi.tokens_label, { days })}
                  value={
                    <CompactNumberFlow
                      value={totals.input + totals.output + totals.cacheRead + totals.cacheWrite}
                      locales={locales}
                    />
                  }
                  hint={t(($) => $.kpi.tokens_hint, {
                    input: formatTokens(totals.input),
                    output: formatTokens(totals.output),
                  })}
                />
                <KpiCard
                  label={t(($) => $.kpi.run_time_label, { days })}
                  value={
                    <DurationNumberFlow
                      seconds={runTimeTotals.totalSeconds}
                      lessThanMinuteLabel={t(($) => $.duration.less_than_minute)}
                      locales={locales}
                    />
                  }
                  hint={t(($) => $.kpi.run_time_hint, {
                    tasks: runTimeTotals.taskCount,
                  })}
                />
                <KpiCard
                  label={t(($) => $.kpi.tasks_label, { days })}
                  value={
                    <NumberFlow
                      value={runTimeTotals.taskCount}
                      locales={locales}
                      format={{ maximumFractionDigits: 0 }}
                      aria-label={String(runTimeTotals.taskCount)}
                    />
                  }
                  hint={t(($) => $.kpi.tasks_hint, {
                    failed: runTimeTotals.failedCount,
                  })}
                  accent={runTimeTotals.failedCount > 0 ? 'default' : 'default'}
                />
              </div>

              {/* Trend chart — toggle picks Tokens / Cost / Time / Tasks /
                  Errors and the parent's dim selector decides whether the
                  bars are per-day or per-calendar-week. All five metrics
                  share the same x-axis so the user can mentally overlay them
                  by flipping the toggle. */}
              <TrendBlock
                dim={dim}
                dailyCost={dailyCost}
                dailyTokens={dailyTokens}
                dailyTime={dailyTime}
                dailyTasks={dailyTasks}
                dailyErrors={dailyErrors}
                weeklyCost={weeklyCost}
                weeklyTokens={weeklyTokens}
                weeklyTime={weeklyTime}
                weeklyTasks={weeklyTasks}
                weeklyErrors={weeklyErrors}
                lessThanMinuteLabel={t(($) => $.duration.less_than_minute)}
              />

              {/* Per-agent leaderboard — user picks the ranking metric;
                  the progress bar and column emphasis follow the metric. */}
              <Leaderboard
                rows={visibleAgentRows}
                agents={agents}
                deletedAgentCount={deletedAgentCount}
                lessThanMinuteLabel={t(($) => $.duration.less_than_minute)}
              />

              {/* Failure breakdown — what broke and who it broke for. Rendered
                  unconditionally (not only when failures exist) so "no failed
                  runs" is an answer the page gives rather than an absence the
                  reader has to infer. */}
              <ErrorsBreakdown
                totals={failureTotals}
                classRows={failureClassRows}
                reasonRows={failureReasonRows}
                agentRows={agentFailureRows}
                agents={agents}
              />
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function ProjectFilter({
  projects,
  value,
  onChange,
}: {
  projects: { id: string; title: string; icon: string | null }[];
  value: string;
  onChange: (v: string) => void;
}) {
  const { t } = useT('usage');
  const allLabel = t(($) => $.filter.all_projects);
  const selected = projects.find((p) => p.id === value);
  const selectedTitle = value === ALL_PROJECTS ? allLabel : (selected?.title ?? allLabel);
  const projectItems = [
    { value: ALL_PROJECTS, label: allLabel },
    ...projects.map((project) => ({ value: project.id, label: project.title })),
  ];

  return (
    <Select items={projectItems} value={value} onValueChange={(v) => onChange(v ?? ALL_PROJECTS)}>
      <SelectTrigger size="sm" className="min-w-[180px]">
        <SelectValue>
          {() => (
            <>
              {selected ? (
                <ProjectIcon project={selected} size="sm" />
              ) : (
                <FolderKanban className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              )}
              <span className="truncate">{selectedTitle}</span>
            </>
          )}
        </SelectValue>
      </SelectTrigger>
      {/* alignItemWithTrigger=false: the default aligns the *selected* item
          to the trigger, which pushes "All projects" above the trigger and
          clips it off-screen when the usage header sits at the top of the
          viewport. Anchor the dropdown to the bottom of the trigger so
          every entry stays reachable.
          max-h-72: cap the dropdown so a long project list scrolls instead
          of stretching to the bottom of the window. */}
      <SelectContent align="start" alignItemWithTrigger={false} className="max-h-72">
        <SelectItem value={ALL_PROJECTS}>
          <FolderKanban className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">{allLabel}</span>
        </SelectItem>
        {projects.map((p) => (
          <SelectItem key={p.id} value={p.id}>
            <ProjectIcon project={p} size="sm" />
            <span className="truncate">{p.title}</span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

type DailyMetric = 'tokens' | 'cost' | 'time' | 'tasks' | 'errors';

function TrendBlock({
  dim,
  dailyCost,
  dailyTokens,
  dailyTime,
  dailyTasks,
  dailyErrors,
  weeklyCost,
  weeklyTokens,
  weeklyTime,
  weeklyTasks,
  weeklyErrors,
  lessThanMinuteLabel,
}: {
  dim: Dim;
  dailyCost: ReturnType<typeof aggregateDailyCost>;
  dailyTokens: ReturnType<typeof aggregateDailyTokens>;
  dailyTime: ReturnType<typeof aggregateDailyTime>;
  dailyTasks: ReturnType<typeof aggregateDailyTasks>;
  dailyErrors: ReturnType<typeof aggregateDailyErrors>;
  weeklyCost: ReturnType<typeof aggregateByWeek>['weeklyCostStack'];
  weeklyTokens: ReturnType<typeof aggregateByWeek>['weeklyTokens'];
  weeklyTime: ReturnType<typeof aggregateWeeklyTime>;
  weeklyTasks: ReturnType<typeof aggregateWeeklyTasks>;
  weeklyErrors: ReturnType<typeof aggregateWeeklyErrors>;
  lessThanMinuteLabel: string;
}) {
  const { t } = useT('usage');
  const [metric, setMetric] = useState<DailyMetric>('tokens');

  const costData = dim === 'weekly' ? weeklyCost : dailyCost;
  const tokensData = dim === 'weekly' ? weeklyTokens : dailyTokens;
  const timeData = dim === 'weekly' ? weeklyTime : dailyTime;
  const tasksData = dim === 'weekly' ? weeklyTasks : dailyTasks;
  const errorsData = dim === 'weekly' ? weeklyErrors : dailyErrors;

  const totalCost = costData.reduce((sum, d) => sum + d.total, 0);
  const totalTokens = tokensData.reduce(
    (sum, d) => sum + d.input + d.output + d.cacheRead + d.cacheWrite,
    0,
  );
  const totalSeconds = timeData.reduce((sum, d) => sum + d.totalSeconds, 0);
  const totalTasks = tasksData.reduce((sum, d) => sum + d.completed + d.failed, 0);
  const totalFailed = errorsData.reduce((sum, d) => sum + d.failed, 0);
  const isEmpty =
    metric === 'cost'
      ? totalCost === 0
      : metric === 'tokens'
        ? totalTokens === 0
        : metric === 'time'
          ? totalSeconds === 0
          : metric === 'tasks'
            ? totalTasks === 0
            : totalFailed === 0;

  const title =
    dim === 'weekly'
      ? metric === 'cost'
        ? t(($) => $.weekly.title_cost)
        : metric === 'tokens'
          ? t(($) => $.weekly.title_tokens)
          : metric === 'time'
            ? t(($) => $.weekly.title_time)
            : metric === 'tasks'
              ? t(($) => $.weekly.title_tasks)
              : t(($) => $.weekly.title_errors)
      : metric === 'cost'
        ? t(($) => $.daily.title_cost)
        : metric === 'tokens'
          ? t(($) => $.daily.title_tokens)
          : metric === 'time'
            ? t(($) => $.daily.title_time)
            : metric === 'tasks'
              ? t(($) => $.daily.title_tasks)
              : t(($) => $.daily.title_errors);

  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <h4 className="text-sm font-semibold">{title}</h4>
        <Segmented
          label={t(($) => $.daily.metric_label)}
          value={metric}
          onChange={setMetric}
          options={[
            { label: t(($) => $.daily.metric_tokens), value: 'tokens' as const },
            { label: t(($) => $.daily.metric_cost), value: 'cost' as const },
            { label: t(($) => $.daily.metric_time), value: 'time' as const },
            { label: t(($) => $.daily.metric_tasks), value: 'tasks' as const },
            { label: t(($) => $.daily.metric_errors), value: 'errors' as const },
          ]}
        />
      </div>
      <div className="min-h-[240px]">
        {isEmpty ? (
          <div className="flex aspect-[3/1] flex-col items-center justify-center gap-2 rounded-md border border-dashed bg-muted/20 p-6 text-center">
            <BarChart3 className="h-5 w-5 text-muted-foreground/50" />
            <p className="text-xs text-muted-foreground">
              {metric === 'errors' ? t(($) => $.errors.no_data) : t(($) => $.daily.no_data)}
            </p>
          </div>
        ) : dim === 'weekly' ? (
          metric === 'cost' ? (
            <WeeklyCostChart data={weeklyCost} />
          ) : metric === 'tokens' ? (
            <WeeklyTokensChart data={weeklyTokens} />
          ) : metric === 'time' ? (
            <WeeklyTimeChart
              data={weeklyTime}
              formatY={(s) => formatDuration(s, lessThanMinuteLabel)}
              formatTooltip={(s) => formatDuration(s, lessThanMinuteLabel)}
            />
          ) : metric === 'tasks' ? (
            <WeeklyTasksChart data={weeklyTasks} />
          ) : (
            <WeeklyErrorsChart data={weeklyErrors} />
          )
        ) : metric === 'cost' ? (
          <DailyCostChart data={dailyCost} />
        ) : metric === 'tokens' ? (
          <DailyTokensChart data={dailyTokens} />
        ) : metric === 'time' ? (
          <DailyTimeChart
            data={dailyTime}
            formatY={(s) => formatDuration(s, lessThanMinuteLabel)}
            formatTooltip={(s) => formatDuration(s, lessThanMinuteLabel)}
          />
        ) : metric === 'tasks' ? (
          <DailyTasksChart data={dailyTasks} />
        ) : (
          <DailyErrorsChart data={dailyErrors} />
        )}
      </div>
    </div>
  );
}

function useFailureClassLabel(): (c: FailureClass) => string {
  const { t } = useT('usage');
  return (c) => {
    switch (c) {
      case 'auth':
        return t(($) => $.errors.class.auth);
      case 'rate_limit':
        return t(($) => $.errors.class.rate_limit);
      case 'timeout':
        return t(($) => $.errors.class.timeout);
      case 'provider':
        return t(($) => $.errors.class.provider);
      case 'runtime':
        return t(($) => $.errors.class.runtime);
      case 'agent':
        return t(($) => $.errors.class.agent);
      case 'other':
        return t(($) => $.errors.class.other);
    }
  };
}

const TOP_OFFENDER_LIMIT = 8;

const OFFENDER_GRID =
  'grid grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_4rem_4rem_4rem] items-center gap-3';

function ErrorsBreakdown({
  totals,
  classRows,
  reasonRows,
  agentRows,
  agents,
}: {
  totals: FailureTotals;
  classRows: FailureClassRow[];
  reasonRows: FailureReasonRow[];
  agentRows: AgentFailureRow[];
  agents: { id: string; name: string }[];
}) {
  const { t } = useT('usage');
  const classLabel = useFailureClassLabel();
  const [showReasons, setShowReasons] = useState(false);
  const [showAllAgents, setShowAllAgents] = useState(false);
  const [sortBy, setSortBy] = useState<OffenderSort>('failed');

  const sortOptions = useMemo(
    () => [
      { value: 'failed' as const, label: t(($) => $.errors.sort_failed) },
      { value: 'rate' as const, label: t(($) => $.errors.sort_rate) },
    ],
    [t],
  );

  const sortedAgents = useMemo(() => sortAgentFailures(agentRows, sortBy), [agentRows, sortBy]);

  const leader = sortedAgents[0];
  const maxValue = leader ? OFFENDER_METRIC[sortBy](leader) : 0;

  const visibleAgents = showAllAgents ? sortedAgents : sortedAgents.slice(0, TOP_OFFENDER_LIMIT);

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pt-4 pb-3">
        <h4 className="text-sm font-semibold">{t(($) => $.errors.title)}</h4>
        <span className="text-xs text-muted-foreground">
          {totals.failed > 0
            ? t(($) => $.errors.summary, {
                failed: totals.failed,
                total: totals.total,
                rate: formatRate(totals.failed, totals.total),
              })
            : t(($) => $.errors.no_data)}
        </span>
      </div>

      {totals.failed === 0 ? null : (
        <>
          <div className="border-b p-4">
            <div className="mb-2.5 flex items-center justify-between gap-2">
              {/* Spells out its own denominator. The header above quotes a
                  rate over every run (3.3% of 8575); this section splits the
                  failures alone (287). Two percentages one above the other
                  with different denominators read as a contradiction unless
                  each says what it is counting. */}
              <h5 className="text-xs font-medium text-muted-foreground">
                {t(($) => $.errors.mix_title, { failed: totals.failed })}
              </h5>
              <button
                type="button"
                onClick={() => setShowReasons((v) => !v)}
                className="text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
              >
                {showReasons ? t(($) => $.errors.hide_reasons) : t(($) => $.errors.show_reasons)}
              </button>
            </div>
            {showReasons ? (
              <ReasonList rows={reasonRows} />
            ) : (
              <ClassComposition rows={classRows} classLabel={classLabel} />
            )}
          </div>

          <div className="p-4">
            <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
              <h5 className="text-xs font-medium text-muted-foreground">
                {t(($) => $.errors.by_agent)}
              </h5>
              <div className="flex flex-wrap items-center justify-end gap-3">
                <Segmented
                  label={t(($) => $.errors.sort_label)}
                  value={sortBy}
                  onChange={setSortBy}
                  options={sortOptions}
                />
                {sortedAgents.length > TOP_OFFENDER_LIMIT ? (
                  <button
                    type="button"
                    onClick={() => setShowAllAgents((v) => !v)}
                    className="text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
                  >
                    {showAllAgents
                      ? t(($) => $.errors.show_less, {
                          count: TOP_OFFENDER_LIMIT,
                        })
                      : t(($) => $.errors.show_all, {
                          count: sortedAgents.length,
                        })}
                  </button>
                ) : null}
              </div>
            </div>
            {/* Column headers, as on the leaderboard: `4 / 10 · 40%` was one
                unlabelled blob and the reader had to guess which number was
                which. The active metric's column is emphasised so it is
                obvious what the ranking and the bars measure. */}
            {sortedAgents.length > 0 ? (
              <div
                className={`${OFFENDER_GRID} border-b py-2 text-xs font-medium text-muted-foreground`}
              >
                <span>{t(($) => $.errors.header_agent)}</span>
                <span />
                <span className={`text-right ${sortBy === 'failed' ? 'text-foreground' : ''}`}>
                  {t(($) => $.errors.header_failed)}
                </span>
                <span className="text-right">{t(($) => $.errors.header_runs)}</span>
                <span className={`text-right ${sortBy === 'rate' ? 'text-foreground' : ''}`}>
                  {t(($) => $.errors.header_rate)}
                </span>
              </div>
            ) : null}
            <ul aria-label={t(($) => $.errors.by_agent)} className="divide-y">
              {visibleAgents.map((row) => (
                <AgentFailureItem
                  key={row.agentId}
                  row={row}
                  name={agents.find((a) => a.id === row.agentId)?.name ?? null}
                  maxValue={maxValue}
                  sortBy={sortBy}
                  classLabel={classLabel}
                />
              ))}
            </ul>
          </div>
        </>
      )}
    </div>
  );
}

function ClassComposition({
  rows,
  classLabel,
}: {
  rows: FailureClassRow[];
  classLabel: (c: FailureClass) => string;
}) {
  const { t } = useT('usage');
  const total = rows.reduce((sum, r) => sum + r.count, 0);
  if (total === 0) return null;

  return (
    <div className="space-y-2.5">
      {/* Segments are ordered by count desc (the aggregator's order), so the
          bar reads heaviest-first left to right. */}
      <div className="flex h-2 w-full overflow-hidden rounded-full bg-muted">
        {rows.map((row) => (
          <div
            key={row.failureClass}
            className="h-full transition-[width] duration-300 ease-out"
            style={{
              width: `${(row.count / total) * 100}%`,
              backgroundColor: FAILURE_CLASS_COLOR[row.failureClass],
            }}
          />
        ))}
      </div>
      <ul
        aria-label={t(($) => $.errors.mix_label)}
        className="flex flex-wrap items-center gap-x-4 gap-y-1.5"
      >
        {rows.map((row) => (
          <li key={row.failureClass} className="flex items-center gap-1.5">
            <span
              aria-hidden
              className="h-2 w-2 shrink-0 rounded-[2px]"
              style={{ backgroundColor: FAILURE_CLASS_COLOR[row.failureClass] }}
            />
            <span className="text-xs">{classLabel(row.failureClass)}</span>
            <span className="text-xs tabular-nums text-muted-foreground">{row.count}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function ReasonList({ rows }: { rows: FailureReasonRow[] }) {
  const { t } = useT('usage');
  return (
    <ul
      aria-label={t(($) => $.errors.codes_label)}
      className="grid grid-cols-1 gap-x-6 gap-y-1.5 sm:grid-cols-2"
    >
      {rows.map((row) => (
        <li key={row.reason} className="flex items-center justify-between gap-2">
          <span className="flex min-w-0 items-center gap-2">
            <span
              aria-hidden
              className="h-2 w-2 shrink-0 rounded-[2px]"
              style={{ backgroundColor: FAILURE_CLASS_COLOR[row.failureClass] }}
            />
            <code className="truncate text-xs text-muted-foreground">{row.reason}</code>
          </span>
          <span className="shrink-0 text-xs tabular-nums">{row.count}</span>
        </li>
      ))}
    </ul>
  );
}

function AgentFailureItem({
  row,
  name,
  maxValue,
  sortBy,
  classLabel,
}: {
  row: AgentFailureRow;
  name: string | null;
  maxValue: number;
  sortBy: OffenderSort;
  classLabel: (c: FailureClass) => string;
}) {
  const { t } = useT('usage');
  const wsPaths = useWorkspacePaths();

  const segments = FAILURE_CLASSES.filter((c) => row.classes[c] > 0);
  const composition = segments.map((c) => `${classLabel(c)} ${row.classes[c]}`).join(' · ');

  const value = OFFENDER_METRIC[sortBy](row);
  const pct = maxValue > 0 ? Math.min(100, (value / maxValue) * 100) : 0;

  const weakSample = !hasRateSample(row);

  const label = (
    <span className={`block truncate text-xs${name ? '' : ' italic text-muted-foreground'}`}>
      {name ?? t(($) => $.errors.other_agents)}
    </span>
  );

  return (
    <li className={`${OFFENDER_GRID} py-2`}>
      {name ? (
        <AppLink
          href={`${wsPaths.agentDetail(row.agentId)}?view=overview`}
          newTabTitle={name}
          className="min-w-0 hover:underline"
        >
          {label}
        </AppLink>
      ) : (
        label
      )}
      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
        <div
          role="img"
          aria-label={composition}
          title={composition}
          className="flex h-full overflow-hidden rounded-full transition-[width] duration-300 ease-out"
          style={{ width: `${pct}%` }}
        >
          {segments.map((c) => (
            <div
              key={c}
              className="h-full"
              style={{
                width: `${(row.classes[c] / row.failed) * 100}%`,
                backgroundColor: FAILURE_CLASS_COLOR[c],
              }}
            />
          ))}
        </div>
      </div>
      <span
        className={`text-right text-xs tabular-nums ${sortBy === 'failed' ? 'font-medium text-foreground' : 'text-muted-foreground'}`}
      >
        {row.failed}
      </span>
      <span className="text-right text-xs tabular-nums text-muted-foreground">{row.total}</span>
      <span
        title={weakSample ? t(($) => $.errors.low_sample, { count: MIN_RATE_SAMPLE }) : undefined}
        className={`text-right text-xs tabular-nums ${
          sortBy === 'rate' && !weakSample ? 'font-medium text-foreground' : 'text-muted-foreground'
        }`}
      >
        {formatRate(row.failed, row.total)}
      </span>
    </li>
  );
}

type LeaderboardSort = 'tokens' | 'cost' | 'time' | 'tasks';

const SORT_METRIC: Record<LeaderboardSort, (r: AgentDashboardRow) => number> = {
  tokens: (r) => r.tokens,
  cost: (r) => r.cost,
  time: (r) => r.seconds,
  tasks: (r) => r.taskCount,
};

const LEADERBOARD_LIMIT = 10;

function Leaderboard({
  rows,
  agents,
  deletedAgentCount,
  lessThanMinuteLabel,
}: {
  rows: AgentDashboardRow[];
  agents: { id: string; name: string }[];
  deletedAgentCount: number;
  lessThanMinuteLabel: string;
}) {
  const { t } = useT('usage');
  const [sortBy, setSortBy] = useState<LeaderboardSort>('tokens');
  const [showAll, setShowAll] = useState(false);

  const sortOptions = useMemo(
    () => [
      { value: 'tokens' as const, label: t(($) => $.leaderboard.header_tokens) },
      { value: 'cost' as const, label: t(($) => $.leaderboard.header_cost) },
      { value: 'time' as const, label: t(($) => $.leaderboard.header_time) },
      { value: 'tasks' as const, label: t(($) => $.leaderboard.header_tasks) },
    ],
    [t],
  );

  const sortedRows = useMemo(() => {
    const metric = SORT_METRIC[sortBy];
    return rows.toSorted((a, b) => metric(b) - metric(a));
  }, [rows, sortBy]);

  const maxValue = useMemo(() => {
    const metric = SORT_METRIC[sortBy];
    return sortedRows.reduce((m, r) => Math.max(m, metric(r)), 0);
  }, [sortedRows, sortBy]);

  const visibleRows = showAll ? sortedRows : sortedRows.slice(0, LEADERBOARD_LIMIT);

  const colClass = (key: LeaderboardSort) =>
    `text-right ${sortBy === key ? 'text-foreground' : 'text-muted-foreground'}`;

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pt-4 pb-3">
        <h4 className="text-sm font-semibold">{t(($) => $.leaderboard.title)}</h4>
        <div className="flex flex-wrap items-center justify-end gap-3">
          <Segmented
            label={t(($) => $.leaderboard.sort_label)}
            value={sortBy}
            onChange={setSortBy}
            options={sortOptions}
          />
          <span className="text-xs text-muted-foreground">
            {deletedAgentCount > 0
              ? t(($) => $.leaderboard.caption_with_deleted, {
                  count: rows.length - 1,
                  deleted: deletedAgentCount,
                })
              : t(($) => $.leaderboard.caption, { count: rows.length })}
          </span>
          {/* The caption right beside this already states how many agents the
              window covers, so the toggle carries a count only when
              collapsing — spelling the total out twice reads as two different
              numbers once the deleted-agents bucket splits the caption. */}
          {sortedRows.length > LEADERBOARD_LIMIT ? (
            <button
              type="button"
              onClick={() => setShowAll((v) => !v)}
              className="text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
            >
              {showAll
                ? t(($) => $.leaderboard.show_less, { count: LEADERBOARD_LIMIT })
                : t(($) => $.leaderboard.show_all)}
            </button>
          ) : null}
        </div>
      </div>
      {sortedRows.length === 0 ? (
        <p className="px-4 py-8 text-center text-xs text-muted-foreground">
          {t(($) => $.leaderboard.no_data)}
        </p>
      ) : (
        <>
          <div className="grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_5rem_5rem_5rem_4rem] items-center gap-3 border-b px-4 py-2 text-xs font-medium text-muted-foreground">
            <span>{t(($) => $.leaderboard.header_agent)}</span>
            <span />
            <span className={colClass('tokens')}>{t(($) => $.leaderboard.header_tokens)}</span>
            <span className={colClass('cost')}>{t(($) => $.leaderboard.header_cost)}</span>
            <span className={colClass('time')}>{t(($) => $.leaderboard.header_time)}</span>
            <span className={colClass('tasks')}>{t(($) => $.leaderboard.header_tasks)}</span>
          </div>
          {/* A real list, like the Errors card's offender list: the rows are
              now a truncated ranking, so screen readers need the count and the
              item boundaries rather than a bag of divs. */}
          <ul aria-label={t(($) => $.leaderboard.title)} className="divide-y">
            {visibleRows.map((row) => {
              const isDeletedBucket = row.agentId === DELETED_AGENTS_ROW_ID;
              const agent = agents.find((a) => a.id === row.agentId);
              const value = SORT_METRIC[sortBy](row);
              const pct = maxValue > 0 ? (value / maxValue) * 100 : 0;
              return (
                <li
                  key={row.agentId}
                  className="grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_5rem_5rem_5rem_4rem] items-center gap-3 px-4 py-2"
                >
                  <div className="flex min-w-0 items-center gap-2">
                    {isDeletedBucket ? (
                      <>
                        <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
                          <Trash2 className="h-3 w-3" />
                        </span>
                        <span className="truncate text-sm font-medium italic text-muted-foreground">
                          {t(($) => $.leaderboard.deleted_agents)}
                        </span>
                      </>
                    ) : (
                      <>
                        <ActorAvatar
                          actorType="agent"
                          actorId={row.agentId}
                          size="md"
                          enableHoverCard
                        />
                        <span className="cursor-pointer truncate text-sm font-medium">
                          {agent?.name ?? row.agentId}
                        </span>
                      </>
                    )}
                  </div>
                  <div className="relative h-2 overflow-hidden rounded-full bg-muted">
                    <div
                      className="h-full rounded-full bg-chart-1 transition-[width] duration-300 ease-out"
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                  <div
                    className={`text-right text-xs tabular-nums ${sortBy === 'tokens' ? 'font-medium text-foreground' : 'text-muted-foreground'}`}
                  >
                    {formatTokens(row.tokens)}
                  </div>
                  <div
                    className={`text-right tabular-nums ${sortBy === 'cost' ? 'text-sm font-medium' : 'text-xs text-muted-foreground'}`}
                  >
                    ${row.cost.toFixed(2)}
                  </div>
                  <div
                    className={`text-right text-xs tabular-nums ${sortBy === 'time' ? 'font-medium text-foreground' : 'text-muted-foreground'}`}
                  >
                    {isDeletedBucket ? '—' : formatDuration(row.seconds, lessThanMinuteLabel)}
                  </div>
                  <div
                    className={`text-right text-xs tabular-nums ${sortBy === 'tasks' ? 'font-medium text-foreground' : 'text-muted-foreground'}`}
                  >
                    {isDeletedBucket ? '—' : row.taskCount}
                  </div>
                </li>
              );
            })}
          </ul>
        </>
      )}
    </div>
  );
}

function DashboardSkeleton() {
  return (
    <div className="space-y-5">
      <Skeleton className="h-28 rounded-lg" />
      <Skeleton className="h-56 rounded-lg" />
      <Skeleton className="h-48 rounded-lg" />
    </div>
  );
}

function DashboardEmpty() {
  const { t } = useT('usage');
  return (
    <div className="flex flex-col items-center rounded-lg border border-dashed py-12 text-center">
      <BarChart3 className="h-6 w-6 text-muted-foreground/40" />
      <p className="mt-3 text-sm font-medium">{t(($) => $.empty.title)}</p>
      <p className="mt-1 max-w-md text-xs text-muted-foreground">{t(($) => $.empty.body)}</p>
    </div>
  );
}
