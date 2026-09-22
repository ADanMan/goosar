import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

const state = vi.hoisted(() => ({
  user: null as { id: string } | null,
  isAuthLoading: false,
  overlay: null as { type: string } | null,
  workspace: null as { id: string; slug: string } | null,
  listFetched: true,
  wsList: [] as { id: string; slug: string }[],
  workspaceSeen: true,
  modalRenders: 0,
  modalAriaLabel: 'source-backfill-modal-marker',
  bannerRenders: 0,
  bannerTestId: 'missing-credentials-banner-marker',
  reminderRenders: 0,
  reminderTestId: 'provisioning-reminder-marker',
  setProvisioningWorkspace: vi.fn(),
}));

vi.mock('@goosar/core/auth', () => {
  const useAuthStore = (selector: (s: typeof state) => unknown) => {
    if (selector.toString().includes('isLoading')) return state.isAuthLoading;
    return state.user;
  };
  return { useAuthStore };
});

vi.mock('@goosar/core/platform', () => ({
  setCurrentWorkspace: vi.fn(),
}));

const mocks = vi.hoisted(() => ({
  setProvisioningWorkspace: vi.fn(),
  getProvisioningStatus: vi.fn(),
  retryProvisioning: vi.fn(),
}));

vi.mock('../platform/provisioning-bridge', () => ({
  setProvisioningWorkspace: mocks.setProvisioningWorkspace,
  getProvisioningStatus: mocks.getProvisioningStatus,
  retryProvisioning: mocks.retryProvisioning,
}));

vi.mock('@goosar/core/workspace', async () => {
  const actual =
    await vi.importActual<typeof import('@goosar/core/workspace')>('@goosar/core/workspace');
  return {
    ...actual,
    workspaceBySlugOptions: () => ({
      queryKey: ['workspace-by-slug'],
      queryFn: async () => state.workspace,
    }),
    workspaceListOptions: () => ({
      queryKey: ['workspace-list'],
      queryFn: async () => state.wsList,
    }),
  };
});

vi.mock('@goosar/core/paths', async () => {
  const actual = await vi.importActual<typeof import('@goosar/core/paths')>('@goosar/core/paths');
  return {
    ...actual,
    WorkspaceSlugProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
    paths: {
      ...actual.paths,
      login: () => '/login',
    },
  };
});

vi.mock('@goosar/views/workspace/use-workspace-seen', () => ({
  useWorkspaceSeen: () => state.workspaceSeen,
}));

vi.mock('@goosar/views/workspace/welcome-after-onboarding', () => ({
  WelcomeAfterOnboarding: () => null,
}));

vi.mock('@goosar/views/layout', () => ({
  WorkspacePresencePrefetch: () => null,
}));

vi.mock('@goosar/views/onboarding', () => ({
  SourceBackfillModal: () => {
    state.modalRenders += 1;
    return <div data-testid={state.modalAriaLabel} />;
  },
  ProvisioningReminder: () => {
    state.reminderRenders += 1;
    return <div data-testid={state.reminderTestId} />;
  },
}));

vi.mock('@goosar/views/capabilities', () => ({
  MissingCredentialsBanner: () => {
    state.bannerRenders += 1;
    return <div data-testid={state.bannerTestId} />;
  },
}));

vi.mock('@/stores/tab-store', () => ({
  useTabStore: Object.assign(() => null, {
    getState: () => ({ validateWorkspaceSlugs: vi.fn() }),
  }),
}));

vi.mock('@/stores/window-overlay-store', () => {
  const useWindowOverlayStore = (selector: (s: typeof state) => unknown) => selector(state);
  return { useWindowOverlayStore };
});

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { WorkspaceRouteLayout } from './workspace-route-layout';

function renderLayout() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  qc.setQueryData(['workspace-by-slug'], state.workspace);
  qc.setQueryData(['workspace-list'], state.wsList);
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/acme/issues']}>
        <Routes>
          <Route path=":workspaceSlug/*" element={<WorkspaceRouteLayout />}>
            <Route path="*" element={<div data-testid="outlet" />} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  state.user = { id: 'u1' };
  state.isAuthLoading = false;
  state.overlay = null;
  state.workspace = { id: 'ws-1', slug: 'acme' };
  state.listFetched = true;
  state.wsList = [{ id: 'ws-1', slug: 'acme' }];
  state.workspaceSeen = true;
  state.modalRenders = 0;
  state.bannerRenders = 0;
  state.reminderRenders = 0;
  mocks.getProvisioningStatus.mockResolvedValue(null);
  mocks.retryProvisioning.mockResolvedValue(undefined);
  state.setProvisioningWorkspace.mockClear();
  Object.defineProperty(window, 'daemonAPI', {
    configurable: true,
    value: { setProvisioningWorkspace: state.setProvisioningWorkspace },
  });
});

describe('WorkspaceRouteLayout', () => {
  it('points package provisioning (issue #188) at the resolved workspace', () => {
    renderLayout();
    expect(mocks.setProvisioningWorkspace).toHaveBeenCalledWith('ws-1');
  });

  it('mounts SourceBackfillModal when no WindowOverlay is active', () => {
    const { queryByTestId } = renderLayout();
    expect(queryByTestId(state.modalAriaLabel)).not.toBeNull();
    expect(state.modalRenders).toBeGreaterThan(0);
  });

  it('suppresses SourceBackfillModal while a WindowOverlay is active', () => {
    state.overlay = { type: 'new-workspace' };
    const { queryByTestId } = renderLayout();
    expect(queryByTestId(state.modalAriaLabel)).toBeNull();
    expect(state.modalRenders).toBe(0);
  });

  it('mounts the missing-credentials banner when no WindowOverlay is active', () => {
    const { queryByTestId } = renderLayout();
    expect(queryByTestId(state.bannerTestId)).not.toBeNull();
    expect(state.bannerRenders).toBeGreaterThan(0);
  });

  it('suppresses the missing-credentials banner while a WindowOverlay is active', () => {
    state.overlay = { type: 'new-workspace' };
    const { queryByTestId } = renderLayout();
    expect(queryByTestId(state.bannerTestId)).toBeNull();
    expect(state.bannerRenders).toBe(0);
  });

  it('mounts the provisioning reminder when no WindowOverlay is active', () => {
    const { queryByTestId } = renderLayout();
    expect(queryByTestId(state.reminderTestId)).not.toBeNull();
    expect(state.reminderRenders).toBeGreaterThan(0);
  });

  it('suppresses the provisioning reminder while a WindowOverlay is active', () => {
    state.overlay = { type: 'onboarding' };
    const { queryByTestId } = renderLayout();
    expect(queryByTestId(state.reminderTestId)).toBeNull();
    expect(state.reminderRenders).toBe(0);
  });

  it('does not call the provisioning IPC before the workspace has resolved', () => {
    state.workspace = null;
    state.listFetched = false;
    renderLayout();
    const idCalls = mocks.setProvisioningWorkspace.mock.calls.filter((call) => call[0] !== null);
    expect(idCalls).toEqual([]);
  });
});
