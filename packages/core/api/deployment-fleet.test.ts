import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiClient } from './client';
import { EMPTY_DEPLOYMENT_FLEET } from './deployment-fleet';

const BASE = 'https://api.example.test';

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubJson(body: unknown): ReturnType<typeof vi.fn> {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }),
  );
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

describe('ApiClient.getDeploymentFleet', () => {
  it('GETs the fleet and parses machines, runtimes and agents', async () => {
    const fetchMock = stubJson({
      machines: [
        {
          daemon_id: 'd-1',
          workspace_id: 'ws-1',
          workspace_name: 'Acme',
          workspace_slug: 'acme',
          owner_email: 'ops@example.test',
          owner_name: 'Ops',
          device_info: 'MacBook',
          online: true,
          last_heartbeat_at: '2026-09-08T10:00:00Z',
          client_version: '1.0.0',
          version_outdated: false,
          running_tasks: 2,
          stuck_tasks: 1,
          runtimes: [
            {
              id: 'rt-1',
              name: 'claude',
              provider: 'claude',
              visibility: 'private',
              status: 'online',
              online: true,
              last_seen_at: '2026-09-08T10:00:00Z',
              running_tasks: 2,
              stuck_tasks: 1,
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
        },
      ],
      summary: {
        machines_total: 1,
        machines_online: 1,
        machines_offline: 0,
        outdated_versions: 0,
        running_tasks: 2,
        stuck_tasks: 1,
      },
      total: 1,
      truncated: false,
      min_client_version: '0.2.21',
    });

    const fleet = await new ApiClient(BASE).getDeploymentFleet();

    expect(String(fetchMock.mock.calls[0]?.[0])).toBe(`${BASE}/api/deployment/fleet`);
    expect(fleet.total).toBe(1);
    expect(fleet.min_client_version).toBe('0.2.21');
    expect(fleet.machines[0]?.runtimes[0]?.agents[0]?.system_key).toBe('goosar_helper');
    expect(fleet.summary.stuck_tasks).toBe(1);
  });

  it('defaults a drifted machine to the non-alarming reading', async () => {
    stubJson({
      machines: [{ daemon_id: 'd-1', workspace_name: 'Acme' }],
      total: 1,
    });

    const fleet = await new ApiClient(BASE).getDeploymentFleet();
    const machine = fleet.machines[0];

    expect(machine?.daemon_id).toBe('d-1');
    expect(machine?.version_outdated).toBe(false);
    expect(machine?.online).toBe(false);
    expect(machine?.last_heartbeat_at).toBeNull();
    expect(machine?.runtimes).toEqual([]);
    expect(machine?.stuck_tasks).toBe(0);
    expect(fleet.summary).toEqual(EMPTY_DEPLOYMENT_FLEET.summary);
  });

  it('falls back to the empty fleet when the response is the wrong shape', async () => {
    stubJson([{ daemon_id: 'd-1' }]);
    await expect(new ApiClient(BASE).getDeploymentFleet()).resolves.toEqual(EMPTY_DEPLOYMENT_FLEET);
  });
});
