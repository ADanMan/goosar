// Тесты REST-поллинга доски в деградированном режиме: каталог свойств
// должен оставаться доступным.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ViewStoreProvider } from '@goosar/core/issues/stores/view-store-context';
import { getIssueSurfaceViewStore } from '@goosar/core/issues/stores/surface-view-store';
import type { ReactNode } from 'react';
import { renderWithI18n } from '../../test/i18n';

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

const PROPERTY_ID = 'prop-1';

vi.mock('@goosar/core/properties', () => ({
  propertyListOptions: (wsId: string, includeArchived = false) => ({
    queryKey: ['properties', wsId, 'list', includeArchived],
    queryFn: async () => [
      {
        id: PROPERTY_ID,
        name: 'Stage',
        type: 'select',
        config: { options: [{ id: 'opt-1', name: 'Draft', color: 'gray' }] },
      },
    ],
  }),
  useSetIssueProperty: () => ({ mutate: vi.fn(), isPending: false }),
  useUnsetIssueProperty: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock('@goosar/core/hooks', () => ({
  useWorkspaceId: () => 'ws-1',
}));

vi.mock('@goosar/core/workspace/hooks', () => ({
  useActorName: () => ({ getActorName: () => 'Someone' }),
  buildActorNameResolver: () => () => 'Someone',
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

import { BoardView } from './board-view';

function propertyCatalogInterval(): unknown {
  const match = h.queries.find((entry) => entry.queryKey[0] === 'properties');
  return match?.refetchInterval;
}

let qc: QueryClient;

beforeEach(() => {
  h.degraded = false;
  h.queries = [];
  qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
});

afterEach(() => {
  cleanup();
  qc.clear();
});

async function renderBoard(surfaceKey: string, grouping: string) {
  const store = getIssueSurfaceViewStore(surfaceKey);
  store.getState().setGrouping(grouping as never);
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <ViewStoreProvider store={store}>{children}</ViewStoreProvider>
      </QueryClientProvider>
    );
  }
  renderWithI18n(
    <Wrapper>
      <BoardView issues={[]} visibleStatuses={[]} hiddenStatuses={[]} onMoveIssue={() => {}} />
    </Wrapper>,
  );
  await waitFor(() => expect(propertyCatalogInterval()).not.toBeUndefined());
}

describe('BoardView property columns — degraded REST fallback (#257)', () => {
  it('polls the property catalog while degraded when it is the column set', async () => {
    h.degraded = true;
    await renderBoard('board-prop-degraded', `property:${PROPERTY_ID}`);
    expect(propertyCatalogInterval()).toBe(60_000);
  });

  it('does not poll the property catalog while the connection is healthy', async () => {
    await renderBoard('board-prop-healthy', `property:${PROPERTY_ID}`);
    expect(propertyCatalogInterval()).toBe(false);
  });

  it('leaves the catalog unpolled when the columns come from status', async () => {
    h.degraded = true;
    await renderBoard('board-status-degraded', 'status');
    expect(propertyCatalogInterval()).toBe(false);
  });
});
