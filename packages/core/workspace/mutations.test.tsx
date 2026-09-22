/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { setApiInstance } from '../api';
import type { ApiClient } from '../api/client';
import { defaultStorage } from '../platform/storage';
import type { Workspace } from '../types';
import { useCreateWorkspace, useDeleteWorkspace } from './mutations';
import { workspaceKeys } from './queries';
import { isWorkspaceDeletePending, unmarkWorkspaceDeletePending } from './pending-delete';

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

const makeWorkspace = (id: string, slug: string): Workspace => ({
  id,
  name: slug,
  slug,
  description: null,
  context: null,
  settings: {},
  repos: [],
  issue_prefix: 'MUL',
  avatar_url: null,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
});

describe('useCreateWorkspace', () => {
  let qc: QueryClient;
  let createWorkspace: ReturnType<
    typeof vi.fn<(data: { name: string; slug: string }) => Promise<Workspace>>
  >;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    createWorkspace = vi.fn();
    setApiInstance({ createWorkspace } as unknown as ApiClient);
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [makeWorkspace('ws-1', 'existing')]);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it('seeds the successful response without invalidating the workspace list', async () => {
    const created = makeWorkspace('ws-2', 'created');
    createWorkspace.mockResolvedValue(created);
    const { result } = renderHook(() => useCreateWorkspace(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ name: 'Created', slug: 'created' });
    });

    expect(
      qc.getQueryData<Workspace[]>(workspaceKeys.list())?.map((workspace) => workspace.id),
    ).toEqual(['ws-1', 'ws-2']);
    expect(qc.getQueryState(workspaceKeys.list())?.isInvalidated).toBe(false);
  });

  it('invalidates the workspace list when create fails', async () => {
    createWorkspace.mockRejectedValue(new Error('response lost'));
    const { result } = renderHook(() => useCreateWorkspace(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({ name: 'Created', slug: 'created' }),
      ).rejects.toThrow('response lost');
    });

    expect(qc.getQueryState(workspaceKeys.list())?.isInvalidated).toBe(true);
    expect(
      qc.getQueryData<Workspace[]>(workspaceKeys.list())?.map((workspace) => workspace.id),
    ).toEqual(['ws-1']);
  });
});

describe('useDeleteWorkspace', () => {
  let qc: QueryClient;
  let deleteWorkspace: ReturnType<typeof vi.fn<(id: string) => Promise<void>>>;
  let listWorkspaces: ReturnType<typeof vi.fn<() => Promise<Workspace[]>>>;

  const serverList = () => [makeWorkspace('ws-1', 'keep-me'), makeWorkspace('ws-2', 'delete-me')];

  const seedList = () => {
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), serverList());
  };

  const cachedList = () => qc.getQueryData<Workspace[]>(workspaceKeys.list()) ?? [];

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    deleteWorkspace = vi.fn().mockResolvedValue(undefined);
    listWorkspaces = vi.fn().mockResolvedValue(serverList());
    setApiInstance({ deleteWorkspace, listWorkspaces } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    unmarkWorkspaceDeletePending('ws-2');
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('leaves the list cache untouched while the DELETE is pending (no optimistic removal)', async () => {
    seedList();
    let resolveDelete!: () => void;
    deleteWorkspace.mockReturnValue(
      new Promise<void>((resolve) => {
        resolveDelete = resolve;
      }),
    );

    const { result } = renderHook(() => useDeleteWorkspace(), {
      wrapper: createWrapper(qc),
    });

    let mutationDone: Promise<void>;
    await act(async () => {
      mutationDone = result.current.mutateAsync('ws-2');
      await Promise.resolve();
    });

    expect(deleteWorkspace).toHaveBeenCalledWith('ws-2');
    expect(cachedList().map((w) => w.id)).toEqual(['ws-1', 'ws-2']);

    await act(async () => {
      resolveDelete();
      await mutationDone;
    });
  });

  it('invalidates the workspace list after a successful delete', async () => {
    seedList();
    const { result } = renderHook(() => useDeleteWorkspace(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync('ws-2');
    });

    expect(qc.getQueryState(workspaceKeys.list())?.isInvalidated).toBe(true);
  });

  it("clears the deleted slug's workspace-scoped storage on success", async () => {
    seedList();
    defaultStorage.setItem('goosar_issue_draft:delete-me', 'draft');
    defaultStorage.setItem('goosar_issue_draft:keep-me', 'draft');

    const { result } = renderHook(() => useDeleteWorkspace(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync('ws-2');
    });

    expect(defaultStorage.getItem('goosar_issue_draft:delete-me')).toBeNull();
    expect(defaultStorage.getItem('goosar_issue_draft:keep-me')).toBe('draft');
  });

  it('leaves storage and cache untouched when the DELETE fails', async () => {
    seedList();
    deleteWorkspace.mockRejectedValue(new Error('boom'));
    defaultStorage.setItem('goosar_issue_draft:delete-me', 'draft');

    const { result } = renderHook(() => useDeleteWorkspace(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await expect(result.current.mutateAsync('ws-2')).rejects.toThrow('boom');
    });

    expect(defaultStorage.getItem('goosar_issue_draft:delete-me')).toBe('draft');
    expect(cachedList().map((w) => w.id)).toEqual(['ws-1', 'ws-2']);
  });

  it('keeps the self-initiated marker after success and lifts it after failure', async () => {
    seedList();
    const { result } = renderHook(() => useDeleteWorkspace(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync('ws-2');
    });
    expect(isWorkspaceDeletePending('ws-2')).toBe(true);

    unmarkWorkspaceDeletePending('ws-2');
    deleteWorkspace.mockRejectedValue(new Error('boom'));
    await act(async () => {
      await expect(result.current.mutateAsync('ws-2')).rejects.toThrow('boom');
    });
    expect(isWorkspaceDeletePending('ws-2')).toBe(false);
  });
});
