// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enAgents from '../../locales/en/agents.json';
import enSettings from '../../locales/en/settings.json';
import { McpTab } from './mcp-tab';

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, settings: enSettings },
};
const DIALOG_COPY = enAgents.tab_body.mcp_config;

const mockListWorkspaceMcpServers = vi.hoisted(() => vi.fn());
const mockListOffers = vi.hoisted(() => vi.fn());
const mockSetOfferEnabled = vi.hoisted(() => vi.fn());

vi.mock('@goosar/core/api', () => ({
  api: {
    listWorkspaceMcpServers: (...args: unknown[]) => mockListWorkspaceMcpServers(...args),
    createWorkspaceMcpServer: vi.fn(),
    updateWorkspaceMcpServer: vi.fn(),
    deleteWorkspaceMcpServer: vi.fn(),
    listWorkspaceDeploymentMcpServers: (...args: unknown[]) => mockListOffers(...args),
    setWorkspaceDeploymentMcpServerEnabled: (...args: unknown[]) => mockSetOfferEnabled(...args),
  },
}));

const memberRef = vi.hoisted(() => ({
  current: { userId: 'user-1', role: 'owner', member: null, isLoading: false },
}));

vi.mock('@goosar/core/permissions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/permissions')>()),
  useCurrentMember: () => memberRef.current,
}));

function renderTab() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <McpTab wsId="ws-1" />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

async function openEditDialog() {
  const user = userEvent.setup();
  await user.click(
    await screen.findByRole('button', {
      name: enSettings.mcp.edit_server,
    }),
  );
  await screen.findByText(DIALOG_COPY.dialog_edit_title);
  return user;
}

describe('McpTab edit dialog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    memberRef.current = {
      userId: 'user-1',
      role: 'owner',
      member: null,
      isLoading: false,
    };
  });

  it('opens the edit dialog on the STDIO tab for a stdio server', async () => {
    mockListWorkspaceMcpServers.mockResolvedValue([
      { id: 's-1', workspace_id: 'ws-1', name: 'files', transport: 'stdio' },
    ]);

    renderTab();
    await openEditDialog();

    expect(screen.getByRole('button', { name: DIALOG_COPY.dialog_type_stdio })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    const command = screen.getByLabelText(DIALOG_COPY.dialog_command_label) as HTMLInputElement;
    expect(command.value).toBe('');
  });

  it('opens the edit dialog on the HTTP tab with an empty URL, even when the raw response leaks one', async () => {
    mockListWorkspaceMcpServers.mockResolvedValue([
      {
        id: 's-2',
        workspace_id: 'ws-1',
        name: 'jira',
        transport: 'http',
        url: 'https://mcp.example/session/leaked-token',
        headers: { Authorization: 'Bearer leaked-token' },
      },
    ]);

    renderTab();
    await openEditDialog();

    expect(screen.getByRole('button', { name: DIALOG_COPY.dialog_type_http })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    const url = screen.getByLabelText(DIALOG_COPY.dialog_url_label) as HTMLInputElement;
    expect(url.value).toBe('');
    expect(document.body.textContent).not.toContain('leaked-token');
  });
});

describe('McpTab and the deployment library', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    memberRef.current = {
      userId: 'user-1',
      role: 'owner',
      member: null,
      isLoading: false,
    };
    mockListOffers.mockResolvedValue([]);
    mockSetOfferEnabled.mockResolvedValue(undefined);
  });

  it('offers no edit or delete on a record the deployment owns', async () => {
    mockListWorkspaceMcpServers.mockResolvedValue([
      {
        id: 's1',
        workspace_id: 'ws-1',
        name: 'corp-jira',
        transport: 'http',
        source: 'deployment',
      },
    ]);
    renderTab();

    expect(await screen.findByText('corp-jira')).toBeTruthy();
    expect(screen.queryByRole('button', { name: enSettings.mcp.edit_server })).toBeNull();
    expect(screen.queryByRole('button', { name: enSettings.mcp.remove_server })).toBeNull();
    expect(document.body.textContent).toContain(enSettings.mcp.deployment_badge);
  });

  it("keeps the workspace's own entries editable beside them", async () => {
    mockListWorkspaceMcpServers.mockResolvedValue([
      {
        id: 's1',
        workspace_id: 'ws-1',
        name: 'own-server',
        transport: 'http',
        source: 'workspace',
      },
    ]);
    renderTab();

    expect(await screen.findByRole('button', { name: enSettings.mcp.edit_server })).toBeTruthy();
  });

  it('takes an offered record through the enablement API', async () => {
    mockListWorkspaceMcpServers.mockResolvedValue([]);
    mockListOffers.mockResolvedValue([
      { id: 'd1', name: 'corp-jira', transport: 'http', enabled: false },
    ]);
    renderTab();
    const user = userEvent.setup();

    const toggle = await screen.findByRole('switch', {
      name: enSettings.mcp.offer_enable_aria.replace('{{name}}', 'corp-jira'),
    });
    await user.click(toggle);

    await waitFor(() => expect(mockSetOfferEnabled).toHaveBeenCalledTimes(1));
    expect(mockSetOfferEnabled.mock.calls[0]).toEqual(['ws-1', 'd1', true]);
  });

  it('does not claim the workspace declined a record the server left unstated', async () => {
    mockListWorkspaceMcpServers.mockResolvedValue([]);
    mockListOffers.mockResolvedValue([{ id: 'd1', name: 'corp-jira', transport: 'http' }]);
    renderTab();

    const toggle = await screen.findByRole('switch', {
      name: enSettings.mcp.offer_enable_aria.replace('{{name}}', 'corp-jira'),
    });
    expect(toggle.getAttribute('aria-checked')).toBe('false');
  });
});
