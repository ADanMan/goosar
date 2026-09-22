import { queryOptions } from '@tanstack/react-query';
import { api } from '../api';
import type { RuntimeModelsResult } from '../types/agent';

export const runtimeModelsKeys = {
  all: () => ['runtimes', 'models'] as const,
  forRuntime: (runtimeId: string) => [...runtimeModelsKeys.all(), runtimeId] as const,
};

const POLL_INTERVAL_MS = 500;
const POLL_TIMEOUT_MS = 30_000;

export async function resolveRuntimeModels(runtimeId: string): Promise<RuntimeModelsResult> {
  const initial = await api.initiateListModels(runtimeId);
  const start = Date.now();
  let current = initial;
  while (current.status === 'pending' || current.status === 'running') {
    if (Date.now() - start > POLL_TIMEOUT_MS) {
      throw new Error('model discovery timed out');
    }
    await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS));
    current = await api.getListModelsResult(runtimeId, initial.id);
  }
  if (current.status === 'failed' || current.status === 'timeout') {
    throw new Error(current.error || 'model discovery failed');
  }
  return { models: current.models ?? [], supported: current.supported };
}

export function runtimeModelsOptions(runtimeId: string | null | undefined) {
  return queryOptions({
    queryKey: runtimeId ? runtimeModelsKeys.forRuntime(runtimeId) : runtimeModelsKeys.all(),
    queryFn: () => resolveRuntimeModels(runtimeId as string),
    enabled: Boolean(runtimeId),
    staleTime: 60_000,
    retry: false,
  });
}
