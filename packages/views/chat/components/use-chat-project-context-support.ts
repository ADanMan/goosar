'use client';

import { useQuery } from '@tanstack/react-query';
import {
  runtimeListOptions,
  readRuntimeCliVersion,
  chatProjectContextSupported,
} from '@goosar/core/runtimes';

export function useChatProjectContextSupport(
  wsId: string,
  agent: { runtime_id?: string | null } | null | undefined,
): boolean | null {
  const { data: runtimes = [] } = useQuery({ ...runtimeListOptions(wsId), enabled: !!wsId });
  if (!agent?.runtime_id) return null;
  const runtime = runtimes.find((r) => r.id === agent.runtime_id);
  if (!runtime) return null;
  return chatProjectContextSupported(readRuntimeCliVersion(runtime.metadata));
}
