// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { ReactNode } from 'react';
import { render, screen } from '@testing-library/react';
import type { Agent } from '@goosar/core/types';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../../locales/en/common.json';
import enAgents from '../../../locales/en/agents.json';
import enSettings from '../../../locales/en/settings.json';

type MemberRole = 'owner' | 'admin' | 'member' | 'guest';

const membersRef = vi.hoisted(() => ({
  current: [{ user_id: 'user-1', role: 'owner' as MemberRole }],
}));
const installationsRef = vi.hoisted(() => ({
  current: {
    installations: [] as unknown[],
    configured: true,
    install_supported: true,
  },
}));

vi.mock('@tanstack/react-query', () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) return { data: undefined };
    const key = JSON.stringify(opts.queryKey);
    if (key.includes('members')) return { data: membersRef.current };
    if (key.includes('installations')) return { data: installationsRef.current };
    return { data: undefined };
  },
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock('@goosar/core/hooks', () => ({
  useWorkspaceId: () => 'ws-1',
}));

vi.mock('@goosar/core/workspace/queries', () => ({
  memberListOptions: () => ({ queryKey: ['members'], queryFn: vi.fn() }),
}));

vi.mock('@goosar/core/slack', () => ({
  slackInstallationsOptions: () => ({
    queryKey: ['slack', 'installations'],
    queryFn: vi.fn(),
  }),
}));

vi.mock('@goosar/core/auth', () => {
  const useAuthStore = Object.assign(
    (sel?: (s: { user: { id: string } }) => unknown) =>
      sel ? sel({ user: { id: 'user-1' } }) : { user: { id: 'user-1' } },
    { getState: () => ({ user: { id: 'user-1' } }) },
  );
  return { useAuthStore };
});

vi.mock('../../../settings/components/slack-tab', () => ({
  SlackAgentBindButton: ({ agentId }: { agentId: string }) => (
    <div data-testid="slack-bind-button" data-agent-id={agentId} />
  ),
}));

import { IntegrationsTab } from './integrations-tab';

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, settings: enSettings },
};

const agent: Agent = {
  id: 'agent-1',
  workspace_id: 'ws-1',
  runtime_id: 'runtime-1',
  name: 'Agent',
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
  owner_id: 'user-1',
  skills: [],
  created_at: '2026-04-16T00:00:00Z',
  updated_at: '2026-04-16T00:00:00Z',
  archived_at: null,
  archived_by: null,
};

function renderTab(children: ReactNode) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>,
  );
}

function resetFixtures() {
  vi.clearAllMocks();
  membersRef.current = [{ user_id: 'user-1', role: 'owner' }];
  installationsRef.current = {
    installations: [],
    configured: true,
    install_supported: true,
  };
}

describe('IntegrationsTab', () => {
  beforeEach(resetFixtures);

  it('renders the Slack bind entry for an admin when configured and supported', () => {
    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByText('Slack')).toBeTruthy();
    expect(screen.getByTestId('slack-bind-button').getAttribute('data-agent-id')).toBe('agent-1');
  });

  it('shows the coming-soon notice when the install transport is not wired', () => {
    installationsRef.current = {
      installations: [],
      configured: true,
      install_supported: false,
    };
    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByText(/install coming soon/i)).toBeTruthy();
    expect(screen.queryByTestId('slack-bind-button')).toBeNull();
  });

  it('points members at Settings when they are not an admin', () => {
    membersRef.current = [{ user_id: 'user-1', role: 'member' }];
    renderTab(<IntegrationsTab agent={{ ...agent, owner_id: 'user-2' }} />);
    expect(screen.getByText(/Only workspace owners and admins can connect an agent/i)).toBeTruthy();
    expect(screen.queryByTestId('slack-bind-button')).toBeNull();
  });

  it('renders the bind entry (not coming-soon) when installs are unavailable but the agent is already bound', () => {
    installationsRef.current = {
      installations: [{ agent_id: 'agent-1', status: 'active' }],
      configured: true,
      install_supported: false,
    };
    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByTestId('slack-bind-button')).toBeTruthy();
    expect(screen.queryByText(/install coming soon/i)).toBeNull();
  });
});
