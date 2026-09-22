import { keepPreviousData, queryOptions } from '@tanstack/react-query';
import { api } from '../api';

export const dashboardKeys = {
  all: (wsId: string) => ['dashboard', wsId] as const,
  daily: (wsId: string, days: number, projectId: string | null, tz: string) =>
    [...dashboardKeys.all(wsId), 'daily', days, projectId, tz] as const,
  byAgent: (wsId: string, days: number, projectId: string | null, tz: string) =>
    [...dashboardKeys.all(wsId), 'by-agent', days, projectId, tz] as const,
  agentRuntime: (wsId: string, days: number, projectId: string | null, tz: string) =>
    [...dashboardKeys.all(wsId), 'agent-runtime', days, projectId, tz] as const,
  runTimeDaily: (wsId: string, days: number, projectId: string | null, tz: string) =>
    [...dashboardKeys.all(wsId), 'runtime-daily', days, projectId, tz] as const,
  failuresDaily: (wsId: string, days: number, projectId: string | null, tz: string) =>
    [...dashboardKeys.all(wsId), 'failures-daily', days, projectId, tz] as const,
  failuresByAgent: (wsId: string, days: number, projectId: string | null, tz: string) =>
    [...dashboardKeys.all(wsId), 'failures-by-agent', days, projectId, tz] as const,
};

const STALE_TIME = 60 * 1000;

function isSameDashboardScope(
  previousKey: readonly unknown[] | undefined,
  nextKey: readonly unknown[],
): boolean {
  if (!previousKey || previousKey.length !== nextKey.length) return false;
  return previousKey.every((part, index) => index === 3 || Object.is(part, nextKey[index]));
}

export function dashboardUsageDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.daily(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardUsageDaily({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardUsageByAgentOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.byAgent(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardUsageByAgent({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardAgentRunTimeOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.agentRuntime(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardAgentRunTime({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardRunTimeDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.runTimeDaily(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardRunTimeDaily({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardFailuresDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.failuresDaily(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardFailuresDaily({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardFailuresByAgentOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.failuresByAgent(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardFailuresByAgent({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}
