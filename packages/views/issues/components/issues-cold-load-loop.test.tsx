// Регрессионные тесты цикла рендера при холодной загрузке маршрута задач
// с реальным react-virtuoso.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { BoardView } from './board-view';
import { SwimLaneView } from './swimlane-view';
import { IssueContextMenuProvider } from '../actions';
import { setApiInstance } from '@goosar/core/api';
import type { ApiClient } from '@goosar/core/api/client';
import type { Issue } from '@goosar/core/types';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enIssues from '../../locales/en/issues.json';

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

vi.mock('@goosar/core/hooks', () => ({
  useWorkspaceId: () => 'ws-1',
}));

vi.mock('@goosar/core/paths', async () => {
  const actual = await vi.importActual<typeof import('@goosar/core/paths')>('@goosar/core/paths');
  return {
    ...actual,
    useWorkspaceSlug: () => 'acme',
    useRequiredWorkspaceSlug: () => 'acme',
    useWorkspacePaths: () => actual.paths.workspace('acme'),
  };
});

const mockAuthUser = { id: 'user-1', email: 'test@test.com', name: 'Test User' };
vi.mock('@goosar/core/auth', () => ({
  useAuthStore: Object.assign(
    (selector?: any) => {
      const state = { user: mockAuthUser, isAuthenticated: true };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: mockAuthUser, isAuthenticated: true }) },
  ),
  registerAuthStore: vi.fn(),
  createAuthStore: vi.fn(),
}));

vi.mock('../../navigation', () => ({
  AppLink: ({ children, href, ...props }: any) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useNavigation: () => ({ push: vi.fn(), pathname: '/issues' }),
  NavigationProvider: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock('@goosar/core/issues/config', () => ({
  ALL_STATUSES: ['backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled'],
  STATUS_ORDER: ['backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled'],
  STATUS_CONFIG: {
    backlog: { label: 'Backlog', iconColor: 'text-muted-foreground', hoverBg: 'hover:bg-accent' },
    todo: { label: 'Todo', iconColor: 'text-muted-foreground', hoverBg: 'hover:bg-accent' },
    in_progress: {
      label: 'In Progress',
      iconColor: 'text-warning',
      hoverBg: 'hover:bg-warning/10',
    },
    in_review: { label: 'In Review', iconColor: 'text-success', hoverBg: 'hover:bg-success/10' },
    done: { label: 'Done', iconColor: 'text-info', hoverBg: 'hover:bg-info/10' },
    blocked: {
      label: 'Blocked',
      iconColor: 'text-destructive',
      hoverBg: 'hover:bg-destructive/10',
    },
    cancelled: {
      label: 'Cancelled',
      iconColor: 'text-muted-foreground',
      hoverBg: 'hover:bg-accent',
    },
  },
  PRIORITY_ORDER: ['urgent', 'high', 'medium', 'low', 'none'],
  PRIORITY_CONFIG: {
    urgent: { label: 'Urgent', bars: 4, color: 'text-destructive' },
    high: { label: 'High', bars: 3, color: 'text-warning' },
    medium: { label: 'Medium', bars: 2, color: 'text-warning' },
    low: { label: 'Low', bars: 1, color: 'text-info' },
    none: { label: 'No priority', bars: 0, color: 'text-muted-foreground' },
  },
}));

const mockLoadMore = vi.fn();
const loadMoreResult = {
  total: 0,
  loaded: 0,
  hasMore: false,
  isLoading: false,
  loadMore: mockLoadMore,
};
vi.mock('@goosar/core/issues/mutations', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/issues/mutations')>();
  return {
    ...actual,
    useLoadMoreByStatus: () => loadMoreResult,
    useLoadMoreByAssigneeGroup: () => loadMoreResult,
  };
});

vi.mock('@goosar/core/properties', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/properties')>();
  return {
    ...actual,
    useSetIssueProperty: () => ({ mutate: vi.fn(), mutateAsync: vi.fn() }),
    useUnsetIssueProperty: () => ({ mutate: vi.fn(), mutateAsync: vi.fn() }),
  };
});

const mockViewState: Record<string, unknown> = {
  grouping: 'status',
  sortBy: 'position',
  sortDirection: 'asc',
  cardProperties: {
    priority: true,
    assignee: true,
    dueDate: true,
    project: true,
    childProgress: true,
    labels: true,
  },
  swimlaneGrouping: 'assignee',
  swimlaneOrders: { parent: [], project: [], assignee: [] },
  collapsedSwimlanes: { parent: [], project: [], assignee: [] },
  setSwimlaneGrouping: vi.fn(),
  setSwimlaneOrder: vi.fn(),
  toggleSwimlaneCollapsed: vi.fn(),
  hideStatus: vi.fn(),
  showStatus: vi.fn(),
  priorityFilters: [],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  projectFilters: [],
  includeNoProject: false,
  labelFilters: [],
  propertyFilters: {},
  cardPropertyIds: [],
  agentRunningFilter: false,
};
vi.mock('@goosar/core/issues/stores/view-store-context', () => ({
  ViewStoreProvider: ({ children }: { children: ReactNode }) => children,
  useViewStore: (selector?: any) => (selector ? selector(mockViewState) : mockViewState),
  useViewStoreApi: () => ({ getState: () => mockViewState, setState: vi.fn(), subscribe: vi.fn() }),
}));

vi.mock('@goosar/core/modals', () => ({
  useModalStore: Object.assign(() => ({ open: vi.fn() }), { getState: () => ({ open: vi.fn() }) }),
}));

vi.mock('@dnd-kit/core', () => ({
  DndContext: ({ children }: any) => children,
  DragOverlay: () => null,
  PointerSensor: class {},
  useSensor: () => ({}),
  useSensors: () => [],
  useDroppable: () => ({ setNodeRef: vi.fn(), isOver: false }),
  pointerWithin: vi.fn(),
  closestCenter: vi.fn(),
}));

vi.mock('@dnd-kit/sortable', () => ({
  SortableContext: ({ children }: any) => children,
  verticalListSortingStrategy: {},
  arrayMove: <T,>(arr: T[]): T[] => arr.slice(),
  useSortable: () => ({
    attributes: {},
    listeners: {},
    setNodeRef: vi.fn(),
    transform: null,
    transition: null,
    isDragging: false,
  }),
}));

vi.mock('@dnd-kit/utilities', () => ({
  CSS: { Transform: { toString: () => undefined } },
}));

const pending = () => new Promise<never>(() => {});

function makeIssue(overrides: Partial<Issue> & { id: string }): Issue {
  return {
    workspace_id: 'ws-1',
    number: 1,
    identifier: `PROJ-${overrides.id}`,
    title: `Issue ${overrides.id}`,
    description: null,
    status: 'todo',
    priority: 'none',
    assignee_type: null,
    assignee_id: null,
    creator_type: 'member',
    creator_id: 'user-1',
    parent_issue_id: null,
    project_id: null,
    position: 100,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function renderWithProviders(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <IssueContextMenuProvider>{ui}</IssueContextMenuProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe('Issues cold-load render loop (MUL-4985)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockViewState.grouping = 'status';
    mockViewState.swimlaneGrouping = 'assignee';
    setApiInstance({
      listMembers: pending,
      listAgents: pending,
      listSquads: pending,
      getAgentTaskSnapshot: () => Promise.resolve([]),
      listChildrenByParents: () => Promise.resolve({ issues: [] }),
      listProjects: pending,
      getBaseUrl: () => '',
    } as unknown as ApiClient);
  });

  it('Board with a large column paints during cold load (real Virtuoso mounts, no update-depth loop)', async () => {
    const issues = Array.from({ length: 40 }, (_, i) =>
      makeIssue({ id: `b${i}`, title: `Board Card ${i}`, status: 'todo', position: 100 + i }),
    );

    renderWithProviders(
      <BoardView
        issues={issues}
        visibleStatuses={['todo', 'in_progress', 'done']}
        hiddenStatuses={[]}
        onMoveIssue={vi.fn()}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText('Todo')).toBeInTheDocument();
    });
    expect(screen.getByText('Board Card 0')).toBeInTheDocument();
  });

  it('Swimlane grouped by assignee paints during cold load (real Virtuoso mounts, no update-depth loop)', async () => {
    mockViewState.swimlaneGrouping = 'assignee';
    const issues = [
      makeIssue({
        id: 's1',
        title: 'Swim Card 1',
        assignee_type: 'member',
        assignee_id: 'user-1',
        status: 'todo',
      }),
      makeIssue({
        id: 's2',
        title: 'Swim Card 2',
        assignee_type: 'agent',
        assignee_id: 'agent-1',
        status: 'in_progress',
      }),
      makeIssue({
        id: 's3',
        title: 'Swim Card 3',
        assignee_type: null,
        assignee_id: null,
        status: 'todo',
      }),
    ];

    renderWithProviders(<SwimLaneView issues={issues} onMoveIssue={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByText('Swim Card 1')).toBeInTheDocument();
    });
    expect(screen.getByText('Swim Card 3')).toBeInTheDocument();
  });
});
