import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

const state = vi.hoisted(() => ({
  overlay: { type: 'onboarding' as const },
  status: null as unknown,
}));

const mocks = vi.hoisted(() => ({
  setProvisioningWorkspace: vi.fn(),
  retry: vi.fn(),
  onboardingFlowProps: null as Record<string, unknown> | null,
}));

vi.mock('@/stores/window-overlay-store', () => ({
  useWindowOverlayStore: (selector: (s: typeof state) => unknown) =>
    selector({ ...state, overlay: state.overlay } as never),
}));

vi.mock('@goosar/views/onboarding', () => ({
  OnboardingFlow: (props: Record<string, unknown>) => {
    mocks.onboardingFlowProps = props;
    return (
      <button onClick={() => (props.onWorkspaceProvisioning as (id: string) => void)('ws-9')}>
        stub-onboarding-workspace-ready
      </button>
    );
  },
}));

vi.mock('@goosar/views/workspace/new-workspace-page', () => ({
  NewWorkspacePage: () => null,
}));
vi.mock('@goosar/views/invite', () => ({ InvitePage: () => null }));
vi.mock('@goosar/views/invitations', () => ({ InvitationsPage: () => null }));
vi.mock('@goosar/views/navigation', () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));
vi.mock('@goosar/core/workspace/queries', () => ({
  workspaceListOptions: () => ({
    queryKey: ['ws-list'],
    queryFn: async () => [],
  }),
}));
vi.mock('../platform/use-local-runtimes-pending', () => ({
  useLocalRuntimesPending: () => false,
}));
vi.mock('../platform/use-perimeter-machine', () => ({
  usePerimeterMachine: () => undefined,
}));
vi.mock('../platform/save-llm-connection', () => ({
  saveLlmConnection: vi.fn(),
}));
vi.mock('./use-desktop-runtime-context', () => ({
  useDesktopRuntimeContext: () => ({
    localDaemonId: null,
    localMachineName: null,
  }),
}));
vi.mock('../platform/use-provisioning-status', () => ({
  useProvisioningStatus: (workspaceId: string | null) => {
    mocks.setProvisioningWorkspace(workspaceId);
    return { status: state.status, retry: mocks.retry };
  },
}));

import { WindowOverlay } from './window-overlay';

function renderOverlay() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <WindowOverlay />
    </QueryClientProvider>,
  );
}

describe('WindowOverlay — provisioning wiring (issue #188)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    state.overlay = { type: 'onboarding' };
    state.status = null;
    mocks.onboardingFlowProps = null;
  });

  it('passes the polled status and retry callback through to OnboardingFlow', () => {
    state.status = { state: 'syncing' };
    renderOverlay();
    expect(mocks.onboardingFlowProps?.provisioningStatus).toEqual({
      state: 'syncing',
    });
    expect(mocks.onboardingFlowProps?.onRetryProvisioning).toBe(mocks.retry);
  });

  it('starts with a null provisioning workspace and updates it when OnboardingFlow reports one', async () => {
    const user = userEvent.setup();
    renderOverlay();
    expect(mocks.setProvisioningWorkspace).toHaveBeenLastCalledWith(null);

    await user.click(screen.getByText('stub-onboarding-workspace-ready'));

    expect(mocks.setProvisioningWorkspace).toHaveBeenLastCalledWith('ws-9');
  });
});
