'use client';

import { useEffect, useState } from 'react';

export interface LocalDaemonStatus {
  daemonId: string | null;
  deviceName: string | null;
  running: boolean;
}

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
  deviceName?: string;
}

interface DaemonAPILike {
  getStatus?: () => Promise<DaemonStatusLike>;
  onStatusChange?: (cb: (s: DaemonStatusLike) => void) => () => void;
}

function readDaemonAPI(): DaemonAPILike | undefined {
  if (typeof window === 'undefined') return undefined;
  return (window as unknown as { daemonAPI?: DaemonAPILike }).daemonAPI;
}

function toStatus(s: DaemonStatusLike | undefined): LocalDaemonStatus {
  if (!s) return { daemonId: null, deviceName: null, running: false };
  return {
    daemonId: s.daemonId ?? null,
    deviceName: s.deviceName ?? null,
    running: s.state === 'running',
  };
}

export function useLocalDaemonStatus(): LocalDaemonStatus {
  const [status, setStatus] = useState<LocalDaemonStatus>(() => ({
    daemonId: null,
    deviceName: null,
    running: false,
  }));

  useEffect(() => {
    const api = readDaemonAPI();
    if (!api) return;
    let cancelled = false;
    if (api.getStatus) {
      api
        .getStatus()
        .then((s) => {
          if (!cancelled) setStatus(toStatus(s));
        })
        .catch(() => {
          // Ignore — onStatusChange will populate once the daemon comes up.
        });
    }
    const unsubscribe = api.onStatusChange?.((s) => {
      setStatus(toStatus(s));
    });
    return () => {
      cancelled = true;
      unsubscribe?.();
    };
  }, []);

  return status;
}
