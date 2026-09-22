// Тесты поллинга фасетов: без них колонки списка и доски теряют итоги.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { setApiInstance } from '@goosar/core/api';
import type { ApiClient } from '@goosar/core/api/client';
import {
  getIssueSurfaceViewStore,
  pruneIssueSurfaceViewStates,
} from '@goosar/core/issues/stores/surface-view-store';
import { ViewStoreProvider } from '@goosar/core/issues/stores/view-store-context';
import type { ListIssuesParams, ListIssuesResponse } from '@goosar/core/types';

interface CapturedQuery {
  queryKey: readonly unknown[];
  refetchInterval?: unknown;
}

const h = vi.hoisted(() => ({
  degraded: false,
  queries: [] as CapturedQuery[],
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
    useQuery: (options: CapturedQuery) => {
      h.queries.push(options);
      return (actual.useQuery as (o: unknown) => unknown)(options);
    },
  };
});

vi.mock('@goosar/core/hooks', () => ({
  useWorkspaceId: () => 'ws-1',
}));

vi.mock('@goosar/core/issues/mutations', () => ({
  useUpdateIssue: () => ({ mutate: vi.fn(), isPending: false }),
  useBatchUpdateIssues: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useBatchDeleteIssues: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

vi.mock('@goosar/core/modals', () => ({
  useModalStore: { getState: () => ({ open: vi.fn() }) },
}));

vi.mock('../../i18n', () => ({
  useT: () => ({ t: () => 'translated' }),
}));

import { useIssueSurfaceController } from './use-issue-surface-controller';
import { statusTableMethodsFromLegacy } from './status-table-test-api';

const BACKGROUND = 60_000;

function never<T>() {
  return new Promise<T>(() => {});
}

function makeWrapper(qc: QueryClient, surfaceKey: string) {
  const store = getIssueSurfaceViewStore(surfaceKey);
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <ViewStoreProvider store={store}>{children}</ViewStoreProvider>
      </QueryClientProvider>
    );
  };
}

function facetsInterval(): unknown {
  const match = h.queries.find((entry) => entry.queryKey.includes('facets'));
  return match?.refetchInterval;
}

function ganttInterval(): unknown {
  const match = h.queries.find((entry) => entry.queryKey.includes('project-gantt'));
  return match?.refetchInterval;
}

function projectListInterval(): unknown {
  const match = h.queries.find(
    (entry) => entry.queryKey[0] === 'projects' && entry.queryKey.includes('list'),
  );
  return match?.refetchInterval;
}

function childProgressInterval(): unknown {
  const match = h.queries.find((entry) => entry.queryKey.includes('child-progress'));
  return match?.refetchInterval;
}

let qc: QueryClient;

beforeEach(() => {
  h.degraded = false;
  h.queries = [];
  qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const listIssues = vi.fn(() => never<ListIssuesResponse>()) as unknown as (
    params?: ListIssuesParams,
  ) => Promise<ListIssuesResponse>;
  setApiInstance({
    listIssues,
    ...statusTableMethodsFromLegacy(listIssues),
    listGroupedIssues: vi.fn(() => never()),
    listProjects: vi.fn(() => never()),
    getAgentTaskSnapshot: vi.fn(() => never()),
    getWorkspaceWorkingAgents: vi.fn(() => Promise.resolve([])),
    getChildIssueProgress: vi.fn(() => never()),
  } as unknown as ApiClient);
  pruneIssueSurfaceViewStates([]);
});

afterEach(() => {
  cleanup();
  qc.clear();
  pruneIssueSurfaceViewStates([]);
});

async function renderController(surfaceKey: string) {
  renderHook(
    () =>
      useIssueSurfaceController({
        scope: { type: 'project', projectId: 'p1' },
        modes: ['board', 'list', 'swimlane', 'gantt'],
      }),
    { wrapper: makeWrapper(qc, surfaceKey) },
  );
  await waitFor(() => expect(facetsInterval()).not.toBeUndefined());
}

describe('useIssueSurfaceController facets — degraded REST fallback (#257)', () => {
  it('does not poll facets while the realtime connection is healthy', async () => {
    await renderController('project:p1-facets-healthy');
    expect(facetsInterval()).toBe(false);
  });

  it('polls facets on the background cadence while degraded', async () => {
    h.degraded = true;
    await renderController('project:p1-facets-degraded');
    expect(facetsInterval()).toBe(BACKGROUND);
  });
});

describe('useIssueSurfaceController child progress — degraded REST fallback (#257)', () => {
  it('does not poll the sub-issue progress map while the connection is healthy', async () => {
    await renderController('project:p1-progress-healthy');
    expect(childProgressInterval()).toBe(false);
  });

  it('polls the sub-issue progress map on the background cadence while degraded', async () => {
    h.degraded = true;
    await renderController('project:p1-progress-degraded');
    expect(childProgressInterval()).toBe(BACKGROUND);
  });
});

describe('useIssueSurfaceController gantt — degraded REST fallback (#257)', () => {
  async function renderGantt(surfaceKey: string) {
    getIssueSurfaceViewStore(surfaceKey).getState().setViewMode('gantt');
    renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: 'project', projectId: 'p1' },
          modes: ['board', 'list', 'swimlane', 'gantt'],
        }),
      { wrapper: makeWrapper(qc, surfaceKey) },
    );
    await waitFor(() => expect(ganttInterval()).not.toBeUndefined());
  }

  it('does not poll the gantt canvas while the realtime connection is healthy', async () => {
    await renderGantt('project:p1-gantt-healthy');
    expect(ganttInterval()).toBe(false);
  });

  it('polls the gantt canvas on the background cadence while degraded', async () => {
    h.degraded = true;
    await renderGantt('project:p1-gantt-degraded');
    expect(ganttInterval()).toBe(BACKGROUND);
  });
});

describe('useIssueSurfaceController projects — degraded REST fallback (#257)', () => {
  async function renderSwimlane(surfaceKey: string, grouping: 'project' | 'assignee') {
    const store = getIssueSurfaceViewStore(surfaceKey);
    store.getState().setViewMode('swimlane');
    store.getState().setSwimlaneGrouping(grouping);
    renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: 'project', projectId: 'p1' },
          modes: ['board', 'list', 'swimlane', 'gantt'],
        }),
      { wrapper: makeWrapper(qc, surfaceKey) },
    );
    await waitFor(() => expect(projectListInterval()).not.toBeUndefined());
  }

  it('polls the project list while degraded when it is the lane set', async () => {
    h.degraded = true;
    await renderSwimlane('project:p1-projects-degraded', 'project');
    expect(projectListInterval()).toBe(BACKGROUND);
  });

  it('does not poll the project list while the connection is healthy', async () => {
    await renderSwimlane('project:p1-projects-healthy', 'project');
    expect(projectListInterval()).toBe(false);
  });

  it('leaves the project list unpolled where it is only a card label', async () => {
    h.degraded = true;
    await renderSwimlane('project:p1-projects-label', 'assignee');
    expect(projectListInterval()).toBe(false);
  });
});
