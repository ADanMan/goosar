import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';

const stableHandles = vi.hoisted(() => ({
  createMutateAsync: vi.fn(async () => ({})),
  updateMutateAsync: vi.fn(async () => ({})),
  deleteMutateAsync: vi.fn(async () => ({})),
  resolveMutateAsync: vi.fn(async () => ({})),
  toggleMutate: vi.fn(),
}));

const wsHandlers = vi.hoisted(() => new Map<string, (payload: unknown) => void>());

vi.mock('@goosar/core/issues/mutations', () => ({
  useCreateComment: () => ({
    mutateAsync: stableHandles.createMutateAsync,
    mutate: vi.fn(),
    isPending: false,
  }),
  useUpdateComment: () => ({
    mutateAsync: stableHandles.updateMutateAsync,
    mutate: vi.fn(),
    isPending: false,
  }),
  useDeleteComment: () => ({
    mutateAsync: stableHandles.deleteMutateAsync,
    mutate: vi.fn(),
    isPending: false,
  }),
  useResolveComment: () => ({
    mutateAsync: stableHandles.resolveMutateAsync,
    mutate: vi.fn(),
    isPending: false,
  }),
  useToggleCommentReaction: () => ({
    mutateAsync: vi.fn(),
    mutate: stableHandles.toggleMutate,
    isPending: false,
  }),
}));

vi.mock('@goosar/core/issues/queries', () => ({
  issueTimelineOptions: (id: string) => ({
    queryKey: ['issues', 'timeline', id],
    queryFn: () => Promise.resolve([]),
  }),
  issueKeys: {
    timeline: (id: string) => ['issues', 'timeline', id],
  },
}));

const queryState = vi.hoisted(() => ({
  data: undefined as unknown,
  isLoading: false,
  pollingInterval: false as number | false,
  capturedRefetchInterval: undefined as unknown,
}));

const cacheUpdates = vi.hoisted(() => ({
  last: null as unknown,
}));

vi.mock('@tanstack/react-query', async () => {
  const actual =
    await vi.importActual<typeof import('@tanstack/react-query')>('@tanstack/react-query');
  return {
    ...actual,
    useQuery: (options?: { refetchInterval?: unknown }) => {
      queryState.capturedRefetchInterval = options?.refetchInterval;
      return {
        data: queryState.data,
        isLoading: queryState.isLoading,
      };
    },
    useQueryClient: () => ({
      invalidateQueries: vi.fn(),
      setQueryData: vi.fn((_key: unknown, updater: unknown) => {
        cacheUpdates.last =
          typeof updater === 'function'
            ? (updater as (old: unknown) => unknown)(queryState.data)
            : updater;
      }),
      getQueryData: vi.fn(),
      cancelQueries: vi.fn(),
    }),
    useMutationState: () => [],
  };
});

vi.mock('@goosar/core/realtime', () => ({
  useWSEvent: (event: string, handler: (payload: unknown) => void) => {
    wsHandlers.set(event, handler);
  },
  useWSReconnect: vi.fn(),
  useRealtimePollingInterval: () => queryState.pollingInterval,
  DEFAULT_DEGRADED_POLL_INTERVAL_MS: 15_000,
}));

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { useIssueTimeline } from './use-issue-timeline';

describe('useIssueTimeline', () => {
  beforeEach(() => {
    wsHandlers.clear();
    stableHandles.createMutateAsync.mockClear();
    stableHandles.updateMutateAsync.mockClear();
    stableHandles.deleteMutateAsync.mockClear();
    stableHandles.resolveMutateAsync.mockClear();
    stableHandles.toggleMutate.mockClear();
    queryState.data = [];
    queryState.isLoading = false;
    queryState.pollingInterval = false;
    queryState.capturedRefetchInterval = undefined;
    cacheUpdates.last = null;
  });

  describe('degraded-mode REST polling (#257)', () => {
    it('does not poll while the realtime connection is healthy', () => {
      queryState.pollingInterval = false;
      renderHook(() => useIssueTimeline('issue-1', 'user-1'));
      expect(queryState.capturedRefetchInterval).toBe(false);
    });

    it('polls the timeline while the realtime connection is degraded', () => {
      queryState.pollingInterval = 15_000;
      renderHook(() => useIssueTimeline('issue-1', 'user-1'));
      expect(queryState.capturedRefetchInterval).toBe(15_000);
    });
  });

  it('submitReply / editComment / deleteComment / toggleReaction keep identity across unrelated re-renders', () => {
    const { result, rerender } = renderHook(() => useIssueTimeline('issue-1', 'user-1'));

    const first = {
      submitComment: result.current.submitComment,
      submitReply: result.current.submitReply,
      editComment: result.current.editComment,
      deleteComment: result.current.deleteComment,
      toggleReaction: result.current.toggleReaction,
    };

    rerender();
    rerender();

    expect(result.current.submitReply).toBe(first.submitReply);
    expect(result.current.editComment).toBe(first.editComment);
    expect(result.current.deleteComment).toBe(first.deleteComment);
    expect(result.current.toggleReaction).toBe(first.toggleReaction);
    expect(result.current.submitComment).toBe(first.submitComment);
  });

  it('returns the timeline as a flat array directly from the query cache', () => {
    queryState.data = [
      {
        type: 'comment',
        id: 'c1',
        actor_type: 'member',
        actor_id: 'u',
        created_at: '2026-05-06T01:00:00Z',
      },
      {
        type: 'comment',
        id: 'c2',
        actor_type: 'member',
        actor_id: 'u',
        created_at: '2026-05-06T02:00:00Z',
      },
      {
        type: 'comment',
        id: 'c3',
        actor_type: 'member',
        actor_id: 'u',
        created_at: '2026-05-06T03:00:00Z',
      },
    ];
    const { result } = renderHook(() => useIssueTimeline('issue-1', 'user-1'));
    expect(result.current.timeline.map((e) => e.id)).toEqual(['c1', 'c2', 'c3']);
  });

  it('passes suppressed agent ids through editComment', async () => {
    const { result } = renderHook(() => useIssueTimeline('issue-1', 'user-1'));

    await act(async () => {
      await result.current.editComment('comment-1', 'updated', ['attachment-1'], ['agent-1']);
    });

    expect(stableHandles.updateMutateAsync).toHaveBeenCalledWith({
      commentId: 'comment-1',
      content: 'updated',
      attachmentIds: ['attachment-1'],
      suppressAgentIds: ['agent-1'],
    });
  });

  it('comment:created appends the new entry to the cache', () => {
    queryState.data = [];
    renderHook(() => useIssueTimeline('issue-1', 'user-1'));
    const handler = wsHandlers.get('comment:created');
    act(() => {
      handler!({
        comment: {
          id: 'new-c',
          issue_id: 'issue-1',
          author_type: 'member',
          author_id: 'u',
          content: 'hi',
          parent_id: null,
          created_at: '2026-05-06T05:00:00Z',
          updated_at: '2026-05-06T05:00:00Z',
          type: 'comment',
          reactions: [],
          attachments: [],
        },
      });
    });
    const updated = cacheUpdates.last as Array<{ id: string }>;
    expect(updated.map((e) => e.id)).toEqual(['new-c']);
  });

  it('comment:created inserts at the correct sorted position by created_at', () => {
    queryState.data = [
      {
        type: 'comment',
        id: 'c1',
        actor_type: 'member',
        actor_id: 'u',
        created_at: '2026-05-06T01:00:00Z',
      },
      {
        type: 'comment',
        id: 'c3',
        actor_type: 'member',
        actor_id: 'u',
        created_at: '2026-05-06T03:00:00Z',
      },
    ];
    renderHook(() => useIssueTimeline('issue-1', 'user-1'));
    const handler = wsHandlers.get('comment:created');
    act(() => {
      handler!({
        comment: {
          id: 'c2',
          issue_id: 'issue-1',
          author_type: 'member',
          author_id: 'u',
          content: '',
          parent_id: null,
          created_at: '2026-05-06T02:00:00Z',
          updated_at: '2026-05-06T02:00:00Z',
          type: 'comment',
          reactions: [],
          attachments: [],
        },
      });
    });
    const updated = cacheUpdates.last as Array<{ id: string }>;
    expect(updated.map((e) => e.id)).toEqual(['c1', 'c2', 'c3']);
  });

  it('comment:created re-sorts when the new entry is oldest', () => {
    queryState.data = [
      {
        type: 'comment',
        id: 'c2',
        actor_type: 'member',
        actor_id: 'u',
        created_at: '2026-05-06T02:00:00Z',
      },
      {
        type: 'comment',
        id: 'c3',
        actor_type: 'member',
        actor_id: 'u',
        created_at: '2026-05-06T03:00:00Z',
      },
    ];
    renderHook(() => useIssueTimeline('issue-1', 'user-1'));
    const handler = wsHandlers.get('comment:created');
    act(() => {
      handler!({
        comment: {
          id: 'c1',
          issue_id: 'issue-1',
          author_type: 'member',
          author_id: 'u',
          content: '',
          parent_id: null,
          created_at: '2026-05-06T01:00:00Z',
          updated_at: '2026-05-06T01:00:00Z',
          type: 'comment',
          reactions: [],
          attachments: [],
        },
      });
    });
    const updated = cacheUpdates.last as Array<{ id: string }>;
    expect(updated.map((e) => e.id)).toEqual(['c1', 'c2', 'c3']);
  });

  it('ignores WS events for other issues', () => {
    queryState.data = [];
    renderHook(() => useIssueTimeline('issue-1', 'user-1'));
    const handler = wsHandlers.get('comment:created');
    act(() => {
      handler!({
        comment: {
          id: 'x',
          issue_id: 'different-issue',
          author_type: 'member',
          author_id: 'u',
          content: '',
          parent_id: null,
          created_at: '',
          updated_at: '',
          type: 'comment',
          reactions: [],
          attachments: [],
        },
      });
    });
    expect(cacheUpdates.last).toBeNull();
  });

  it('comment:resolved updates the matching entry in place with the new resolved fields', () => {
    queryState.data = [
      {
        type: 'comment',
        id: 'c1',
        actor_type: 'member',
        actor_id: 'u',
        content: 'hello',
        parent_id: null,
        created_at: '2026-05-06T01:00:00Z',
        updated_at: '2026-05-06T01:00:00Z',
        reactions: [],
        attachments: [],
        resolved_at: null,
        resolved_by_type: null,
        resolved_by_id: null,
      },
      {
        type: 'comment',
        id: 'c2',
        actor_type: 'member',
        actor_id: 'u',
        content: 'untouched',
        parent_id: null,
        created_at: '2026-05-06T02:00:00Z',
        updated_at: '2026-05-06T02:00:00Z',
        reactions: [],
        attachments: [],
        resolved_at: null,
        resolved_by_type: null,
        resolved_by_id: null,
      },
    ];
    renderHook(() => useIssueTimeline('issue-1', 'user-1'));
    const handler = wsHandlers.get('comment:resolved');
    expect(handler).toBeDefined();
    act(() => {
      handler!({
        comment: {
          id: 'c1',
          issue_id: 'issue-1',
          author_type: 'member',
          author_id: 'u',
          content: 'hello',
          parent_id: null,
          created_at: '2026-05-06T01:00:00Z',
          updated_at: '2026-05-06T01:00:00Z',
          type: 'comment',
          reactions: [],
          attachments: [],
          resolved_at: '2026-05-06T03:00:00Z',
          resolved_by_type: 'member',
          resolved_by_id: 'u',
        },
      });
    });
    const updated = cacheUpdates.last as Array<{
      id: string;
      resolved_at: string | null;
      resolved_by_type: string | null;
      resolved_by_id: string | null;
    }>;
    expect(updated.map((e) => e.id)).toEqual(['c1', 'c2']);
    expect(updated[0]!.resolved_at).toBe('2026-05-06T03:00:00Z');
    expect(updated[0]!.resolved_by_type).toBe('member');
    expect(updated[0]!.resolved_by_id).toBe('u');
    expect(updated[1]!.resolved_at).toBeNull();
  });

  it('comment:unresolved clears the resolved fields on the matching entry', () => {
    queryState.data = [
      {
        type: 'comment',
        id: 'c1',
        actor_type: 'member',
        actor_id: 'u',
        content: 'hello',
        parent_id: null,
        created_at: '2026-05-06T01:00:00Z',
        updated_at: '2026-05-06T01:00:00Z',
        reactions: [],
        attachments: [],
        resolved_at: '2026-05-06T03:00:00Z',
        resolved_by_type: 'member',
        resolved_by_id: 'u',
      },
    ];
    renderHook(() => useIssueTimeline('issue-1', 'user-1'));
    const handler = wsHandlers.get('comment:unresolved');
    expect(handler).toBeDefined();
    act(() => {
      handler!({
        comment: {
          id: 'c1',
          issue_id: 'issue-1',
          author_type: 'member',
          author_id: 'u',
          content: 'hello',
          parent_id: null,
          created_at: '2026-05-06T01:00:00Z',
          updated_at: '2026-05-06T01:00:00Z',
          type: 'comment',
          reactions: [],
          attachments: [],
          resolved_at: null,
          resolved_by_type: null,
          resolved_by_id: null,
        },
      });
    });
    const updated = cacheUpdates.last as Array<{
      id: string;
      resolved_at: string | null;
    }>;
    expect(updated[0]!.resolved_at).toBeNull();
  });

  it('comment:resolved ignores events from other issues', () => {
    queryState.data = [
      {
        type: 'comment',
        id: 'c1',
        actor_type: 'member',
        actor_id: 'u',
        content: 'hello',
        parent_id: null,
        created_at: '2026-05-06T01:00:00Z',
        updated_at: '2026-05-06T01:00:00Z',
        reactions: [],
        attachments: [],
        resolved_at: null,
        resolved_by_type: null,
        resolved_by_id: null,
      },
    ];
    renderHook(() => useIssueTimeline('issue-1', 'user-1'));
    const handler = wsHandlers.get('comment:resolved');
    act(() => {
      handler!({
        comment: {
          id: 'c1',
          issue_id: 'different-issue',
          author_type: 'member',
          author_id: 'u',
          content: 'hello',
          parent_id: null,
          created_at: '2026-05-06T01:00:00Z',
          updated_at: '2026-05-06T01:00:00Z',
          type: 'comment',
          reactions: [],
          attachments: [],
          resolved_at: '2026-05-06T03:00:00Z',
          resolved_by_type: 'member',
          resolved_by_id: 'u',
        },
      });
    });
    expect(cacheUpdates.last).toBeNull();
  });
});
