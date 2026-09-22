'use client';

import { useQuery } from '@tanstack/react-query';
import { agentListOptions, squadListOptions } from '../workspace/queries';
import { runtimeListOptions } from '../runtimes/queries';
import { agentTaskSnapshotOptions } from './queries';
import {
  useRealtimePollingInterval,
  BACKGROUND_DEGRADED_POLL_INTERVAL_MS,
} from '../realtime/use-realtime-polling-interval';

export function useWorkspacePresencePrefetch(wsId: string | undefined): void {
  const pollingInterval = useRealtimePollingInterval(BACKGROUND_DEGRADED_POLL_INTERVAL_MS);
  useQuery({
    ...agentListOptions(wsId ?? ''),
    enabled: !!wsId,
    refetchInterval: pollingInterval,
  });
  useQuery({
    ...runtimeListOptions(wsId ?? ''),
    enabled: !!wsId,
    refetchInterval: pollingInterval,
  });
  useQuery({
    ...agentTaskSnapshotOptions(wsId ?? ''),
    enabled: !!wsId,
    refetchInterval: pollingInterval,
  });
  useQuery({ ...squadListOptions(wsId ?? ''), enabled: !!wsId });
}
