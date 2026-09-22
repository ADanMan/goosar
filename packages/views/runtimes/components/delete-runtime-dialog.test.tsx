// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import type { Agent, AgentRuntime } from '@goosar/core/types';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enRuntimes from '../../locales/en/runtimes.json';
import enAgents from '../../locales/en/agents.json';

const TEST_RESOURCES = {
  en: { common: enCommon, runtimes: enRuntimes, agents: enAgents },
};

const { ApiError, apiDeleteRuntime, apiArchiveAgentsAndDeleteRuntime } = vi.hoisted(() => {
  class ApiError extends Error {
    status: number;
    body: unknown;
    constructor(message: string, status: number, body: unknown) {
      super(message);
      this.status = status;
      this.body = body;
    }
  }
  return {
    ApiError,
    apiDeleteRuntime: vi.fn(),
    apiArchiveAgentsAndDeleteRuntime: vi.fn(),
  };
});

vi.mock('@goosar/core/api', () => ({
  api: {
    deleteRuntime: (...args: unknown[]) => apiDeleteRuntime(...args),
    archiveAgentsAndDeleteRuntime: (...args: unknown[]) =>
      apiArchiveAgentsAndDeleteRuntime(...args),
    listAgents: vi.fn(),
    listMembers: vi.fn(),
  },
  ApiError,
}));

vi.mock('@goosar/core/runtimes/mutations', () => ({
  useDeleteRuntime: () => ({
    isPending: false,
    mutate: vi.fn(),
    mutateAsync: (...args: unknown[]) => apiDeleteRuntime(...args),
  }),
  useArchiveAgentsAndDeleteRuntime: () => ({
    isPending: false,
    mutate: vi.fn(),
    mutateAsync: (vars: { runtimeId: string; expectedActiveAgentIds: string[] }) =>
      apiArchiveAgentsAndDeleteRuntime(vars.runtimeId, vars.expectedActiveAgentIds),
  }),
}));

vi.mock('@tanstack/react-query', async () => {
  const actual =
    await vi.importActual<typeof import('@tanstack/react-query')>('@tanstack/react-query');
  return {
    ...actual,
    useQuery: vi.fn(() => ({ data: [], isLoading: false })),
  };
});

vi.mock('@goosar/core/agents', () => ({
  useWorkspacePresenceMap: () => ({ byAgent: new Map(), loading: false }),
}));

vi.mock('@goosar/core/auth', () => ({
  useAuthStore: (sel: (s: { user: { id: string } }) => unknown) => sel({ user: { id: 'user-me' } }),
}));

vi.mock('../../common/actor-avatar', () => ({ ActorAvatar: () => null }));
vi.mock('../../agents/presence', () => ({
  availabilityConfig: {
    online: { dotClass: '', textClass: '' },
    unstable: { dotClass: '', textClass: '' },
    offline: { dotClass: '', textClass: '' },
  },
  workloadConfig: {
    working: { icon: () => null, textClass: '' },
    queued: { icon: () => null, textClass: '' },
    idle: { icon: () => null, textClass: '' },
  },
}));

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { useQuery } from '@tanstack/react-query';
import { DeleteRuntimeDialog } from './delete-runtime-dialog';

const mockedUseQuery = vi.mocked(useQuery);

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: 'rt-1',
    workspace_id: 'ws-1',
    daemon_id: null,
    name: 'Cloud Runtime',
    runtime_mode: 'cloud',
    provider: 'claude',
    launch_header: '',
    status: 'online',
    device_info: '',
    metadata: {},
    owner_id: 'user-me',
    visibility: 'private',
    last_seen_at: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function makeAgent(id: string, overrides: Partial<Agent> = {}): Agent {
  return {
    id,
    workspace_id: 'ws-1',
    runtime_id: 'rt-1',
    name: `Agent ${id}`,
    description: '',
    instructions: '',
    avatar_url: null,
    runtime_mode: 'cloud',
    runtime_config: {},
    custom_args: [],
    visibility: 'private',
    permission_mode: 'private',
    invocation_targets: [],
    status: 'idle',
    max_concurrent_tasks: 1,
    model: 'claude-sonnet-4-5',
    owner_id: 'user-me',
    skills: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function renderDialog(
  opts: {
    runtime?: AgentRuntime;
    cachedAgents?: Agent[];
    onDeleted?: () => void;
  } = {},
) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const onOpenChange = vi.fn();
  const onDeleted = opts.onDeleted ?? vi.fn();

  mockedUseQuery.mockImplementation(((queryArg: unknown) => {
    const q = queryArg as { queryKey?: readonly unknown[] };
    const key = q?.queryKey ?? [];
    const tail = key[key.length - 1];
    if (tail === 'agents') {
      return { data: opts.cachedAgents ?? [], isLoading: false } as unknown as ReturnType<
        typeof useQuery
      >;
    }
    return { data: [], isLoading: false } as unknown as ReturnType<typeof useQuery>;
  }) as unknown as typeof useQuery);

  const utils = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <DeleteRuntimeDialog
          open
          onOpenChange={onOpenChange}
          runtime={opts.runtime ?? makeRuntime()}
          wsId="ws-1"
          onDeleted={onDeleted}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { ...utils, onOpenChange, onDeleted };
}

describe('DeleteRuntimeDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders the light-mode prompt when no agents are bound', () => {
    renderDialog({ cachedAgents: [] });

    expect(screen.getByText('Delete Runtime?')).toBeInTheDocument();
    expect(screen.getByText('Delete runtime')).toBeInTheDocument();
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
    expect(screen.queryByText(/Archive .* and delete this Runtime/)).not.toBeInTheDocument();
  });

  it('opens directly in cascade mode when local cache shows bound agents, with the destructive button gated by the checkbox', async () => {
    renderDialog({
      cachedAgents: [makeAgent('a-1', { name: 'Alpha' }), makeAgent('a-2', { name: 'Beta' })],
    });

    expect(screen.getByText(/Archive 2 agents and delete this Runtime/)).toBeInTheDocument();
    const confirm = screen.getByRole('button', {
      name: /Archive 2 agents and delete runtime/,
    }) as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);

    const checkbox = screen.getByRole('checkbox') as HTMLInputElement;
    fireEvent.click(checkbox);
    await waitFor(() => expect(confirm.disabled).toBe(false));

    apiArchiveAgentsAndDeleteRuntime.mockResolvedValueOnce({
      status: 'ok',
      agents_archived: 2,
      tasks_cancelled: 0,
    });
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(apiArchiveAgentsAndDeleteRuntime).toHaveBeenCalledWith('rt-1', ['a-1', 'a-2']),
    );
  });

  it('pivots from light to cascade mode when the strict DELETE returns runtime_has_active_agents', async () => {
    const fresh = makeAgent('a-9', { name: 'FreshAgent' });
    apiDeleteRuntime.mockRejectedValueOnce(
      new ApiError('conflict', 409, {
        code: 'runtime_has_active_agents',
        active_agents: [fresh],
      }),
    );

    renderDialog({ cachedAgents: [] });

    const lightConfirm = screen.getByRole('button', { name: 'Delete runtime' });
    fireEvent.click(lightConfirm);

    await waitFor(() =>
      expect(screen.getByText(/Archive 1 agent and delete this Runtime/)).toBeInTheDocument(),
    );
    expect(screen.getByText('FreshAgent')).toBeInTheDocument();
    expect(
      screen.getByText(/Active agents were added since you opened this dialog/),
    ).toBeInTheDocument();
  });

  it('re-prompts when the cascade returns runtime_delete_plan_changed', async () => {
    apiArchiveAgentsAndDeleteRuntime.mockRejectedValueOnce(
      new ApiError('plan changed', 409, {
        code: 'runtime_delete_plan_changed',
        active_agents: [
          makeAgent('a-1', { name: 'Alpha' }),
          makeAgent('a-2', { name: 'Beta' }),
          makeAgent('a-3', { name: 'Gamma' }),
        ],
      }),
    );

    renderDialog({
      cachedAgents: [makeAgent('a-1', { name: 'Alpha' }), makeAgent('a-2', { name: 'Beta' })],
    });

    fireEvent.click(screen.getByRole('checkbox'));
    fireEvent.click(
      screen.getByRole('button', {
        name: /Archive 2 agents and delete runtime/,
      }),
    );

    await waitFor(() =>
      expect(screen.getByText(/Archive 3 agents and delete this Runtime/)).toBeInTheDocument(),
    );
    expect(screen.getByText('Gamma')).toBeInTheDocument();
    expect(screen.getByRole('checkbox').getAttribute('aria-checked')).toBe('false');
    expect(screen.getByText(/active agent set changed/i)).toBeInTheDocument();
  });

  it('renders the self-heal banner in light mode for an online local runtime', () => {
    renderDialog({
      runtime: makeRuntime({ runtime_mode: 'local', status: 'online' }),
      cachedAgents: [],
    });
    expect(screen.getByText(/managed by a running local daemon/i)).toBeInTheDocument();
  });

  it('explains that deleting a profile-backed runtime only removes the current instance', () => {
    renderDialog({
      runtime: makeRuntime({
        runtime_mode: 'local',
        status: 'online',
        profile_id: 'profile-1',
      }),
      cachedAgents: [],
    });

    expect(screen.getByText(/registered from a custom runtime profile/i)).toBeInTheDocument();
    expect(screen.getByText(/delete the custom runtime profile/i)).toBeInTheDocument();
    expect(screen.queryByText(/managed by a running local daemon/i)).not.toBeInTheDocument();
  });

  it('shows the profile-backed runtime explanation in cascade mode', () => {
    renderDialog({
      runtime: makeRuntime({ profile_id: 'profile-1' }),
      cachedAgents: [makeAgent('a-1', { name: 'Alpha' })],
    });

    expect(screen.getByText(/registered from a custom runtime profile/i)).toBeInTheDocument();
    expect(screen.getByText(/Archive 1 agent and delete this Runtime/)).toBeInTheDocument();
  });

  it('renders the self-heal banner in cascade mode for an online local runtime with bound agents', () => {
    renderDialog({
      runtime: makeRuntime({ runtime_mode: 'local', status: 'online' }),
      cachedAgents: [makeAgent('a-1', { name: 'Alpha' })],
    });
    expect(screen.getByText(/managed by a running local daemon/i)).toBeInTheDocument();
    expect(screen.getByText(/Archive 1 agent and delete this Runtime/)).toBeInTheDocument();
  });

  it('does NOT render the self-heal banner for offline local or cloud runtimes', () => {
    const { unmount } = renderDialog({
      runtime: makeRuntime({ runtime_mode: 'local', status: 'offline' }),
      cachedAgents: [],
    });
    expect(screen.queryByText(/managed by a running local daemon/i)).not.toBeInTheDocument();
    unmount();

    renderDialog({
      runtime: makeRuntime({ runtime_mode: 'cloud', status: 'online' }),
      cachedAgents: [],
    });
    expect(screen.queryByText(/managed by a running local daemon/i)).not.toBeInTheDocument();
  });

  it('allows confirm to proceed on a self-healing runtime instead of toasting an error', async () => {
    apiDeleteRuntime.mockResolvedValueOnce({ status: 'ok' });

    const onDeleted = vi.fn();
    renderDialog({
      runtime: makeRuntime({ runtime_mode: 'local', status: 'online' }),
      cachedAgents: [],
      onDeleted,
    });

    fireEvent.click(screen.getByRole('button', { name: 'Delete runtime' }));
    await waitFor(() => expect(apiDeleteRuntime).toHaveBeenCalledWith('rt-1'));
    await waitFor(() => expect(onDeleted).toHaveBeenCalled());
  });
});
