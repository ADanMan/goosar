// Тесты запасного REST-поллинга для главного экрана продукта.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { setApiInstance } from '@goosar/core/api';
import type { ApiClient } from '@goosar/core/api/client';
import type { Issue, IssueTableQuerySpec, IssueTableRowsRequest } from '@goosar/core/types';

interface CapturedQuery {
  queryKey: readonly unknown[];
  refetchInterval?: unknown;
}

const h = vi.hoisted(() => ({
  degraded: false,
  pageQueries: [] as CapturedQuery[],
  infiniteQueries: [] as CapturedQuery[],
}));

vi.mock('@goosar/core/realtime', () => ({
  DEFAULT_DEGRADED_POLL_INTERVAL_MS: 15_000,
  BACKGROUND_DEGRADED_POLL_INTERVAL_MS: 60_000,
  useRealtimePollingInterval: (ms: number) => (h.degraded ? ms : false),
}));

vi.mock('@tanstack/react-query', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-query')>();
  return {
    ...actual,
    useQueries: (args: { queries: CapturedQuery[] }) => {
      h.pageQueries = args.queries;
      return (actual.useQueries as (a: unknown) => unknown)(args);
    },
    useInfiniteQuery: (options: CapturedQuery) => {
      h.infiniteQueries.push(options);
      return (actual.useInfiniteQuery as (o: unknown) => unknown)(options);
    },
  };
});

import { useIssueStatusBranches } from './use-issue-status-branches';
import { useIssueGroupBranches } from './use-issue-group-branches';
import { BACKGROUND_DEGRADED_POLL_INTERVAL_MS } from '@goosar/core/realtime';

const BACKGROUND = BACKGROUND_DEGRADED_POLL_INTERVAL_MS;

function makeIssue(id: string): Issue {
  return {
    id,
    workspace_id: 'ws-1',
    number: 1,
    identifier: 'MUL-1',
    title: id,
    description: null,
    status: 'todo',
    priority: 'none',
    assignee_type: null,
    assignee_id: null,
    creator_type: 'member',
    creator_id: 'user-1',
    parent_issue_id: null,
    project_id: null,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };
}

const query: IssueTableQuerySpec = {
  scope: { kind: 'workspace' },
  filters: { include_sub_issues: true },
  sort: { field: 'position', direction: 'asc' },
};

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

function cursorOf(entry: CapturedQuery): unknown {
  return entry.queryKey[entry.queryKey.length - 1];
}

function intervalsByCursor(): Map<unknown, unknown> {
  const byCursor = new Map<unknown, unknown>();
  for (const entry of h.pageQueries) {
    byCursor.set(cursorOf(entry), entry.refetchInterval);
  }
  return byCursor;
}

function intervalsByGroupKey(): Map<unknown, unknown> {
  const byGroup = new Map<unknown, unknown>();
  for (const entry of h.pageQueries) {
    if (cursorOf(entry) !== null) continue;
    const key = entry.queryKey as unknown[];
    byGroup.set(key[key.length - 4], entry.refetchInterval);
  }
  return byGroup;
}

const EMPTY_GROUP_KEY = 'assignee:empty';

function rowsResponder() {
  return vi.fn(async (request: IssueTableRowsRequest) => {
    if (request.group_key === EMPTY_GROUP_KEY) {
      return {
        query_fingerprint: 'test',
        group_key: request.group_key,
        parent_id: null,
        total: 0,
        rows: [],
        branch_total: 0,
        next_cursor: null,
      };
    }
    const continuation = request.page?.cursor === 'cursor-2';
    return {
      query_fingerprint: 'test',
      group_key: request.group_key,
      parent_id: null,
      total: 1,
      rows: [
        {
          issue: makeIssue(continuation ? 'issue-2' : 'issue-1'),
          direct_child_count: 0,
        },
      ],
      branch_total: 2,
      next_cursor: continuation ? null : 'cursor-2',
    };
  });
}

interface PageShape {
  rows: string[];
  next_cursor: string | null;
}

function rowsPage(request: IssueTableRowsRequest, page: PageShape) {
  return {
    query_fingerprint: 'test',
    group_key: request.group_key,
    parent_id: null,
    total: page.rows.length,
    rows: page.rows.map((id) => ({
      issue: makeIssue(id),
      direct_child_count: 0,
    })),
    branch_total: 2,
    next_cursor: page.next_cursor,
  };
}

const GROUPS_RESPONSE = {
  query_fingerprint: 'test',
  total: 2,
  groups: [
    {
      key: 'assignee:unassigned',
      value: { kind: 'assignee' as const, actor: null },
      count: 2,
    },
  ],
  next_cursor: null,
};

async function pollHeadOnce(
  queryClient: QueryClient,
  head: PageShape = { rows: ['issue-1'], next_cursor: 'cursor-2' },
) {
  const entry = queryClient
    .getQueryCache()
    .getAll()
    .find((query) => {
      const key = query.queryKey as unknown[];
      return key.includes('rows') && key[key.length - 1] === null;
    });
  if (!entry) throw new Error('no head row-page query is in the cache');
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  setApiInstance({
    listIssueTableGroups: vi.fn(async () => GROUPS_RESPONSE),
    listIssueTableRows: vi.fn(async (request: IssueTableRowsRequest) => {
      await gate;
      return rowsPage(request, head);
    }),
  } as unknown as ApiClient);
  const inflight = queryClient.refetchQueries({
    queryKey: entry.queryKey,
    exact: true,
  });
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  await act(async () => {
    release();
    await inflight;
  });
}

async function refreshAllPages(
  queryClient: QueryClient,
  pages: { head: PageShape; tail: PageShape },
) {
  setApiInstance({
    listIssueTableGroups: vi.fn(async () => GROUPS_RESPONSE),
    listIssueTableRows: vi.fn(async (request: IssueTableRowsRequest) =>
      rowsPage(request, request.page?.cursor == null ? pages.head : pages.tail),
    ),
  } as unknown as ApiClient);
  await act(async () => {
    await queryClient.refetchQueries({ type: 'active' });
  });
}

async function refreshAllPagesInFlight(
  queryClient: QueryClient,
  pages: { head: PageShape; tail: PageShape },
  onInFlight: () => void,
) {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  setApiInstance({
    listIssueTableGroups: vi.fn(async () => GROUPS_RESPONSE),
    listIssueTableRows: vi.fn(async (request: IssueTableRowsRequest) => {
      await gate;
      return rowsPage(request, request.page?.cursor == null ? pages.head : pages.tail);
    }),
  } as unknown as ApiClient);
  const inflight = queryClient.refetchQueries({ type: 'active' });
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  onInFlight();
  await act(async () => {
    release();
    await inflight;
  });
}

beforeEach(() => {
  h.degraded = false;
  h.pageQueries = [];
  h.infiniteQueries = [];
});

afterEach(() => cleanup());

describe('useIssueStatusBranches — degraded REST fallback (#257)', () => {
  async function renderStatusBranches() {
    setApiInstance({
      listIssueTableRows: rowsResponder(),
    } as unknown as ApiClient);
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    const { result } = renderHook(
      () =>
        useIssueStatusBranches({
          wsId: 'ws-1',
          query,
          statuses: ['todo'],
          facets: undefined,
          facetsPending: false,
          facetsFetching: false,
          enabled: true,
        }),
      { wrapper: wrapper(queryClient) },
    );
    await waitFor(() => expect(result.current.issues).toHaveLength(1));
    act(() => result.current.pagination.todo.loadMore());
    await waitFor(() => expect(result.current.issues).toHaveLength(2));
    return { queryClient, result };
  }

  it('does not poll any page while the realtime connection is healthy', async () => {
    const { queryClient } = await renderStatusBranches();
    const byCursor = intervalsByCursor();
    expect(byCursor.get(null)).toBe(false);
    expect(byCursor.get('cursor-2')).toBe(false);
    queryClient.clear();
  });

  it('polls every loaded page of every status branch while degraded', async () => {
    h.degraded = true;
    const { queryClient } = await renderStatusBranches();
    const byCursor = intervalsByCursor();
    expect(byCursor.get(null)).toBe(BACKGROUND);
    expect(byCursor.get('cursor-2')).toBe(BACKGROUND);
    queryClient.clear();
  });

  it('keeps the pages the user scrolled when a poll tick returns the same head', async () => {
    h.degraded = true;
    const { queryClient, result } = await renderStatusBranches();
    await pollHeadOnce(queryClient);
    expect(result.current.issues).toHaveLength(2);
    queryClient.clear();
  });

  it('still drops the tails when a head refresh actually moves the window', async () => {
    h.degraded = true;
    const { queryClient, result } = await renderStatusBranches();
    await pollHeadOnce(queryClient, {
      rows: ['issue-new'],
      next_cursor: 'cursor-2',
    });
    await waitFor(() => expect(result.current.issues).toHaveLength(1));
    expect(result.current.issues[0]?.id).toBe('issue-new');
    queryClient.clear();
  });

  it('drops the tails when a cursor page moves under an unchanged head', async () => {
    const { queryClient, result } = await renderStatusBranches();
    await refreshAllPages(queryClient, {
      head: { rows: ['issue-1'], next_cursor: 'cursor-2' },
      tail: { rows: ['issue-3'], next_cursor: null },
    });
    await waitFor(() => expect(result.current.issues).toHaveLength(1));
    expect(result.current.issues[0]?.id).toBe('issue-1');
    queryClient.clear();
  });

  it('keeps the scrolled rows on screen while the whole chain is refetching', async () => {
    h.degraded = true;
    const { queryClient, result } = await renderStatusBranches();
    await refreshAllPagesInFlight(
      queryClient,
      {
        head: { rows: ['issue-1'], next_cursor: 'cursor-2' },
        tail: { rows: ['issue-2'], next_cursor: null },
      },
      () => expect(result.current.issues).toHaveLength(2),
    );
    expect(result.current.issues).toHaveLength(2);
    queryClient.clear();
  });

  it('keeps the tails when a broad refresh returns the same chain', async () => {
    const { queryClient, result } = await renderStatusBranches();
    await refreshAllPages(queryClient, {
      head: { rows: ['issue-1'], next_cursor: 'cursor-2' },
      tail: { rows: ['issue-2'], next_cursor: null },
    });
    expect(result.current.issues).toHaveLength(2);
    queryClient.clear();
  });
});

describe('useIssueGroupBranches — degraded REST fallback (#257)', () => {
  async function renderGroupBranches(
    groups: Array<{ key: string; count: number }> = [{ key: 'assignee:unassigned', count: 2 }],
  ) {
    setApiInstance({
      listIssueTableGroups: vi.fn(async () => ({
        query_fingerprint: 'test',
        total: 2,
        groups: groups.map((group) => ({
          key: group.key,
          value: { kind: 'assignee' as const, actor: null },
          count: group.count,
        })),
        next_cursor: null,
      })),
      listIssueTableRows: rowsResponder(),
    } as unknown as ApiClient);
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    const { result } = renderHook(
      () =>
        useIssueGroupBranches({
          wsId: 'ws-1',
          query,
          group: { kind: 'assignee' },
          observeEmptyBranches: true,
          enabled: true,
        }),
      { wrapper: wrapper(queryClient) },
    );
    await waitFor(() => expect(result.current.descriptors).toHaveLength(groups.length));
    return { queryClient, result };
  }

  async function loadTwoPages(
    result: { current: ReturnType<typeof useIssueGroupBranches> },
    key = 'assignee:unassigned',
  ) {
    act(() => result.current.pagination[key]!.loadMore());
    await waitFor(() => expect(result.current.issues).toHaveLength(1));
    act(() => result.current.pagination[key]!.loadMore());
    await waitFor(() => expect(result.current.issues).toHaveLength(2));
  }

  it('does not poll groups or pages while the realtime connection is healthy', async () => {
    const { queryClient, result } = await renderGroupBranches();
    await loadTwoPages(result);
    const byCursor = intervalsByCursor();
    expect(byCursor.get(null)).toBe(false);
    expect(byCursor.get('cursor-2')).toBe(false);
    expect(h.infiniteQueries.at(-1)?.refetchInterval).toBe(false);
    queryClient.clear();
  });

  it('polls the group catalog and every loaded page while degraded', async () => {
    h.degraded = true;
    const { queryClient, result } = await renderGroupBranches();
    await loadTwoPages(result);
    const byCursor = intervalsByCursor();
    expect(h.infiniteQueries.at(-1)?.refetchInterval).toBe(BACKGROUND);
    expect(byCursor.get(null)).toBe(BACKGROUND);
    expect(byCursor.get('cursor-2')).toBe(BACKGROUND);
    queryClient.clear();
  });

  it('keeps the pages the user scrolled when a poll tick returns the same head', async () => {
    h.degraded = true;
    const { queryClient, result } = await renderGroupBranches();
    await loadTwoPages(result);
    await pollHeadOnce(queryClient);
    expect(result.current.issues).toHaveLength(2);
    queryClient.clear();
  });

  it('does not poll a branch the catalog reports as empty', async () => {
    h.degraded = true;
    const { queryClient, result } = await renderGroupBranches([
      { key: 'assignee:unassigned', count: 2 },
      { key: 'assignee:empty', count: 0 },
    ]);
    act(() => result.current.pagination['assignee:unassigned']!.loadMore());
    act(() => result.current.pagination['assignee:empty']!.loadMore());
    await waitFor(() => expect(h.pageQueries.length).toBe(2));
    const byGroup = intervalsByGroupKey();
    expect(byGroup.get('assignee:unassigned')).toBe(BACKGROUND);
    expect(byGroup.get('assignee:empty')).toBe(false);
    queryClient.clear();
  });

  it('refetches a branch whose catalog count changed, without waiting for a tick', async () => {
    h.degraded = true;
    const { queryClient, result } = await renderGroupBranches([
      { key: 'assignee:unassigned', count: 2 },
      { key: 'assignee:empty', count: 0 },
    ]);
    act(() => result.current.pagination['assignee:empty']!.loadMore());
    await waitFor(() => expect(result.current.pagination['assignee:empty']!.total).toBe(0));
    expect(result.current.issues).toHaveLength(0);

    setApiInstance({
      listIssueTableGroups: vi.fn(async () => ({
        query_fingerprint: 'test',
        total: 3,
        groups: [
          {
            key: 'assignee:unassigned',
            value: { kind: 'assignee' as const, actor: null },
            count: 2,
          },
          {
            key: 'assignee:empty',
            value: { kind: 'assignee' as const, actor: null },
            count: 1,
          },
        ],
        next_cursor: null,
      })),
      listIssueTableRows: vi.fn(async (request: IssueTableRowsRequest) =>
        rowsPage(
          request,
          request.group_key === 'assignee:empty'
            ? { rows: ['issue-woken'], next_cursor: null }
            : { rows: [], next_cursor: null },
        ),
      ),
    } as unknown as ApiClient);
    await act(async () => {
      await queryClient.refetchQueries({
        predicate: (entry) => entry.queryKey.includes('groups'),
      });
    });
    await waitFor(() =>
      expect(result.current.issues.map((issue) => issue.id)).toContain('issue-woken'),
    );
    queryClient.clear();
  });
});
