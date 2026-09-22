// Регрессионные тесты: открытый редактор ячейки таблицы должен переживать
// постоянные обновления данных.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { setApiInstance } from '@goosar/core/api';
import type { ApiClient } from '@goosar/core/api/client';
import { issueKeys } from '@goosar/core/issues/queries';
import { ViewStoreProvider } from '@goosar/core/issues/stores/view-store-context';
import { getIssueSurfaceViewStore } from '@goosar/core/issues/stores/surface-view-store';
import type { Issue, IssueTableQuerySpec, IssueTableRowsResponse } from '@goosar/core/types';
import { renderWithI18n } from '../../test/i18n';
import { IssueSurfaceSelectionProvider } from '../surface/selection-context';
import type { IssueSurfaceSelection } from '../surface/selection-context';
import type { IssueCreateDefaults } from '../surface/types';
import type { ChildProgress } from './list-row';
import { TableView, useReleaseEditingCellOnUnmount } from './table-view';

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

const navigationMocks = vi.hoisted(() => ({
  push: vi.fn(),
  openInNewTab: vi.fn(),
  getShareableUrl: vi.fn((path: string) => `https://app.example${path}`),
}));
const navigationState = vi.hoisted(() => ({ hasOpenInNewTab: true }));

vi.mock('../../navigation', () => ({
  AppLink: ({ children, ...props }: React.ComponentProps<'a'>) => <a {...props}>{children}</a>,
  useNavigation: () => ({
    push: navigationMocks.push,
    openInNewTab: navigationState.hasOpenInNewTab ? navigationMocks.openInNewTab : undefined,
    getShareableUrl: navigationMocks.getShareableUrl,
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

class ObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

function makeIssue(id: string, title: string, status: Issue['status']): Issue {
  return {
    id,
    workspace_id: 'ws-1',
    number: 1,
    identifier: `MUL-${id}`,
    title,
    description: null,
    status,
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

let serverIssues: Issue[] = [];

function Harness({
  childProgressMap,
  surfaceKey,
  onCreateIssue = () => {},
}: {
  childProgressMap: Map<string, ChildProgress>;
  surfaceKey: string;
  onCreateIssue?: (defaults: IssueCreateDefaults) => void;
}) {
  return (
    <ViewStoreProvider store={getIssueSurfaceViewStore(surfaceKey)}>
      <IssueSurfaceSelectionProvider selection={selection}>
        <TableView
          serverQuery={serverQuery}
          childProgressMap={childProgressMap}
          search=""
          onSearchChange={() => {}}
          onLoadedIssuesChange={() => {}}
          onCreateIssue={onCreateIssue}
          exportIssues={() => Promise.resolve(serverIssues)}
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

describe('TableView cell editors under data refresh', () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    navigationMocks.push.mockReset();
    navigationMocks.openInNewTab.mockReset();
    navigationMocks.getShareableUrl.mockReset();
    navigationMocks.getShareableUrl.mockImplementation(
      (path: string) => `https://app.example${path}`,
    );
    navigationState.hasOpenInNewTab = true;
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
      listIssueTableRows: async () => ({
        query_fingerprint: 'test',
        group_key: null,
        parent_id: null,
        total: serverIssues.length,
        rows: serverIssues.map((issue) => ({
          issue,
          direct_child_count: 0,
        })),
        branch_total: serverIssues.length,
        next_cursor: null,
      }),
    } as unknown as ApiClient);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it('keeps the status picker open and the row order frozen across a refresh, then catches up on close', async () => {
    const user = userEvent.setup({ delay: null, pointerEventsCheck: 0 });
    const issueA = makeIssue('a', 'Alpha task', 'todo');
    const issueB = makeIssue('b', 'Beta task', 'in_progress');
    serverIssues = [issueA, issueB];
    const progress1 = new Map<string, ChildProgress>();
    const surfaceKey = `test-surface-${Math.floor(Math.random() * 1e9)}`;

    const view = renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <Harness childProgressMap={progress1} surfaceKey={surfaceKey} />
      </QueryClientProvider>,
    );

    const identifiers = () => screen.getAllByText(/^MUL-/).map((node) => node.textContent);
    await screen.findByText('MUL-a');
    expect(identifiers()).toEqual(['MUL-a', 'MUL-b']);

    const rowA = screen.getByText('MUL-a').closest('tr')!;
    await user.click(within(rowA).getByRole('button', { name: /Todo/ }));
    expect(screen.getByRole('button', { name: /Backlog/ })).toBeTruthy();

    const refreshedA = { ...issueA, title: 'Alpha task (updated)' };
    serverIssues = [issueB, refreshedA];
    view.rerender(
      <QueryClientProvider client={queryClient}>
        <Harness childProgressMap={new Map<string, ChildProgress>()} surfaceKey={surfaceKey} />
      </QueryClientProvider>,
    );
    act(() => {
      queryClient.setQueriesData<IssueTableRowsResponse>(
        { queryKey: issueKeys.tableAll('ws-1') },
        (previous) =>
          previous
            ? {
                ...previous,
                total: serverIssues.length,
                branch_total: serverIssues.length,
                rows: serverIssues.map((issue) => ({
                  issue,
                  direct_child_count: 0,
                })),
              }
            : previous,
      );
    });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Backlog/ })).toBeTruthy();
      expect(identifiers()).toEqual(['MUL-a', 'MUL-b']);
      expect(screen.getByText('Alpha task (updated)')).toBeTruthy();
    });

    await user.click(screen.getByRole('button', { name: /Backlog/ }));
    expect(screen.queryByRole('button', { name: /Backlog/ })).toBeNull();
    expect(identifiers()).toEqual(['MUL-b', 'MUL-a']);
  }, 60_000);

  it('opens creation with the row as parent and inherits its project', async () => {
    const user = userEvent.setup({ delay: null, pointerEventsCheck: 0 });
    const onCreateIssue = vi.fn();
    const issue = {
      ...makeIssue('a', 'Alpha task', 'todo'),
      project_id: 'project-1',
    };
    serverIssues = [issue];

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <Harness
          childProgressMap={new Map()}
          surfaceKey={`test-create-sub-issue-${Math.floor(Math.random() * 1e9)}`}
          onCreateIssue={onCreateIssue}
        />
      </QueryClientProvider>,
    );

    const row = (await screen.findByText('MUL-a')).closest('tr')!;
    await user.click(within(row).getByRole('button', { name: 'Create sub-issue' }));

    expect(onCreateIssue).toHaveBeenCalledWith({
      parent_issue_id: 'a',
      parent_issue_identifier: 'MUL-a',
      project_id: 'project-1',
    });
  });

  it('opens title and row clicks in a foreground Desktop tab', async () => {
    const user = userEvent.setup({ delay: null, pointerEventsCheck: 0 });
    serverIssues = [makeIssue('a', 'Alpha task', 'todo')];

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <Harness
          childProgressMap={new Map()}
          surfaceKey={`test-new-tab-${Math.floor(Math.random() * 1e9)}`}
        />
      </QueryClientProvider>,
    );

    const row = (await screen.findByText('MUL-a')).closest('tr')!;
    await user.click(within(row).getByRole('button', { name: 'Alpha task' }));
    expect(navigationMocks.openInNewTab).toHaveBeenCalledWith('/test/issues/a', 'MUL-a', {
      activate: true,
    });
    expect(navigationMocks.push).not.toHaveBeenCalled();

    navigationMocks.openInNewTab.mockClear();
    await user.click(row);
    expect(navigationMocks.openInNewTab).toHaveBeenCalledWith('/test/issues/a', 'MUL-a', {
      activate: true,
    });
    expect(navigationMocks.push).not.toHaveBeenCalled();
  });

  it('opens a real browser tab when the platform has no tab adapter', async () => {
    const user = userEvent.setup({ delay: null, pointerEventsCheck: 0 });
    const windowOpen = vi.fn();
    vi.stubGlobal('open', windowOpen);
    navigationState.hasOpenInNewTab = false;
    serverIssues = [makeIssue('a', 'Alpha task', 'todo')];

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <Harness
          childProgressMap={new Map()}
          surfaceKey={`test-browser-tab-${Math.floor(Math.random() * 1e9)}`}
        />
      </QueryClientProvider>,
    );

    const row = (await screen.findByText('MUL-a')).closest('tr')!;
    await user.click(within(row).getByRole('button', { name: 'Alpha task' }));

    expect(navigationMocks.getShareableUrl).toHaveBeenCalledWith('/test/issues/a');
    expect(windowOpen).toHaveBeenCalledWith(
      'https://app.example/test/issues/a',
      '_blank',
      'noopener,noreferrer',
    );
    expect(navigationMocks.push).not.toHaveBeenCalled();
  });
});

describe('useReleaseEditingCellOnUnmount', () => {
  function Probe({
    cellKey,
    editingCellKey,
    setEditingCellKey,
  }: {
    cellKey: string | null;
    editingCellKey: string | null;
    setEditingCellKey: (key: string | null) => void;
  }) {
    useReleaseEditingCellOnUnmount(cellKey, editingCellKey, setEditingCellKey);
    return null;
  }

  afterEach(cleanup);

  it('clears the key when the cell that owns the open editor unmounts', () => {
    const setEditingCellKey = vi.fn();
    const { unmount } = render(
      <Probe
        cellKey="issue-a:status"
        editingCellKey="issue-a:status"
        setEditingCellKey={setEditingCellKey}
      />,
    );

    unmount();

    expect(setEditingCellKey).toHaveBeenCalledWith(null);
  });

  it('leaves the key untouched when a different cell unmounts', () => {
    const setEditingCellKey = vi.fn();
    const { unmount } = render(
      <Probe
        cellKey="issue-b:status"
        editingCellKey="issue-a:status"
        setEditingCellKey={setEditingCellKey}
      />,
    );

    unmount();

    expect(setEditingCellKey).not.toHaveBeenCalled();
  });

  it('does not fire on mount while the cell is not yet the active editor', () => {
    const setEditingCellKey = vi.fn();
    const { rerender, unmount } = render(
      <Probe
        cellKey="issue-a:status"
        editingCellKey={null}
        setEditingCellKey={setEditingCellKey}
      />,
    );
    expect(setEditingCellKey).not.toHaveBeenCalled();

    rerender(
      <Probe
        cellKey="issue-a:status"
        editingCellKey="issue-a:status"
        setEditingCellKey={setEditingCellKey}
      />,
    );
    expect(setEditingCellKey).not.toHaveBeenCalled();

    unmount();
    expect(setEditingCellKey).toHaveBeenCalledTimes(1);
    expect(setEditingCellKey).toHaveBeenCalledWith(null);
  });
});
