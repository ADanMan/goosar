// Тесты REST-поллинга таблицы в деградированном режиме: опрос должен доходить
// до строк, которые видит пользователь.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { setApiInstance } from '@goosar/core/api';
import type { ApiClient } from '@goosar/core/api/client';
import { ViewStoreProvider } from '@goosar/core/issues/stores/view-store-context';
import { getIssueSurfaceViewStore } from '@goosar/core/issues/stores/surface-view-store';
import type { Issue, IssueTableQuerySpec } from '@goosar/core/types';
import { renderWithI18n } from '../../test/i18n';
import { IssueSurfaceSelectionProvider } from '../surface/selection-context';
import type { IssueSurfaceSelection } from '../surface/selection-context';

interface CapturedQuery {
  queryKey: readonly unknown[];
  refetchInterval?: unknown;
}

const h = vi.hoisted(() => ({
  degraded: false,
  branchQueries: [] as CapturedQuery[],
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
      h.branchQueries = args.queries;
      return (actual.useQueries as (a: unknown) => unknown)(args);
    },
  };
});

vi.mock('@goosar/core/hooks', () => ({
  useWorkspaceId: () => 'ws-1',
}));

vi.mock('@tanstack/react-virtual', () => ({
  useVirtualizer: (options: { count: number; getItemKey?: (index: number) => unknown }) => ({
    getVirtualItems: () =>
      Array.from({ length: options.count }, (_, index) => ({
        index,
        key: options.getItemKey?.(index) ?? index,
        start: index * 41,
        end: (index + 1) * 41,
        size: 41,
        lane: 0,
      })),
    getTotalSize: () => options.count * 41,
    measureElement: () => {},
  }),
}));

vi.mock('@goosar/core/workspace/hooks', () => ({
  useActorName: () => ({ getActorName: () => 'Someone' }),
  buildActorNameResolver: () => () => 'Someone',
}));

const mockAuthUser = { id: 'user-1', email: 't@t.co', name: 'Tester' };
vi.mock('@goosar/core/auth', () => ({
  useAuthStore: Object.assign(
    (selector?: (state: unknown) => unknown) => {
      const state = { user: mockAuthUser, isAuthenticated: true };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: mockAuthUser, isAuthenticated: true }) },
  ),
}));

vi.mock('../../navigation', () => ({
  AppLink: ({ children, ...props }: React.ComponentProps<'a'>) => <a {...props}>{children}</a>,
  useNavigation: () => ({
    push: vi.fn(),
    openInNewTab: vi.fn(),
    getShareableUrl: (path: string) => `https://app.example${path}`,
    pathname: '/',
  }),
}));

vi.mock('@goosar/core/paths', async () => {
  const actual = await vi.importActual<typeof import('@goosar/core/paths')>('@goosar/core/paths');
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace('test'),
  };
});

import { TableView } from './table-view';

const BACKGROUND = 60_000;

class ObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

function makeIssue(id: string): Issue {
  return {
    id,
    workspace_id: 'ws-1',
    number: 1,
    identifier: `MUL-${id}`,
    title: id,
    description: null,
    status: 'todo',
    priority: 'none',
    assignee_type: null,
    assignee_id: null,
    creator_type: 'member',
    creator_id: 'member-1',
    parent_issue_id: null,
    project_id: null,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
    properties: {},
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };
}

const selection: IssueSurfaceSelection = {
  selectedIds: new Set<string>(),
  toggle: () => {},
  select: () => {},
  deselect: () => {},
  clear: () => {},
};

const serverQuery: IssueTableQuerySpec = {
  scope: { kind: 'workspace' },
  filters: {},
  sort: { field: 'position', direction: 'asc' },
};

function Harness({ surfaceKey }: { surfaceKey: string }) {
  return (
    <ViewStoreProvider store={getIssueSurfaceViewStore(surfaceKey)}>
      <IssueSurfaceSelectionProvider selection={selection}>
        <TableView
          serverQuery={serverQuery}
          childProgressMap={new Map()}
          search=""
          onSearchChange={() => {}}
          onLoadedIssuesChange={() => {}}
          onCreateIssue={() => {}}
          exportIssues={() => Promise.resolve([])}
          resolveExportLookups={() =>
            Promise.resolve({
              projectMap: new Map(),
              childProgressMap: new Map(),
            })
          }
        />
      </IssueSurfaceSelectionProvider>
    </ViewStoreProvider>
  );
}

function branchOf(entry: CapturedQuery) {
  const { queryKey } = entry;
  return {
    parentId: (queryKey[queryKey.length - 3] as { parentId: string | null }).parentId,
    cursor: queryKey[queryKey.length - 1],
  };
}

function intervalForParent(parentId: string | null): unknown {
  const match = h.branchQueries.find((entry) => {
    const branch = branchOf(entry);
    return branch.parentId === parentId && branch.cursor === null;
  });
  return match?.refetchInterval;
}

let queryClient: QueryClient;

beforeEach(() => {
  h.degraded = false;
  h.branchQueries = [];
  vi.stubGlobal('IntersectionObserver', ObserverStub);
  vi.stubGlobal('ResizeObserver', ObserverStub);
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  setApiInstance({
    listProperties: async () => ({ properties: [] }),
    listMembers: async () => [],
    listAgents: async () => [],
    listSquads: async () => [],
    getAssigneeFrequency: async () => [],
    listIssueTableRows: async (request: { parent_id: string | null }) => ({
      query_fingerprint: 'test',
      group_key: null,
      parent_id: request.parent_id,
      total: 1,
      rows:
        request.parent_id === null
          ? [{ issue: makeIssue('parent-1'), direct_child_count: 1 }]
          : [{ issue: makeIssue('child-1'), direct_child_count: 0 }],
      branch_total: 1,
      next_cursor: null,
    }),
  } as unknown as ApiClient);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  queryClient.clear();
});

async function renderWithExpandedChildBranch(surfaceKey: string) {
  const user = userEvent.setup();
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <Harness surfaceKey={surfaceKey} />
    </QueryClientProvider>,
  );
  await screen.findByText('parent-1');
  await user.click(await screen.findByRole('button', { name: 'Loading…' }));
  await screen.findByText('child-1');
  await waitFor(() => expect(intervalForParent('parent-1')).not.toBeUndefined());
}

describe('TableView degraded polling reaches every mounted branch (#257)', () => {
  const RENDER_BUDGET_MS = 20_000;

  it(
    'does not poll any branch while the realtime connection is healthy',
    async () => {
      await renderWithExpandedChildBranch('polling-healthy');
      expect(intervalForParent(null)).toBe(false);
      expect(intervalForParent('parent-1')).toBe(false);
    },
    RENDER_BUDGET_MS,
  );

  it(
    'polls the expanded sub-task branch as well as the top-level one',
    async () => {
      h.degraded = true;
      await renderWithExpandedChildBranch('polling-degraded');
      expect(intervalForParent(null)).toBe(BACKGROUND);
      expect(intervalForParent('parent-1')).toBe(BACKGROUND);
    },
    RENDER_BUDGET_MS,
  );
});
