// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../locales/en/common.json';
import enOnboarding from '../locales/en/onboarding.json';
import enWorkspace from '../locales/en/workspace.json';

const mocks = vi.hoisted(() => ({
  updateAutopilot: vi.fn(async () => ({})),
  invalidate: vi.fn(async () => undefined),
  data: {
    autopilots: [] as { id: string; status: string; is_template?: boolean }[],
    runtimes: [] as { visibility: string }[],
  },
}));

vi.mock('@tanstack/react-query', () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    const key = JSON.stringify(opts.queryKey);
    const data = key.includes('autopilots')
      ? mocks.data.autopilots
      : key.includes('runtimes')
        ? mocks.data.runtimes
        : undefined;
    return { data, error: null, isSuccess: true, isPending: false };
  },
  useQueryClient: () => ({ invalidateQueries: mocks.invalidate }),
  queryOptions: <T,>(o: T) => o,
}));

vi.mock('@goosar/core/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/api')>()),
  api: { updateAutopilot: mocks.updateAutopilot },
}));

vi.mock('@goosar/core/hooks', () => ({ useWorkspaceId: () => 'ws-1' }));

vi.mock('../onboarding/launchers', () => ({
  RuntimeConnectButton: () => <div data-testid="runtime-connect-button" />,
}));

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { RoleSetupSection } from './role-setup-section';

function renderSection() {
  return render(
    <I18nProvider
      locale="en"
      resources={{
        en: { common: enCommon, workspace: enWorkspace, onboarding: enOnboarding },
      }}
    >
      <RoleSetupSection />
    </I18nProvider>,
  );
}

describe('RoleSetupSection (R-16e)', () => {
  beforeEach(() => {
    cleanup();
    vi.clearAllMocks();
    mocks.data.autopilots = Array.from({ length: 8 }, (_, i) => ({
      id: `ap-${i}`,
      status: 'paused',
      is_template: true,
    }));
    mocks.data.runtimes = [];
  });

  it('shows both cards for a freshly joined role', () => {
    renderSection();

    expect(screen.getByTestId('role-card-autopilots')).toHaveTextContent('Autopilots (8)');
    expect(screen.getByTestId('role-card-runtime')).toBeInTheDocument();
  });

  it('leaves only the autopilots card once a machine is in place', () => {
    mocks.data.runtimes = [{ visibility: 'public' }];

    renderSection();

    expect(screen.getByTestId('role-card-autopilots')).toBeInTheDocument();
    expect(screen.queryByTestId('role-card-runtime')).toBeNull();
  });

  it('renders nothing at all once the role is fully set up', () => {
    mocks.data.autopilots = [{ id: 'ap-0', status: 'active', is_template: true }];
    mocks.data.runtimes = [{ visibility: 'public' }];

    const { container } = renderSection();

    expect(container).toBeEmptyDOMElement();
  });

  it('enables every paused autopilot and refreshes the list', async () => {
    renderSection();

    await userEvent.click(
      screen.getByRole('button', { name: enWorkspace.role_setup.autopilots_action }),
    );

    await waitFor(() => expect(mocks.updateAutopilot).toHaveBeenCalledTimes(8));
    expect(mocks.updateAutopilot).toHaveBeenCalledWith('ap-0', {
      status: 'active',
    });
    expect(mocks.invalidate).toHaveBeenCalled();
  });

  it("counts and enables only the template's paused autopilots", async () => {
    mocks.data.autopilots = [
      { id: 'tpl-1', status: 'paused', is_template: true },
      { id: 'tpl-2', status: 'paused', is_template: true },
      { id: 'mine', status: 'paused', is_template: false },
      { id: 'old-server', status: 'paused' },
    ];

    renderSection();

    expect(screen.getByTestId('role-card-autopilots')).toHaveTextContent('Autopilots (2)');

    await userEvent.click(
      screen.getByRole('button', { name: enWorkspace.role_setup.autopilots_action }),
    );

    await waitFor(() => expect(mocks.updateAutopilot).toHaveBeenCalledTimes(2));
    expect(mocks.updateAutopilot).not.toHaveBeenCalledWith('mine', expect.anything());
    expect(mocks.updateAutopilot).not.toHaveBeenCalledWith('old-server', expect.anything());
  });
});
