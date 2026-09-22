/**
 * @vitest-environment jsdom
 */
import { renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const h = vi.hoisted(() => ({
  pollingInterval: false as number | false,
  captured: new Map<string, unknown>(),
}));

vi.mock('@tanstack/react-query', () => ({
  useQuery: (options: { queryKey: readonly unknown[]; refetchInterval?: unknown }) => {
    h.captured.set(String(options.queryKey[0]), options.refetchInterval);
    return { data: [], isPending: false, isError: false };
  },
}));

vi.mock('../workspace/queries', () => ({
  agentListOptions: (wsId: string) => ({ queryKey: ['agents', wsId] }),
  squadListOptions: (wsId: string) => ({ queryKey: ['squads', wsId] }),
}));
vi.mock('../runtimes/queries', () => ({
  runtimeListOptions: (wsId: string) => ({ queryKey: ['runtimes', wsId] }),
}));
vi.mock('./queries', () => ({
  agentTaskSnapshotOptions: (wsId: string) => ({
    queryKey: ['agent-task-snapshot', wsId],
  }),
}));

vi.mock('../realtime/use-realtime-polling-interval', () => ({
  useRealtimePollingInterval: (ms?: number) =>
    h.pollingInterval === false ? false : (ms ?? h.pollingInterval),
  DEFAULT_DEGRADED_POLL_INTERVAL_MS: 15_000,
  BACKGROUND_DEGRADED_POLL_INTERVAL_MS: 60_000,
}));

import { useWorkspacePresencePrefetch } from './use-workspace-presence-prefetch';
import { useWorkspacePresenceMap, useAgentPresenceDetail } from './use-agent-presence';
import { BACKGROUND_DEGRADED_POLL_INTERVAL_MS } from '../realtime/use-realtime-polling-interval';

const PRESENCE_KEYS = ['agents', 'runtimes', 'agent-task-snapshot'] as const;

beforeEach(() => {
  h.captured.clear();
  h.pollingInterval = false;
});

describe('useWorkspacePresencePrefetch — the single degraded-mode poll owner (#257)', () => {
  it('does not poll while the realtime connection is healthy', () => {
    h.pollingInterval = false;
    renderHook(() => useWorkspacePresencePrefetch('ws-1'));

    for (const key of PRESENCE_KEYS) {
      expect(h.captured.get(key)).toBe(false);
    }
  });

  it('polls all three presence caches on the background cadence while degraded', () => {
    h.pollingInterval = BACKGROUND_DEGRADED_POLL_INTERVAL_MS;
    renderHook(() => useWorkspacePresencePrefetch('ws-1'));

    for (const key of PRESENCE_KEYS) {
      expect(h.captured.get(key)).toBe(BACKGROUND_DEGRADED_POLL_INTERVAL_MS);
    }
  });

  it('leaves the squad roster alone — a directory, not a presence signal', () => {
    h.pollingInterval = BACKGROUND_DEGRADED_POLL_INTERVAL_MS;
    renderHook(() => useWorkspacePresencePrefetch('ws-1'));

    expect(h.captured.get('squads')).toBeUndefined();
  });
});

describe('presence readers are deliberately not interval owners (#257)', () => {
  it('useWorkspacePresenceMap never sets a refetch interval, even while degraded', () => {
    h.pollingInterval = BACKGROUND_DEGRADED_POLL_INTERVAL_MS;
    renderHook(() => useWorkspacePresenceMap('ws-1'));

    for (const key of PRESENCE_KEYS) {
      expect(h.captured.get(key)).toBeUndefined();
    }
  });

  it('useAgentPresenceDetail never sets a refetch interval, even while degraded', () => {
    h.pollingInterval = BACKGROUND_DEGRADED_POLL_INTERVAL_MS;
    renderHook(() => useAgentPresenceDetail('ws-1', 'agent-1'));

    for (const key of PRESENCE_KEYS) {
      expect(h.captured.get(key)).toBeUndefined();
    }
  });
});
