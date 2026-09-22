import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/react';
import type { DeploymentFleet } from '@goosar/core/api/deployment-fleet';
import { renderWithI18n } from '../../test/i18n';

const state = vi.hoisted(() => ({
  fleet: undefined as unknown,
  isError: false,
  dataUpdatedAt: 0,
}));

vi.mock('@tanstack/react-query', () => ({
  useQuery: () =>
    state.isError
      ? {
          data: undefined,
          error: new Error('boom'),
          isError: true,
          isPending: false,
          isSuccess: false,
          dataUpdatedAt: 0,
        }
      : {
          data: state.fleet,
          error: null,
          isError: false,
          isPending: state.fleet === undefined,
          isSuccess: state.fleet !== undefined,
          dataUpdatedAt: state.dataUpdatedAt,
        },
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock('@goosar/core/deployment/admin', () => ({
  DEPLOYMENT_FLEET_REFRESH_MS: 30_000,
  deploymentFleetOptions: () => ({
    queryKey: ['deployment', 'fleet'],
    queryFn: vi.fn(),
  }),
}));

import { DeploymentFleetSection } from './deployment-fleet-section';

function machine(
  over: Partial<DeploymentFleet['machines'][number]> = {},
): DeploymentFleet['machines'][number] {
  return {
    daemon_id: 'd-1',
    workspace_id: 'ws-1',
    workspace_name: 'Acme',
    workspace_slug: 'acme',
    owner_email: 'ops@example.test',
    owner_name: 'Ops',
    device_info: 'MacBook Pro',
    online: true,
    last_heartbeat_at: new Date().toISOString(),
    client_version: '1.0.0',
    version_outdated: false,
    running_tasks: 0,
    stuck_tasks: 0,
    runtimes: [],
    ...over,
  };
}

function fleet(over: Partial<DeploymentFleet> = {}): DeploymentFleet {
  return {
    machines: [],
    summary: {
      machines_total: 0,
      machines_online: 0,
      machines_offline: 0,
      outdated_versions: 0,
      running_tasks: 0,
      stuck_tasks: 0,
    },
    total: 0,
    truncated: false,
    min_client_version: '0.2.21',
    ...over,
  };
}

beforeEach(() => {
  state.fleet = undefined;
  state.isError = false;
  state.dataUpdatedAt = Date.now();
});

describe('DeploymentFleetSection', () => {
  it('renders the summary tiles and one row per machine', () => {
    state.fleet = fleet({
      machines: [
        machine({
          runtimes: [
            {
              id: 'rt-1',
              name: 'claude',
              provider: 'claude',
              visibility: 'private',
              status: 'online',
              online: true,
              last_seen_at: new Date().toISOString(),
              running_tasks: 1,
              stuck_tasks: 0,
              agents: [
                {
                  id: 'a-1',
                  name: 'Helper',
                  system_key: 'goosar_helper',
                  status: 'working',
                },
              ],
            },
          ],
          running_tasks: 1,
        }),
        machine({
          daemon_id: 'd-2',
          workspace_id: 'ws-2',
          workspace_name: 'Beta',
          device_info: 'build-box',
          online: false,
          client_version: '0.0.1',
          version_outdated: true,
        }),
      ],
      summary: {
        machines_total: 2,
        machines_online: 1,
        machines_offline: 1,
        outdated_versions: 1,
        running_tasks: 1,
        stuck_tasks: 0,
      },
      total: 2,
    });

    renderWithI18n(<DeploymentFleetSection />);

    expect(screen.getByText('MacBook Pro')).toBeInTheDocument();
    expect(screen.getByText('build-box')).toBeInTheDocument();
    expect(screen.getByText('Acme')).toBeInTheDocument();
    expect(screen.getByText('0.0.1 — outdated')).toBeInTheDocument();
    expect(screen.getByText(/Helper \(goosar_helper\)/)).toBeInTheDocument();
    expect(screen.getByText('Machines offline')).toBeInTheDocument();
  });

  it('translates the runtime enums and echoes an unknown one raw', () => {
    const rt = (over: Record<string, unknown>) => ({
      id: 'rt',
      name: 'claude',
      provider: 'claude',
      visibility: 'private',
      status: 'online',
      online: true,
      last_seen_at: null,
      running_tasks: 0,
      stuck_tasks: 0,
      agents: [],
      ...over,
    });
    state.fleet = fleet({
      machines: [
        machine({
          runtimes: [
            rt({ id: 'rt-1', name: 'known' }),
            rt({ id: 'rt-2', name: 'drifted', status: 'draining' }),
          ],
        }),
      ],
      total: 1,
    });

    renderWithI18n(<DeploymentFleetSection />);

    expect(screen.getByText(/known · Private · online/)).toBeInTheDocument();
    expect(screen.getByText(/drifted · Private · draining/)).toBeInTheDocument();
  });

  it('filters to offline and to outdated machines', () => {
    state.fleet = fleet({
      machines: [
        machine({ device_info: 'alive-box' }),
        machine({
          daemon_id: 'd-2',
          device_info: 'dead-box',
          online: false,
          version_outdated: true,
          client_version: '0.0.1',
        }),
      ],
      total: 2,
    });

    renderWithI18n(<DeploymentFleetSection />);
    expect(screen.getByText('alive-box')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Offline' }));
    expect(screen.queryByText('alive-box')).not.toBeInTheDocument();
    expect(screen.getByText('dead-box')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Outdated' }));
    expect(screen.getByText('dead-box')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Online' }));
    expect(screen.getByText('alive-box')).toBeInTheDocument();
    expect(screen.queryByText('dead-box')).not.toBeInTheDocument();
  });

  it('distinguishes an empty fleet from an empty filter result', () => {
    state.fleet = fleet();
    const { unmount } = renderWithI18n(<DeploymentFleetSection />);
    expect(
      screen.getByText('No machines have registered in this deployment yet.'),
    ).toBeInTheDocument();
    unmount();

    state.fleet = fleet({ machines: [machine()], total: 1 });
    renderWithI18n(<DeploymentFleetSection />);
    fireEvent.click(screen.getByRole('button', { name: 'Offline' }));
    expect(screen.getByText('No machine matches the filter.')).toBeInTheDocument();
  });

  it('shows how stale the view is instead of implying it is live', () => {
    state.fleet = fleet();
    state.dataUpdatedAt = Date.now() - 12_000;
    renderWithI18n(<DeploymentFleetSection />);
    expect(screen.getByText('updated 12s ago')).toBeInTheDocument();
  });

  it('states a failed read rather than an empty fleet', () => {
    state.isError = true;
    renderWithI18n(<DeploymentFleetSection />);
    expect(screen.getByRole('alert')).toHaveTextContent("Couldn't load the fleet.");
    expect(
      screen.queryByText('No machines have registered in this deployment yet.'),
    ).not.toBeInTheDocument();
  });

  it('says the list was capped instead of passing it off as the whole fleet', () => {
    state.fleet = fleet({ machines: [machine()], total: 240, truncated: true });
    renderWithI18n(<DeploymentFleetSection />);
    expect(screen.getByText('Showing 1 of 240 machines.')).toBeInTheDocument();
  });
});
