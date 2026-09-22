// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { Agent } from '@goosar/core/types';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enAgents from '../../locales/en/agents.json';
import { NavigationProvider, type NavigationAdapter } from '../../navigation';

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

vi.mock('./agent-overview-pane', () => ({
  AgentOverviewPane: () => <div>agent-overview-pane</div>,
}));
vi.mock('../../common/actor-avatar', () => ({
  ActorAvatar: () => <div>actor-avatar</div>,
}));
vi.mock('./agent-presence-indicator', () => ({
  AgentPresenceIndicator: () => null,
}));

const agentsRef = vi.hoisted(() => ({ current: [] as unknown[] }));
const membersRef = vi.hoisted(() => ({ current: [] as unknown[] }));
const membersPendingRef = vi.hoisted(() => ({ current: false }));
const currentUserRef = vi.hoisted(() => ({
  current: { id: 'user-1' } as { id: string } | null,
}));
const mockToastError = vi.hoisted(() => vi.fn());
const mockModalOpen = vi.hoisted(() => vi.fn());

vi.mock('@goosar/core/hooks', () => ({
  useWorkspaceId: () => 'ws-1',
}));
vi.mock('@goosar/core/agents', () => ({
  useWorkspacePresenceMap: () => ({ byAgent: new Map() }),
}));
vi.mock('@goosar/core/workspace/queries', () => ({
  agentListOptions: (wsId: string) => ({
    queryKey: ['agents', wsId],
    queryFn: () => Promise.resolve(agentsRef.current),
  }),
  memberListOptions: (wsId: string) => ({
    queryKey: ['members', wsId],
    queryFn: () =>
      membersPendingRef.current ? new Promise(() => {}) : Promise.resolve(membersRef.current),
  }),
  workspaceKeys: { agents: (wsId: string) => ['agents', wsId] },
}));
vi.mock('@goosar/core/runtimes', () => ({
  runtimeListOptions: (wsId: string) => ({
    queryKey: ['runtimes', wsId],
    queryFn: () => Promise.resolve([]),
  }),
}));
vi.mock('@goosar/core/auth', () => {
  type AuthState = { user: { id: string } | null };
  const state = (): AuthState => ({ user: currentUserRef.current });
  const useAuthStore = Object.assign(
    (selector?: (s: AuthState) => unknown) => (selector ? selector(state()) : state()),
    { getState: state },
  );
  return { useAuthStore };
});
vi.mock('@goosar/core/modals', () => ({
  useModalStore: Object.assign(vi.fn(), {
    getState: () => ({ open: mockModalOpen }),
  }),
}));
vi.mock('@goosar/core/paths', () => ({
  useWorkspacePaths: () => ({
    agents: () => '/acme/agents',
    chat: () => '/acme/chat',
  }),
}));
vi.mock('@goosar/core/api', () => {
  class ApiError extends Error {
    status: number;
    constructor(status: number, message: string) {
      super(message);
      this.status = status;
    }
  }
  return {
    api: { getAgent: vi.fn(() => Promise.reject(new ApiError(404, 'not found'))) },
    ApiError,
  };
});
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: mockToastError },
}));

import { AgentDetailPage } from './agent-detail-page';

const baseAgent: Agent = {
  id: 'agent-1',
  workspace_id: 'ws-1',
  runtime_id: 'runtime-1',
  name: 'Lambda',
  description: '',
  instructions: '',
  avatar_url: null,
  runtime_mode: 'local',
  runtime_config: {},
  custom_args: [],
  visibility: 'workspace',
  permission_mode: 'public_to',
  invocation_targets: [{ target_type: 'workspace', target_id: null }],
  status: 'idle',
  max_concurrent_tasks: 1,
  model: '',
  owner_id: 'user-2',
  skills: [],
  created_at: '2026-05-28T00:00:00Z',
  updated_at: '2026-05-28T00:00:00Z',
  archived_at: null,
  archived_by: null,
};

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const push = vi.fn();
  const navigation: NavigationAdapter = {
    push,
    replace: vi.fn(),
    back: vi.fn(),
    pathname: '/acme/agents/agent-1',
    searchParams: new URLSearchParams(),
    getShareableUrl: (path) => path,
  };
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={queryClient}>
          <AgentDetailPage agentId="agent-1" />
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
  return { push };
}

beforeEach(() => {
  vi.clearAllMocks();
  currentUserRef.current = { id: 'user-1' };
  membersRef.current = [{ user_id: 'user-1', role: 'member' }];
  membersPendingRef.current = false;
  agentsRef.current = [baseAgent];
});

describe('AgentDetailPage DM button', () => {
  it('navigates to the chat deep link when the user can chat with the agent', async () => {
    const { push } = renderPage();
    fireEvent.click(await screen.findByRole('button', { name: 'DM' }));
    expect(push).toHaveBeenCalledWith('/acme/chat?agent=agent-1');
    expect(mockToastError).not.toHaveBeenCalled();
  });

  it('shows a toast instead of navigating when the user lacks chat access', async () => {
    agentsRef.current = [{ ...baseAgent, permission_mode: 'private', invocation_targets: [] }];
    membersRef.current = [{ user_id: 'user-1', role: 'admin' }];
    const { push } = renderPage();
    fireEvent.click(await screen.findByRole('button', { name: 'DM' }));
    expect(mockToastError).toHaveBeenCalledWith("You don't have access to chat with this agent.");
    expect(push).not.toHaveBeenCalled();
  });

  it('disables DM while membership is resolving instead of toasting a false deny', async () => {
    membersPendingRef.current = true;
    const { push } = renderPage();
    const dm = await screen.findByRole('button', { name: 'DM' });
    expect(dm).toBeDisabled();
    fireEvent.click(dm);
    expect(mockToastError).not.toHaveBeenCalled();
    expect(push).not.toHaveBeenCalled();
  });

  it('hides the DM button on an archived agent', async () => {
    agentsRef.current = [{ ...baseAgent, archived_at: '2026-06-01T00:00:00Z' }];
    renderPage();
    await screen.findByText(/This agent is archived/);
    expect(screen.queryByRole('button', { name: 'DM' })).not.toBeInTheDocument();
  });
});
