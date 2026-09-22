import { describe, it, expect, beforeEach, vi } from 'vitest';
import { act, renderHook, waitFor, configure } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

interface QueuedRestore {
  id: string;
  content: string;
  attachments?: unknown[];
  sessionId: string;
}

const h = vi.hoisted(() => {
  const store = {
    appliedDraftRestoreIds: [] as string[],
    markDraftRestoreApplied: vi.fn(),
    forgetDraftRestoreApplied: vi.fn(),
    inputDrafts: {} as Record<string, string>,
    inputDraftAttachments: {} as Record<string, unknown[]>,
    setInputDraft: vi.fn(),
    setInputDraftAttachments: vi.fn(),
    pendingSendRestores: {} as Record<string, QueuedRestore[]>,
    enqueuePendingSendRestore: vi.fn((r: QueuedRestore) => {
      const existing = store.pendingSendRestores[r.sessionId] ?? [];
      if (existing.some((q) => q.id === r.id)) return;
      store.pendingSendRestores = {
        ...store.pendingSendRestores,
        [r.sessionId]: [...existing, r],
      };
    }),
    dequeuePendingSendRestore: vi.fn((sessionId: string, restoreId: string) => {
      const existing = store.pendingSendRestores[sessionId] ?? [];
      const remaining = existing.filter((q) => q.id !== restoreId);
      const next = { ...store.pendingSendRestores };
      if (remaining.length > 0) next[sessionId] = remaining;
      else delete next[sessionId];
      store.pendingSendRestores = next;
    }),
  };
  return {
    listChatDraftRestores: vi.fn(),
    consumeChatDraftRestore: vi.fn(),
    store,
  };
});

vi.mock('@goosar/core/api', () => ({
  api: {
    listChatDraftRestores: h.listChatDraftRestores,
    consumeChatDraftRestore: h.consumeChatDraftRestore,
  },
}));
vi.mock('@goosar/core/hooks', () => ({ useWorkspaceId: () => 'ws-1' }));
vi.mock('@goosar/core/chat', () => ({
  useChatStore: Object.assign((sel: (s: typeof h.store) => unknown) => sel(h.store), {
    getState: () => h.store,
  }),
}));
vi.mock('@goosar/core/realtime', () => ({ removeChatMessageFromCaches: vi.fn() }));
vi.mock('@goosar/core/logger', () => ({
  createLogger: () => ({ info: vi.fn(), warn: vi.fn(), error: vi.fn(), debug: vi.fn() }),
}));

import type { Attachment } from '@goosar/core/types';
import { useChatDraftRestore } from './use-chat-draft-restore';

vi.setConfig({ testTimeout: 20000 });
configure({ asyncUtilTimeout: 5000 });

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

const RESTORE = { id: 'msg-1', chat_session_id: 'sA', content: 'run the thing' };

beforeEach(() => {
  h.listChatDraftRestores.mockReset().mockResolvedValue({ restores: [RESTORE] });
  h.consumeChatDraftRestore.mockReset().mockResolvedValue(undefined);
  h.store.appliedDraftRestoreIds = [];
  h.store.markDraftRestoreApplied.mockReset();
  h.store.forgetDraftRestoreApplied.mockReset();
  h.store.inputDrafts = {};
  h.store.inputDraftAttachments = {};
  h.store.setInputDraft.mockReset();
  h.store.setInputDraftAttachments.mockReset();
  h.store.pendingSendRestores = {};
  h.store.enqueuePendingSendRestore.mockClear();
  h.store.dequeuePendingSendRestore.mockClear();
});

describe('useChatDraftRestore ownership (#5219)', () => {
  it('does not fetch, offer, or consume from a composer the user cannot see', async () => {
    const { result } = renderHook(() => useChatDraftRestore('sA', false), { wrapper });

    await act(async () => {
      await Promise.resolve();
    });

    expect(h.listChatDraftRestores).not.toHaveBeenCalled();
    expect(result.current.restoreDraftRequest).toBeNull();
    expect(h.consumeChatDraftRestore).not.toHaveBeenCalled();
  });

  it('lets the visible composer win the race against a hidden one', async () => {
    const hidden = renderHook(() => useChatDraftRestore('sA', false), { wrapper });
    const visible = renderHook(() => useChatDraftRestore('sA', true), { wrapper });

    await waitFor(() => expect(visible.result.current.restoreDraftRequest).not.toBeNull());
    expect(hidden.result.current.restoreDraftRequest).toBeNull();

    act(() => visible.result.current.handleRestoreDraftApplied());

    expect(h.store.markDraftRestoreApplied).toHaveBeenCalledWith('msg-1');
    await waitFor(() => expect(h.consumeChatDraftRestore).toHaveBeenCalledTimes(1));
    expect(h.consumeChatDraftRestore).toHaveBeenCalledWith('sA', 'msg-1');
  });

  it('drops an unclaimed offer when the composer is hidden mid-flight', async () => {
    const { result, rerender } = renderHook(
      ({ open }: { open: boolean }) => useChatDraftRestore('sA', open),
      { wrapper, initialProps: { open: true } },
    );

    await waitFor(() => expect(result.current.restoreDraftRequest).not.toBeNull());

    rerender({ open: false });

    expect(result.current.restoreDraftRequest).toBeNull();
    expect(h.store.markDraftRestoreApplied).not.toHaveBeenCalled();
    expect(h.consumeChatDraftRestore).not.toHaveBeenCalled();
  });
});

describe('useChatDraftRestore server-less restore queue (#5219)', () => {
  const sendFailure = (sessionId: string) => ({
    id: 'send-failed-1',
    content: 'the text that failed to send',
    attachments: [{ id: 'att-1' } as Attachment],
    sessionId,
  });

  it("queues a restore for a session the user is not looking at, leaving the active session's durable restore free to land", async () => {
    const { result, rerender } = renderHook(() => useChatDraftRestore('sA', true), { wrapper });

    act(() => result.current.enqueueLocalRestore(sendFailure('sB')));
    rerender();

    expect(h.store.pendingSendRestores.sB).toEqual([sendFailure('sB')]);
    expect(h.store.setInputDraft).not.toHaveBeenCalled();

    await waitFor(() => expect(result.current.restoreDraftRequest?.serverRestoreId).toBe('msg-1'));
  });

  it('survives an unmount and is re-offered when the user returns, even with work in progress there', async () => {
    h.store.inputDrafts = { sB: 'work in progress' };

    const first = renderHook(() => useChatDraftRestore('sA', true), { wrapper });
    act(() => first.result.current.enqueueLocalRestore(sendFailure('sB')));
    first.rerender();

    await waitFor(() =>
      expect(first.result.current.restoreDraftRequest?.serverRestoreId).toBe('msg-1'),
    );

    first.unmount();

    expect(h.store.pendingSendRestores.sB).toEqual([sendFailure('sB')]);

    const second = renderHook(() => useChatDraftRestore('sB', true), { wrapper });
    await waitFor(() =>
      expect(second.result.current.restoreDraftRequest?.id).toBe('send-failed-1'),
    );
    expect(second.result.current.restoreDraftRequest?.sessionId).toBe('sB');
    expect(h.store.pendingSendRestores.sB).toHaveLength(1);
  });

  it('leaves the queue entry alone until the composer reports the hand-off, then drops it', async () => {
    const { result, rerender } = renderHook(() => useChatDraftRestore('sB', true), { wrapper });

    act(() => result.current.enqueueLocalRestore(sendFailure('sB')));
    rerender();

    await waitFor(() => expect(result.current.restoreDraftRequest?.id).toBe('send-failed-1'));
    expect(h.store.dequeuePendingSendRestore).not.toHaveBeenCalled();

    act(() => result.current.handleRestoreDraftApplied());

    expect(h.store.dequeuePendingSendRestore).toHaveBeenCalledWith('sB', 'send-failed-1');
    expect(h.store.pendingSendRestores.sB).toBeUndefined();
    expect(h.consumeChatDraftRestore).not.toHaveBeenCalled();
    expect(result.current.restoreDraftRequest).toBeNull();
  });

  it('drops — never parks — a durable restore once the user navigates away from its session', async () => {
    const { result, rerender } = renderHook(
      ({ session }: { session: string }) => useChatDraftRestore(session, true),
      { wrapper, initialProps: { session: 'sA' } },
    );

    await waitFor(() => expect(result.current.restoreDraftRequest?.serverRestoreId).toBe('msg-1'));

    rerender({ session: 'sB' });

    await waitFor(() => expect(result.current.restoreDraftRequest).toBeNull());
    expect(h.store.markDraftRestoreApplied).not.toHaveBeenCalled();
    expect(h.consumeChatDraftRestore).not.toHaveBeenCalled();
  });
});

describe('useChatDraftRestore reconciliation (#5219)', () => {
  it('re-consumes a row on the next fetch after the consume failed', async () => {
    h.store.appliedDraftRestoreIds = ['msg-1'];
    h.consumeChatDraftRestore.mockRejectedValueOnce(new Error('offline'));

    const { result, rerender } = renderHook(
      ({ session }: { session: string }) => useChatDraftRestore(session, true),
      { wrapper, initialProps: { session: 'sA' } },
    );

    await waitFor(() => expect(h.consumeChatDraftRestore).toHaveBeenCalledTimes(1));
    expect(result.current.restoreDraftRequest).toBeNull();
    expect(h.store.forgetDraftRestoreApplied).not.toHaveBeenCalled();

    rerender({ session: 'sB' });
    rerender({ session: 'sA' });

    await waitFor(() => expect(h.consumeChatDraftRestore).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(h.store.forgetDraftRestoreApplied).toHaveBeenCalledWith('msg-1'));
    expect(result.current.restoreDraftRequest).toBeNull();
  });
});
