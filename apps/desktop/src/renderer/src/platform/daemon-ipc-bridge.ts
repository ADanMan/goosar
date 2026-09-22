'use client';

import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { runtimeKeys } from '@goosar/core/runtimes';
import type { AgentRuntime } from '@goosar/core/types';

interface DaemonStatusLike {
  state:
    | 'running'
    | 'stopped'
    | 'starting'
    | 'stopping'
    | 'installing_cli'
    | 'cli_not_found'
    | 'auth_expired';
  daemonId?: string;
}

function mergeDaemonStatus(rt: AgentRuntime, status: DaemonStatusLike): AgentRuntime {
  if (
    status.state === 'stopped' ||
    status.state === 'stopping' ||
    status.state === 'auth_expired'
  ) {
    return { ...rt, status: 'offline' };
  }
  if (status.state === 'running') {
    return {
      ...rt,
      status: 'online',
      last_seen_at: new Date().toISOString(),
    };
  }
  return rt;
}

export function useDaemonIPCBridge(wsId: string | undefined): void {
  const qc = useQueryClient();

  useEffect(() => {
    if (!wsId) return;
    if (typeof window === 'undefined') return;
    const daemonAPI = (
      window as unknown as {
        daemonAPI?: { onStatusChange?: (cb: (s: DaemonStatusLike) => void) => () => void };
      }
    ).daemonAPI;
    if (!daemonAPI?.onStatusChange) return;

    const unsubscribe = daemonAPI.onStatusChange((status) => {
      if (!status.daemonId) return;
      qc.setQueryData<AgentRuntime[]>(runtimeKeys.list(wsId), (old) => {
        if (!old) return old;
        return old.map((rt) =>
          rt.daemon_id === status.daemonId ? mergeDaemonStatus(rt, status) : rt,
        );
      });
    });

    return unsubscribe;
  }, [wsId, qc]);
}
