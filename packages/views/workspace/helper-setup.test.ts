import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Agent } from '@goosar/core/types';

const mocks = vi.hoisted(() => ({
  listAgents: vi.fn(),
  createAgent: vi.fn(),
  updateAgent: vi.fn(),
  listMembers: vi.fn(),
}));

vi.mock('@goosar/core/api', () => ({
  api: {
    listAgents: mocks.listAgents,
    createAgent: mocks.createAgent,
    updateAgent: mocks.updateAgent,
    listMembers: mocks.listMembers,
  },
}));

const authUserRef = vi.hoisted(() => ({
  current: { id: 'user-1' } as { id: string } | null,
}));

vi.mock('@goosar/core/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/auth')>()),
  useAuthStore: Object.assign(
    (selector: (s: { user: unknown }) => unknown) => selector({ user: authUserRef.current }),
    { getState: () => ({ user: authUserRef.current }) },
  ),
}));

import { configStore } from '@goosar/core/config';
import {
  HELPER_AGENT_NAME,
  HELPER_SYSTEM_KEY,
  isWorkspaceHelper,
  prepareWorkspaceHelper,
} from './helper-setup';

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: 'agent-1',
    workspace_id: 'ws-1',
    name: HELPER_AGENT_NAME,
    system_key: HELPER_SYSTEM_KEY,
    description: '',
    avatar_url: null,
    runtime_mode: 'local',
    runtime_config: {},
    runtime_id: 'rt-1',
    visibility: 'workspace',
    status: 'idle',
    max_concurrent_tasks: 6,
    owner_id: 'user-1',
    instructions: '',
    mcp_config: null,
    archived_at: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  } as Agent;
}

function setDeliveryProfile(value: string | undefined) {
  configStore.setState({ deliveryProfile: value } as unknown as Parameters<
    typeof configStore.setState
  >[0]);
}

beforeEach(() => {
  vi.clearAllMocks();
  setDeliveryProfile(undefined);
  authUserRef.current = { id: 'user-1' };
  mocks.updateAgent.mockImplementation(async (_id: string, patch: object) => ({
    ...makeAgent(),
    ...patch,
  }));
});

describe('isWorkspaceHelper', () => {
  it('recognises the Helper by system_key even after the user renamed it', () => {
    expect(isWorkspaceHelper(makeAgent({ name: 'Дежурный' }))).toBe(true);
  });

  it("rejects a hand-made agent that merely carries the Helper's name", () => {
    expect(
      isWorkspaceHelper(makeAgent({ name: HELPER_AGENT_NAME, system_key: 'something_else' })),
    ).toBe(false);
  });

  it('falls back to the name for a pre-0.8.0 Helper with no system_key', () => {
    expect(isWorkspaceHelper(makeAgent({ system_key: undefined }))).toBe(true);
    expect(isWorkspaceHelper(makeAgent({ system_key: undefined, name: 'Other' }))).toBe(false);
  });

  it('never counts an archived Helper', () => {
    expect(isWorkspaceHelper(makeAgent({ archived_at: '2026-01-02T00:00:00Z' }))).toBe(false);
  });
});

describe('the Helper lookup', () => {
  it('returns the workspace Helper the server provisioned', async () => {
    const helper = makeAgent({ mcp_config: {} });
    mocks.listAgents.mockResolvedValue([helper]);

    await expect(prepareWorkspaceHelper('ws-1', null)).resolves.toBe(helper);
    expect(mocks.listAgents).toHaveBeenCalledWith({ workspace_id: 'ws-1' });
  });

  it('never creates an agent — creation belongs to the server', async () => {
    mocks.listAgents.mockResolvedValue([]);

    await expect(prepareWorkspaceHelper('ws-1', null)).resolves.toBeNull();
    expect(mocks.createAgent).not.toHaveBeenCalled();
  });

  it('returns null instead of throwing when the listing fails', async () => {
    mocks.listAgents.mockRejectedValue(new Error('network'));

    await expect(prepareWorkspaceHelper('ws-1', null)).resolves.toBeNull();
  });

  it("skips another member's Helper and resolves to the current user's own", async () => {
    const foreign = makeAgent({
      id: 'agent-2',
      name: `${HELPER_AGENT_NAME} (Bob)`,
      owner_id: 'user-2',
      mcp_config: {},
    });
    const own = makeAgent({ mcp_config: {} });
    mocks.listAgents.mockResolvedValue([foreign, own]);

    await expect(prepareWorkspaceHelper('ws-1', null)).resolves.toBe(own);
    expect(mocks.updateAgent).not.toHaveBeenCalled();
  });

  it("returns null when only other members' Helpers are visible", async () => {
    mocks.listAgents.mockResolvedValue([
      makeAgent({ id: 'agent-2', owner_id: 'user-2', mcp_config: {} }),
    ]);

    await expect(prepareWorkspaceHelper('ws-1', null)).resolves.toBeNull();
    expect(mocks.updateAgent).not.toHaveBeenCalled();
  });

  it('still matches a Helper that carries no owner (pre-#208 row / older backend)', async () => {
    const legacy = makeAgent({ owner_id: null, mcp_config: {} });
    mocks.listAgents.mockResolvedValue([legacy]);

    await expect(prepareWorkspaceHelper('ws-1', null)).resolves.toBe(legacy);
  });

  it('fails closed on an owned Helper when the current user is unknown', async () => {
    authUserRef.current = null;
    mocks.listAgents.mockResolvedValue([makeAgent({ mcp_config: {} })]);

    await expect(prepareWorkspaceHelper('ws-1', null)).resolves.toBeNull();
    expect(mocks.updateAgent).not.toHaveBeenCalled();
  });
});

describe('prepareWorkspaceHelper', () => {
  it('returns null and writes nothing when the workspace has no Helper yet', async () => {
    mocks.listAgents.mockResolvedValue([]);

    await expect(prepareWorkspaceHelper('ws-1', 'rt-2')).resolves.toBeNull();
    expect(mocks.createAgent).not.toHaveBeenCalled();
    expect(mocks.updateAgent).not.toHaveBeenCalled();
  });

  it('rebinds the Helper onto the runtime the user picked', async () => {
    mocks.listAgents.mockResolvedValue([makeAgent({ runtime_id: 'rt-server' })]);

    const helper = await prepareWorkspaceHelper('ws-1', 'rt-picked');

    const rebind = mocks.updateAgent.mock.calls.find(
      ([, patch]) => (patch as { runtime_id?: string }).runtime_id !== undefined,
    );
    expect(rebind).toBeDefined();
    expect(rebind![0]).toBe('agent-1');
    expect((rebind![1] as { runtime_id?: string }).runtime_id).toBe('rt-picked');
    expect(helper?.runtime_id).toBe('rt-picked');
  });

  it('still writes the corporate preset when the rebind is refused', async () => {
    mocks.listAgents.mockResolvedValue([
      makeAgent({ runtime_id: 'rt-server', owner_id: null, mcp_config: null }),
    ]);
    mocks.updateAgent.mockImplementation(async (_id: string, patch: Record<string, unknown>) => {
      if (patch.runtime_id !== undefined) throw new Error('403 forbidden');
      return makeAgent({ ...patch });
    });

    const helper = await prepareWorkspaceHelper('ws-1', 'rt-picked');

    const presetCall = mocks.updateAgent.mock.calls.find(
      ([, patch]) => (patch as { mcp_config?: unknown }).mcp_config !== undefined,
    );
    expect(presetCall).toBeDefined();
    expect(helper).not.toBeNull();
  });

  it('does not rebind when the Helper is already on the picked runtime', async () => {
    mocks.listAgents.mockResolvedValue([makeAgent({ runtime_id: 'rt-1', mcp_config: {} })]);

    await prepareWorkspaceHelper('ws-1', 'rt-1');

    expect(mocks.updateAgent).not.toHaveBeenCalled();
  });

  it('does not rebind when no runtime was picked', async () => {
    mocks.listAgents.mockResolvedValue([makeAgent({ runtime_id: 'rt-server', mcp_config: {} })]);

    await prepareWorkspaceHelper('ws-1', null);

    expect(mocks.updateAgent).not.toHaveBeenCalled();
  });

  it('seeds the corporate MCP presets onto a Helper with no stored config', async () => {
    mocks.listAgents.mockResolvedValue([makeAgent({ mcp_config: null })]);

    await prepareWorkspaceHelper('ws-1', null);

    expect(mocks.updateAgent).toHaveBeenCalledTimes(1);
    const patch = mocks.updateAgent.mock.calls[0]![1] as {
      mcp_config: { mcpServers: Record<string, { enabled: boolean }> };
    };
    expect(Object.keys(patch.mcp_config.mcpServers).length).toBeGreaterThan(0);
    for (const entry of Object.values(patch.mcp_config.mcpServers)) {
      expect(entry.enabled).toBe(false);
    }
  });

  it('never overwrites an existing config — user edits are not ours to replace', async () => {
    mocks.listAgents.mockResolvedValue([
      makeAgent({ mcp_config: { mcpServers: { mine: { command: 'x' } } } }),
    ]);

    await prepareWorkspaceHelper('ws-1', null);

    expect(mocks.updateAgent).not.toHaveBeenCalled();
  });

  it('leaves a redacted config alone: a config exists, this viewer just cannot read it', async () => {
    mocks.listAgents.mockResolvedValue([
      makeAgent({ mcp_config: null, mcp_config_redacted: true }),
    ]);

    await prepareWorkspaceHelper('ws-1', null);

    expect(mocks.updateAgent).not.toHaveBeenCalled();
  });

  it('withholds the catalog from an ungranted member on a perimeter deployment', async () => {
    setDeliveryProfile('perimeter');
    mocks.listAgents.mockResolvedValue([makeAgent({ mcp_config: null })]);
    mocks.listMembers.mockResolvedValue([{ user_id: 'user-1', perimeter_access: false }]);

    await prepareWorkspaceHelper('ws-1', null);

    expect(mocks.updateAgent).not.toHaveBeenCalled();
  });

  it('seeds the catalog for a granted member on a perimeter deployment', async () => {
    setDeliveryProfile('perimeter');
    mocks.listAgents.mockResolvedValue([makeAgent({ mcp_config: null })]);
    mocks.listMembers.mockResolvedValue([{ user_id: 'user-1', perimeter_access: true }]);

    await prepareWorkspaceHelper('ws-1', null);

    expect(mocks.updateAgent).toHaveBeenCalledTimes(1);
  });

  it('fails closed when the perimeter grant cannot be read', async () => {
    setDeliveryProfile('perimeter');
    mocks.listAgents.mockResolvedValue([makeAgent({ mcp_config: null })]);
    mocks.listMembers.mockRejectedValue(new Error('network'));

    await prepareWorkspaceHelper('ws-1', null);

    expect(mocks.updateAgent).not.toHaveBeenCalled();
  });

  it('still returns the Helper when the refinements fail — the flow must not stall', async () => {
    mocks.listAgents.mockResolvedValue([makeAgent({ runtime_id: 'rt-server' })]);
    mocks.updateAgent.mockRejectedValue(new Error('500'));

    const helper = await prepareWorkspaceHelper('ws-1', 'rt-picked');

    expect(helper?.id).toBe('agent-1');
    expect(helper?.runtime_id).toBe('rt-server');
  });

  it('shares one in-flight run across concurrent callers (StrictMode double-mount)', async () => {
    mocks.listAgents.mockResolvedValue([makeAgent({ runtime_id: 'rt-server' })]);

    const [a, b] = await Promise.all([
      prepareWorkspaceHelper('ws-1', 'rt-picked'),
      prepareWorkspaceHelper('ws-1', 'rt-picked'),
    ]);

    expect(a).toBe(b);
    expect(mocks.listAgents).toHaveBeenCalledTimes(1);
    expect(mocks.updateAgent.mock.calls.length).toBeLessThanOrEqual(2);
    expect(
      mocks.updateAgent.mock.calls.filter(
        ([, patch]) => (patch as { runtime_id?: string }).runtime_id !== undefined,
      ),
    ).toHaveLength(1);
  });
});
