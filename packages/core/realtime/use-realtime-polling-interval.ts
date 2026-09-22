'use client';

import { useWSConnectionState } from './provider';

export const DEFAULT_DEGRADED_POLL_INTERVAL_MS = 15_000;

export const BACKGROUND_DEGRADED_POLL_INTERVAL_MS = 60_000;

export function useRealtimeConnectionState() {
  return useWSConnectionState();
}

export function useRealtimePollingInterval(
  intervalMs: number = DEFAULT_DEGRADED_POLL_INTERVAL_MS,
): number | false {
  const state = useRealtimeConnectionState();
  return state === 'degraded' ? intervalMs : false;
}
