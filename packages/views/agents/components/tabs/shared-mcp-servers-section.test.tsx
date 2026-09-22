// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../../locales/en/common.json';
import enAgents from '../../../locales/en/agents.json';
import { SharedMcpServersSection } from './shared-mcp-servers-section';

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mockListAgentMcpServers = vi.hoisted(() => vi.fn());
const mockListWorkspaceMcpServers = vi.hoisted(() => vi.fn());
const mockSetAgentMcpServerEnabled = vi.hoisted(() => vi.fn());
const mockAddAgentMcpServer = vi.hoisted(() => vi.fn());
const mockRemoveAgentMcpServer = vi.hoisted(() => vi.fn());

vi.mock('@goosar/core/api', () => ({
  api: {
    listAgentMcpServers: (...args: unknown[]) => mockListAgentMcpServers(...args),
    listWorkspaceMcpServers: (...args: unknown[]) => mockListWorkspaceMcpServers(...args),
    setAgentMcpServerEnabled: (...args: unknown[]) => mockSetAgentMcpServerEnabled(...args),
    addAgentMcpServer: (...args: unknown[]) => mockAddAgentMcpServer(...args),
    removeAgentMcpServer: (...args: unknown[]) => mockRemoveAgentMcpServer(...args),
  },
}));

function renderSection(canEdit = true) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <SharedMcpServersSection wsId="ws-1" agentId="a-1" canEdit={canEdit} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe('SharedMcpServersSection', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListAgentMcpServers.mockResolvedValue([]);
    mockListWorkspaceMcpServers.mockResolvedValue([]);
  });

  it('shows the assigned servers with their transport, never a configuration', async () => {
    mockListAgentMcpServers.mockResolvedValue([
      {
        id: 's-1',
        workspace_id: 'ws-1',
        name: 'jira',
        transport: 'http',
        enabled: true,
        url: 'https://mcp.example/session/leaked-bearer-token',
        headers: { Authorization: 'Bearer leaked-bearer-token' },
        env: { API_KEY: 'leaked-env-secret' },
      },
    ]);

    const { container } = renderSection();

    expect(await screen.findByText('jira')).toBeTruthy();
    expect(screen.getByText('http')).toBeTruthy();
    expect(container.textContent).not.toContain('https://');
    expect(container.textContent).not.toContain('leaked-bearer-token');
    expect(container.textContent).not.toContain('leaked-env-secret');
    expect(container.textContent).not.toContain('mcp.example');
  });

  it("tells the assigner that assignment discloses the secret to the agent's task audience", async () => {
    renderSection();

    expect(await screen.findByText(enAgents.shared_mcp.assign_warning)).toBeTruthy();
  });

  it('does not show the disclosure warning to a viewer without assign controls', async () => {
    renderSection(false);

    await screen.findByText(enAgents.shared_mcp.empty);
    expect(screen.queryByText(enAgents.shared_mcp.assign_warning)).toBeNull();
  });

  it('says so when nothing is assigned, rather than showing the library as if it were', async () => {
    mockListWorkspaceMcpServers.mockResolvedValue([
      { id: 's-1', workspace_id: 'ws-1', name: 'jira', transport: 'http' },
    ]);

    renderSection();

    expect(await screen.findByText(enAgents.shared_mcp.empty)).toBeTruthy();
    expect(screen.queryByRole('switch')).toBeNull();
  });

  it('toggles one assignment without dropping it', async () => {
    mockListAgentMcpServers.mockResolvedValue([
      { id: 's-1', workspace_id: 'ws-1', name: 'jira', transport: 'http', enabled: true },
    ]);
    mockSetAgentMcpServerEnabled.mockResolvedValue([
      { id: 's-1', workspace_id: 'ws-1', name: 'jira', transport: 'http', enabled: false },
    ]);

    renderSection();
    const toggle = await screen.findByRole('switch');
    await userEvent.click(toggle);

    await waitFor(() => {
      expect(mockSetAgentMcpServerEnabled).toHaveBeenCalledWith('ws-1', 'a-1', 's-1', false);
    });
    expect(screen.getByText('jira')).toBeTruthy();
  });

  it('offers no write affordances to a viewer who cannot manage the agent', async () => {
    mockListAgentMcpServers.mockResolvedValue([
      { id: 's-1', workspace_id: 'ws-1', name: 'jira', transport: 'http', enabled: true },
    ]);

    renderSection(false);

    const toggle = await screen.findByRole('switch');
    expect(toggle.getAttribute('data-disabled')).not.toBeNull();
    expect(screen.queryByText(enAgents.shared_mcp.assign)).toBeNull();
  });

  it('treats a missing enabled flag as off', async () => {
    mockListAgentMcpServers.mockResolvedValue([
      { id: 's-1', workspace_id: 'ws-1', name: 'jira', transport: 'http' },
    ]);

    renderSection();

    const toggle = await screen.findByRole('switch');
    expect(toggle.getAttribute('aria-checked')).toBe('false');
  });
});
