/**
 * @vitest-environment jsdom
 *
 * Проверяет, что секция автопилотов не полагается только на realtime-
 * подписку: без REST-фолбэка индикатор соединения мог показывать спиннер
 * даже после того, как прогон уже завершился на бэкенде.
 *
 * Оба компонента прогоняются через `renderHook(() => Component())` — тело
 * хука выполняется целиком (это нужно, чтобы увидеть query-опции), а
 * возвращаемый JSX только строится, но не монтируется.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { renderHook } from '@testing-library/react';

const h = vi.hoisted(() => ({
  pollingInterval: false as number | false,
  captured: {} as Record<string, unknown>,
}));

vi.mock('@goosar/core/realtime', () => ({
  DEFAULT_DEGRADED_POLL_INTERVAL_MS: 15_000,
  BACKGROUND_DEGRADED_POLL_INTERVAL_MS: 60_000,
  useRealtimePollingInterval: (ms: number = 15_000) => (h.pollingInterval === false ? false : ms),
}));

vi.mock('@tanstack/react-query', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-query')>();
  return {
    ...actual,
    useQuery: (options: { queryKey: readonly unknown[]; refetchInterval?: unknown }) => {
      h.captured[String(options.queryKey[0])] = options.refetchInterval;
      return { data: undefined, isLoading: true, isPending: true };
    },
  };
});

vi.mock('../../i18n', () => ({
  useT: () => ({ t: () => 'x' }),
  useUiLocale: () => 'en',
}));
vi.mock('@goosar/core/hooks', () => ({ useWorkspaceId: () => 'ws-1' }));
vi.mock('@goosar/core/paths', () => ({
  useWorkspacePaths: () => ({ autopilots: () => '/acme/autopilots' }),
}));
vi.mock('../../navigation', () => ({
  useNavigation: () => ({ push: vi.fn(), pathname: '/acme/autopilots' }),
  useRowLink: () => () => ({ href: '#', onClick: vi.fn() }),
}));
vi.mock('@goosar/core/workspace/hooks', () => ({
  useActorName: () => ({ getActorName: () => 'Someone' }),
}));
vi.mock('@goosar/core/autopilots/mutations', () => ({
  useUpdateAutopilot: () => ({ mutateAsync: vi.fn() }),
  useDeleteAutopilot: () => ({ mutateAsync: vi.fn() }),
  useTriggerAutopilot: () => ({ mutateAsync: vi.fn() }),
  useCreateAutopilotTrigger: () => ({ mutateAsync: vi.fn() }),
  useDeleteAutopilotTrigger: () => ({ mutateAsync: vi.fn() }),
  useRotateAutopilotTriggerWebhookToken: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock('@goosar/core/autopilots/queries', () => ({
  autopilotDetailOptions: () => ({ queryKey: ['autopilot-detail'] }),
  autopilotRunsOptions: () => ({ queryKey: ['autopilot-runs'] }),
  autopilotRunOptions: () => ({ queryKey: ['autopilot-run'] }),
  autopilotListOptions: () => ({ queryKey: ['autopilot-list'] }),
}));
vi.mock('@goosar/core/projects/queries', () => ({
  projectDetailOptions: () => ({ queryKey: ['project-detail'] }),
}));

import { AutopilotDetailPage } from './autopilot-detail-page';
import { AutopilotsPage } from './autopilots-page';

beforeEach(() => {
  h.pollingInterval = false;
  h.captured = {};
});

describe('autopilots degraded REST fallback (#257)', () => {
  it('does not poll the run list while the realtime connection is healthy', () => {
    renderHook(() => AutopilotDetailPage({ autopilotId: 'ap-1' }));
    expect(h.captured['autopilot-runs']).toBe(false);
  });

  it('polls the run list on the focused cadence while degraded', () => {
    h.pollingInterval = 15_000;
    renderHook(() => AutopilotDetailPage({ autopilotId: 'ap-1' }));
    expect(h.captured['autopilot-runs']).toBe(15_000);
  });

  it('polls the autopilot list on the background cadence while degraded', () => {
    h.pollingInterval = 60_000;
    renderHook(() => AutopilotsPage());
    expect(h.captured['autopilot-list']).toBe(60_000);
  });

  it('does not poll the autopilot list while the realtime connection is healthy', () => {
    renderHook(() => AutopilotsPage());
    expect(h.captured['autopilot-list']).toBe(false);
  });
});
