// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enSettings from '../../locales/en/settings.json';
import enAgents from '../../locales/en/agents.json';
import { McpTab } from './mcp-tab';
import { SharedMcpServersSection } from '../../agents/components/tabs/shared-mcp-servers-section';

const TEST_RESOURCES = {
  en: { common: enCommon, settings: enSettings, agents: enAgents },
};

const mockListWorkspaceMcpServers = vi.hoisted(() => vi.fn());
const mockListAgentMcpServers = vi.hoisted(() => vi.fn());
const mockSetWorkspaceMcpCredentials = vi.hoisted(() => vi.fn());

vi.mock('@goosar/core/api', () => ({
  api: {
    listWorkspaceMcpServers: (...args: unknown[]) => mockListWorkspaceMcpServers(...args),
    listAgentMcpServers: (...args: unknown[]) => mockListAgentMcpServers(...args),
    setWorkspaceMcpCredentials: (...args: unknown[]) => mockSetWorkspaceMcpCredentials(...args),
    createWorkspaceMcpServer: vi.fn(),
    updateWorkspaceMcpServer: vi.fn(),
    deleteWorkspaceMcpServer: vi.fn(),
    clearWorkspaceMcpCredentials: vi.fn(),
    addAgentMcpServer: vi.fn(),
    setAgentMcpServerEnabled: vi.fn(),
    removeAgentMcpServer: vi.fn(),
  },
}));

vi.mock('@goosar/core/permissions', () => ({
  useCurrentMember: () => ({ role: 'member' }),
}));

const JIRA_SERVER = {
  id: 's-1',
  workspace_id: 'ws-1',
  name: 'atlassian',
  transport: 'stdio',
  credential_schema: [
    {
      key: 'JIRA_TOKEN',
      label: 'Jira API token',
      hint: 'Atlassian profile → Security → Create and manage API tokens',
      required: true,
    },
  ],
  provided_credentials: [],
  missing_credentials: ['JIRA_TOKEN'],
};

function renderWith(ui: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        {ui}
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe('shared MCP credentials — refusal UX (#347)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListWorkspaceMcpServers.mockResolvedValue([]);
    mockListAgentMcpServers.mockResolvedValue([]);
    mockSetWorkspaceMcpCredentials.mockResolvedValue(undefined);
  });

  it('names the missing field instead of showing a server that silently will not run', async () => {
    mockListWorkspaceMcpServers.mockResolvedValue([JIRA_SERVER]);
    renderWith(<McpTab wsId="ws-1" />);

    expect(await screen.findByText(/JIRA_TOKEN/)).toBeTruthy();
    expect(screen.getByText(/left out of your agents' tasks/i)).toBeTruthy();
  });

  it('gives a plain member the connect control — this is not an admin action', async () => {
    mockListWorkspaceMcpServers.mockResolvedValue([JIRA_SERVER]);
    renderWith(<McpTab wsId="ws-1" />);

    const connect = await screen.findByRole('button', { name: /connect/i });
    await userEvent.click(connect);

    expect(await screen.findByText(/Create and manage API tokens/)).toBeTruthy();
  });

  it('sends only the fields the user actually typed, and never prefills a value', async () => {
    mockListWorkspaceMcpServers.mockResolvedValue([JIRA_SERVER]);
    renderWith(<McpTab wsId="ws-1" />);

    await userEvent.click(await screen.findByRole('button', { name: /connect/i }));
    const input = await screen.findByLabelText(/Jira API token/i);
    expect((input as HTMLInputElement).value).toBe('');
    expect((input as HTMLInputElement).type).toBe('password');

    await userEvent.type(input, 'my-own-token');
    await userEvent.click(screen.getByRole('button', { name: /save credentials/i }));

    await waitFor(() => {
      expect(mockSetWorkspaceMcpCredentials).toHaveBeenCalledWith('ws-1', 's-1', {
        JIRA_TOKEN: 'my-own-token',
      });
    });
  });

  it('does not claim the integration works once a value is supplied', async () => {
    mockListWorkspaceMcpServers.mockResolvedValue([
      {
        ...JIRA_SERVER,
        provided_credentials: ['JIRA_TOKEN'],
        missing_credentials: [],
      },
    ]);
    renderWith(<McpTab wsId="ws-1" />);

    expect(await screen.findByText(/does not prove the server answers/i)).toBeTruthy();
  });

  it("tells an agent's viewer that the OWNER's missing credentials block it", async () => {
    mockListAgentMcpServers.mockResolvedValue([{ ...JIRA_SERVER, enabled: true }]);
    renderWith(<SharedMcpServersSection wsId="ws-1" agentId="a-1" canEdit={false} />);

    expect(await screen.findByText(/its owner has not supplied JIRA_TOKEN/i)).toBeTruthy();
    expect(screen.getByText(/Create and manage API tokens/)).toBeTruthy();
  });

  it('says nothing about credentials for a server that asks for none', async () => {
    mockListAgentMcpServers.mockResolvedValue([
      {
        id: 's-2',
        workspace_id: 'ws-1',
        name: 'plain',
        transport: 'http',
        enabled: true,
        credential_schema: [],
        provided_credentials: [],
        missing_credentials: [],
      },
    ]);
    renderWith(<SharedMcpServersSection wsId="ws-1" agentId="a-1" canEdit={false} />);

    expect(await screen.findByText('plain')).toBeTruthy();
    expect(screen.queryByText(/has not supplied/i)).toBeNull();
  });
});
