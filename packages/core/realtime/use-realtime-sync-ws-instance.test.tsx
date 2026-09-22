/**
 * @vitest-environment jsdom
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import type { WSClient } from '../api/ws-client';
import { defaultStorage } from '../platform/storage';
import { issueKeys } from '../issues/queries';
import { workspaceWorkingAgentsKeys } from '../agents/queries';
import { workspaceKeys } from '../workspace/queries';
import {
  markWorkspaceDeletePending,
  unmarkWorkspaceDeletePending,
} from '../workspace/pending-delete';
import { useRealtimeSync, type RealtimeSyncStores } from './use-realtime-sync';

vi.mock('../platform/workspace-storage', () => ({
  getCurrentWsId: () => 'ws-1',
  getCurrentSlug: () => 'test-ws',
  createWorkspaceAwareStorage: (adapter: unknown) => adapter,
  registerForWorkspaceRehydration: () => {},
}));

vi.mock('../paths', () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => '/',
}));

function createMockWs(): WSClient {
  return {
    on: vi.fn(() => () => {}),
    onAny: vi.fn(() => () => {}),
    onReconnect: vi.fn(() => () => {}),
  } as unknown as WSClient;
}

function createStores(): RealtimeSyncStores {
  return {
    authStore: Object.assign(() => ({}), {
      getState: () => ({ user: { id: 'u1' } }),
      subscribe: () => () => {},
      setState: () => {},
      destroy: () => {},
    }),
  } as unknown as RealtimeSyncStores;
}

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe('useRealtimeSync — ws instance change', () => {
  let qc: QueryClient;
  let stores: RealtimeSyncStores;
  let invalidateSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    stores = createStores();
    invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
  });

  it('skips invalidation on first non-null ws instance', () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });

    expect(invalidateSpy).not.toHaveBeenCalled();
  });

  it('does not invalidate when ws goes from instance to null', () => {
    const ws1 = createMockWs();
    const { rerender } = renderHook(({ ws }) => useRealtimeSync(ws, stores), {
      initialProps: { ws: ws1 as WSClient | null },
      wrapper: createWrapper(qc),
    });

    invalidateSpy.mockClear();
    rerender({ ws: null });

    expect(invalidateSpy).not.toHaveBeenCalled();
  });

  it('invalidates exactly once when a new ws instance appears after null gap', () => {
    const ws1 = createMockWs();
    const { rerender } = renderHook(({ ws }) => useRealtimeSync(ws, stores), {
      initialProps: { ws: ws1 as WSClient | null },
      wrapper: createWrapper(qc),
    });

    invalidateSpy.mockClear();
    rerender({ ws: null });
    expect(invalidateSpy).not.toHaveBeenCalled();

    const ws2 = createMockWs();
    rerender({ ws: ws2 });

    expect(invalidateSpy).toHaveBeenCalledTimes(30);
  });

  it('does not re-invalidate when rerendered with the same ws instance', () => {
    const ws1 = createMockWs();
    const { rerender } = renderHook(({ ws }) => useRealtimeSync(ws, stores), {
      initialProps: { ws: ws1 as WSClient | null },
      wrapper: createWrapper(qc),
    });

    invalidateSpy.mockClear();
    rerender({ ws: ws1 });

    expect(invalidateSpy).not.toHaveBeenCalled();
  });

  it('invalidates chat, pins, labels, and invitations queries on ws instance change', () => {
    const ws1 = createMockWs();
    const { rerender } = renderHook(({ ws }) => useRealtimeSync(ws, stores), {
      initialProps: { ws: ws1 as WSClient | null },
      wrapper: createWrapper(qc),
    });

    invalidateSpy.mockClear();
    rerender({ ws: null });

    const ws2 = createMockWs();
    rerender({ ws: ws2 });

    const calls = invalidateSpy.mock.calls.map(
      (call: [{ queryKey?: unknown }, ...unknown[]]) => call[0].queryKey,
    );
    expect(calls).toContainEqual(['chat', 'ws-1']);
    expect(calls).toContainEqual(['labels', 'ws-1']);
    expect(calls).toContainEqual(['workspaces', 'ws-1', 'invitations']);
  });

  it('invalidates per-issue caches (no wsId in key) on ws instance change', () => {
    const ws1 = createMockWs();
    const { rerender } = renderHook(({ ws }) => useRealtimeSync(ws, stores), {
      initialProps: { ws: ws1 as WSClient | null },
      wrapper: createWrapper(qc),
    });

    invalidateSpy.mockClear();
    rerender({ ws: null });

    const ws2 = createMockWs();
    rerender({ ws: ws2 });

    const calls = invalidateSpy.mock.calls.map(
      (call: [{ queryKey?: unknown }, ...unknown[]]) => call[0].queryKey,
    );
    expect(calls).toContainEqual(['issues', 'timeline']);
    expect(calls).toContainEqual(['issues', 'reactions']);
    expect(calls).toContainEqual(['issues', 'subscribers']);
    expect(calls).toContainEqual(['issues', 'usage']);
    expect(calls).toContainEqual(['issues', 'attachments']);
    expect(calls).toContainEqual(['issues', 'tasks']);
  });

  it('invalidates per-chat-session caches (no wsId in key) on ws instance change', () => {
    const ws1 = createMockWs();
    const { rerender } = renderHook(({ ws }) => useRealtimeSync(ws, stores), {
      initialProps: { ws: ws1 as WSClient | null },
      wrapper: createWrapper(qc),
    });

    invalidateSpy.mockClear();
    rerender({ ws: null });

    const ws2 = createMockWs();
    rerender({ ws: ws2 });

    const calls = invalidateSpy.mock.calls.map(
      (call: [{ queryKey?: unknown }, ...unknown[]]) => call[0].queryKey,
    );
    expect(calls).toContainEqual(['chat', 'messages']);
    expect(calls).toContainEqual(['chat', 'messages-page']);
    expect(calls).toContainEqual(['chat', 'pending-task']);
    expect(calls).toContainEqual(['task-messages']);
  });
});

describe('useRealtimeSync — Table server membership invalidation', () => {
  let qc: QueryClient;
  let stores: RealtimeSyncStores;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    stores = createStores();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('invalidates Table queries after a task lifecycle event', () => {
    vi.useFakeTimers();
    const ws = createMockWs();
    const invalidate = vi.spyOn(qc, 'invalidateQueries');
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });
    const onAny = vi.mocked(ws.onAny).mock.calls[0]?.[0];
    expect(onAny).toBeDefined();

    onAny!({ type: 'task:completed', payload: {} } as never);
    vi.advanceTimersByTime(100);

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: issueKeys.tableAll('ws-1'),
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: workspaceWorkingAgentsKeys.all('ws-1'),
    });
  });

  it('invalidates Table queries after a property definition changes', () => {
    const ws = createMockWs();
    const invalidate = vi.spyOn(qc, 'invalidateQueries');
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });
    const propertyUpdated = vi
      .mocked(ws.on)
      .mock.calls.find(([event]) => event === 'property:updated')?.[1];
    expect(propertyUpdated).toBeDefined();

    (propertyUpdated as (payload: unknown) => void)({});

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: issueKeys.tableAll('ws-1'),
    });
  });
});

describe('useRealtimeSync — workspace:deleted self-initiated suppression', () => {
  let qc: QueryClient;
  let stores: RealtimeSyncStores;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    stores = createStores();
  });

  afterEach(() => {
    unmarkWorkspaceDeletePending('ws-2');
    localStorage.clear();
  });

  const dispatchWorkspaceDeleted = (ws: WSClient, workspaceId: string) => {
    const call = vi.mocked(ws.on).mock.calls.find(([event]) => event === 'workspace:deleted');
    expect(call).toBeDefined();
    (call![1] as (p: unknown) => void)({ workspace_id: workspaceId });
  };

  it('ignores the event for a delete this client initiated', () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });
    qc.setQueryData(workspaceKeys.list(), [{ id: 'ws-2', slug: 'delete-me' }]);
    defaultStorage.setItem('goosar_issue_draft:delete-me', 'draft');

    markWorkspaceDeletePending('ws-2');
    dispatchWorkspaceDeleted(ws, 'ws-2');

    expect(defaultStorage.getItem('goosar_issue_draft:delete-me')).toBe('draft');
  });

  it('still cleans up for a delete initiated elsewhere', () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });
    qc.setQueryData(workspaceKeys.list(), [{ id: 'ws-2', slug: 'delete-me' }]);
    defaultStorage.setItem('goosar_issue_draft:delete-me', 'draft');

    dispatchWorkspaceDeleted(ws, 'ws-2');

    expect(defaultStorage.getItem('goosar_issue_draft:delete-me')).toBeNull();
  });
});
