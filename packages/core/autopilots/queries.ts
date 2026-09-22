import { queryOptions } from '@tanstack/react-query';
import { api } from '../api';

export const autopilotKeys = {
  all: (wsId: string) => ['autopilots', wsId] as const,
  list: (wsId: string) => [...autopilotKeys.all(wsId), 'list'] as const,
  detail: (wsId: string, id: string) => [...autopilotKeys.all(wsId), 'detail', id] as const,
  runs: (wsId: string, id: string) => [...autopilotKeys.all(wsId), 'runs', id] as const,
  run: (wsId: string, autopilotId: string, runId: string) =>
    [...autopilotKeys.all(wsId), 'runs', autopilotId, runId] as const,
  deliveries: (wsId: string, id: string) => [...autopilotKeys.all(wsId), 'deliveries', id] as const,
  delivery: (wsId: string, autopilotId: string, deliveryId: string) =>
    [...autopilotKeys.all(wsId), 'deliveries', autopilotId, deliveryId] as const,
  cronPreview: (wsId: string, expr: string, tz: string) =>
    [...autopilotKeys.all(wsId), 'cron-preview', expr, tz] as const,
};

export function autopilotListOptions(wsId: string) {
  return queryOptions({
    queryKey: autopilotKeys.list(wsId),
    queryFn: () => api.listAutopilots(),
    select: (data) => data.autopilots,
  });
}

export function autopilotDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: autopilotKeys.detail(wsId, id),
    queryFn: () => api.getAutopilot(id),
  });
}

export function autopilotRunsOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: autopilotKeys.runs(wsId, id),
    queryFn: () => api.listAutopilotRuns(id),
    select: (data) => data.runs,
  });
}

export function autopilotRunOptions(
  wsId: string,
  autopilotId: string,
  runId: string,
  options?: { enabled?: boolean },
) {
  return queryOptions({
    queryKey: autopilotKeys.run(wsId, autopilotId, runId),
    queryFn: () => api.getAutopilotRun(autopilotId, runId),
    enabled: options?.enabled ?? true,
  });
}

export function autopilotDeliveriesOptions(
  wsId: string,
  autopilotId: string,
  options?: { enabled?: boolean },
) {
  return queryOptions({
    queryKey: autopilotKeys.deliveries(wsId, autopilotId),
    queryFn: () => api.listAutopilotDeliveries(autopilotId),
    select: (data) => data.deliveries,
    enabled: options?.enabled ?? true,
  });
}

export function autopilotDeliveryOptions(
  wsId: string,
  autopilotId: string,
  deliveryId: string,
  options?: { enabled?: boolean },
) {
  return queryOptions({
    queryKey: autopilotKeys.delivery(wsId, autopilotId, deliveryId),
    queryFn: () => api.getAutopilotDelivery(autopilotId, deliveryId),
    enabled: options?.enabled ?? true,
  });
}

export function cronPreviewOptions(
  wsId: string,
  expr: string,
  tz: string,
  options?: { enabled?: boolean },
) {
  return queryOptions({
    queryKey: autopilotKeys.cronPreview(wsId, expr, tz),
    queryFn: () => api.cronPreview({ expr, tz }),
    enabled: options?.enabled ?? true,
    staleTime: 30_000,
    retry: false,
  });
}
