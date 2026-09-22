// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { Agent, AgentRuntime } from '@goosar/core/types';
import { ApiError } from '@goosar/core/api';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../../locales/en/common.json';
import enAgents from '../../../locales/en/agents.json';
import { McpConfigTab } from './mcp-config-tab';

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mockRuntimeCapabilities = vi.hoisted(() => vi.fn());

vi.mock('@goosar/core/runtimes', async () => {
  const actual =
    await vi.importActual<typeof import('@goosar/core/runtimes')>('@goosar/core/runtimes');
  return {
    ...actual,
    runtimeCapabilitiesOptions: (runtimeId: string | null) => ({
      queryKey: ['runtime-capabilities', runtimeId],
      queryFn: () => mockRuntimeCapabilities(runtimeId),
      enabled: Boolean(runtimeId),
      retry: false,
    }),
  };
});

vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

const baseAgent: Agent = {
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
  created_at: '2026-05-28T00:00:00Z',
  updated_at: '2026-05-28T00:00:00Z',
  archived_at: null,
  archived_by: null,
};

function TestShell({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    </I18nProvider>
  );
}

function renderTab(
  overrides: Partial<Agent> = {},
  onSave = vi.fn().mockResolvedValue(undefined),
  runtime: AgentRuntime | null = null,
  canEdit = true,
  isOwner = true,
) {
  const result = render(
    <TestShell>
      <McpConfigTab
        agent={{ ...baseAgent, ...overrides }}
        runtime={runtime}
        canEdit={canEdit}
        isOwner={isOwner}
        onSave={onSave}
      />
    </TestShell>,
  );
  return { ...result, onSave };
}

const onlineRuntime: AgentRuntime = {
  id: 'runtime-1',
  workspace_id: 'ws-1',
  daemon_id: 'daemon-1',
  name: 'Claude (Mac)',
  runtime_mode: 'local',
  provider: 'claude',
  launch_header: '',
  status: 'online',
  device_info: 'Mac',
  metadata: {},
  owner_id: 'user-1',
  visibility: 'private',
  last_seen_at: null,
  created_at: '2026-07-11T00:00:00Z',
  updated_at: '2026-07-11T00:00:00Z',
};

describe('McpConfigTab', () => {
  beforeEach(() => vi.clearAllMocks());

  it('renders redacted managed MCP without exposing add or edit controls', () => {
    renderTab({ mcp_config: null, mcp_config_redacted: true });

    expect(screen.getByText(/hidden from your view/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /add mcp/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  });

  it('offers no Add to a non-owner even when there is nothing to redact', () => {
    renderTab({ mcp_config: null, mcp_config_redacted: false }, vi.fn(), null, true, false);
    expect(screen.queryByRole('button', { name: /add mcp/i })).not.toBeInTheDocument();
  });

  it("offers Add to the agent's owner", () => {
    renderTab({ mcp_config: null, mcp_config_redacted: false });
    expect(screen.getByRole('button', { name: /add mcp/i })).toBeInTheDocument();
  });

  it('projects historical aggregate config into individually managed rows', () => {
    renderTab({
      mcp_config: {
        version: 1,
        mcpServers: {
          fetch: { command: 'uvx', args: ['mcp-server-fetch'] },
          docs: { type: 'http', url: 'https://example.test/mcp' },
        },
      },
    });

    expect(screen.getByText('fetch')).toBeInTheDocument();
    expect(screen.getByText('docs')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /managed by goosar/i })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /inherited from runtime/i })).toBeInTheDocument();
    expect(screen.queryByLabelText(/MCP config JSON editor/i)).not.toBeInTheDocument();
  });

  it('flags an already-saved server whose configuration cannot start', () => {
    renderTab({
      mcp_config: {
        mcpServers: {
          broken: { command: 'npx -y @modelcontextprotocol/server-filesystem' },
          fine: { command: '/usr/local/bin/mcp-server' },
        },
      },
    });

    const broken = screen.getByText('broken').closest('li');
    const fine = screen.getByText('fine').closest('li');

    expect(broken).not.toBeNull();
    expect(broken).toHaveTextContent(/invalid config/i);
    expect(fine).not.toHaveTextContent(/invalid config/i);
  });

  it('flags an already-saved server that has to reach a public registry to start', () => {
    renderTab({
      mcp_config: { mcpServers: { fetch: { command: 'uvx', args: ['x'] } } },
    });

    expect(screen.getByText('fetch').closest('li')).toHaveTextContent(/public registry/i);
  });

  it('adds one stdio server through the form and preserves historical top-level data', async () => {
    const user = userEvent.setup();
    const { onSave } = renderTab({ mcp_config: { version: 1 } });

    await user.click(screen.getByRole('button', { name: /add mcp/i }));
    await user.type(screen.getByLabelText('Name'), 'fetch');
    await user.type(screen.getByLabelText('Command'), 'uvx');
    await user.click(screen.getByRole('button', { name: /add server/i }));

    expect(onSave).toHaveBeenCalledWith({
      mcp_config: {
        version: 1,
        mcpServers: { fetch: { command: 'uvx' } },
      },
    });
  });

  it('adds one HTTP server through JSON mode', async () => {
    const user = userEvent.setup();
    const { onSave } = renderTab();

    await user.click(screen.getByRole('button', { name: /add mcp/i }));
    await user.type(screen.getByLabelText('Name'), 'weather');
    await user.click(screen.getByRole('tab', { name: 'JSON' }));
    fireEvent.change(screen.getByLabelText(/MCP server JSON configuration/i), {
      target: {
        value: JSON.stringify({
          type: 'http',
          url: 'https://example.invalid/mcp',
        }),
      },
    });
    await user.click(screen.getByRole('button', { name: /add server/i }));

    expect(onSave).toHaveBeenCalledWith({
      mcp_config: {
        mcpServers: {
          weather: {
            type: 'http',
            url: 'https://example.invalid/mcp',
          },
        },
      },
    });
  });

  it('edits one historical server without replacing its siblings', async () => {
    const user = userEvent.setup();
    const existing = {
      version: 1,
      mcpServers: {
        fetch: {
          command: 'uvx',
          timeout: 30,
          tools: { include: ['fetch_url'] },
        },
        docs: { url: 'https://example.test/mcp' },
      },
    };
    const { onSave } = renderTab({ mcp_config: existing });

    await user.click(screen.getByRole('button', { name: /edit mcp server fetch/i }));
    const command = screen.getByLabelText('Command');
    await user.clear(command);
    await user.type(command, 'npx');
    await user.click(screen.getByRole('button', { name: /save changes/i }));

    expect(onSave).toHaveBeenCalledWith({
      mcp_config: {
        version: 1,
        mcpServers: {
          fetch: {
            timeout: 30,
            tools: { include: ['fetch_url'] },
            command: 'npx',
          },
          docs: { url: 'https://example.test/mcp' },
        },
      },
    });
  });

  it('deletes the last managed server only after confirmation', async () => {
    const user = userEvent.setup();
    const { onSave } = renderTab({
      mcp_config: { mcpServers: { fetch: { command: 'uvx' } } },
    });

    await user.click(screen.getByRole('button', { name: /delete mcp server fetch/i }));
    expect(screen.getByText(/runtime servers are not affected/i)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /delete server/i }));

    expect(onSave).toHaveBeenCalledWith({ mcp_config: { mcpServers: {} } });
  });

  it('blocks invalid single-server JSON', async () => {
    const user = userEvent.setup();
    const { onSave } = renderTab();

    await user.click(screen.getByRole('button', { name: /add mcp/i }));
    await user.type(screen.getByLabelText('Name'), 'broken');
    await user.click(screen.getByRole('tab', { name: 'JSON' }));
    fireEvent.change(screen.getByLabelText(/MCP server JSON configuration/i), {
      target: { value: '{not json' },
    });

    expect(screen.getByText(/invalid json/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /add server/i })).toBeDisabled();
    expect(onSave).not.toHaveBeenCalled();
  });

  it('lists inherited MCP servers discovered from the assigned runtime', async () => {
    mockRuntimeCapabilities.mockResolvedValue({
      skills: [],
      supported: true,
      mcpServers: [{ name: 'linear', transport: 'http', source: 'User config', enabled: true }],
      mcpSupported: true,
    });

    renderTab({}, undefined, onlineRuntime);

    expect(await screen.findByText('linear')).toBeInTheDocument();
  });

  it('shows a permission notice when capability discovery is forbidden', async () => {
    mockRuntimeCapabilities.mockRejectedValue(
      new ApiError('insufficient permissions', 403, 'Forbidden'),
    );

    renderTab({}, undefined, onlineRuntime);

    expect(
      await screen.findByText("You don't have permission to view this runtime's MCP servers."),
    ).toBeInTheDocument();
  });

  it('shows a retry notice when capability discovery fails', async () => {
    mockRuntimeCapabilities.mockRejectedValue(new Error('daemon did not respond within 3 minutes'));

    renderTab({}, undefined, onlineRuntime);

    expect(
      await screen.findByText("Couldn't discover runtime MCP servers. Try again."),
    ).toBeInTheDocument();
  });

  const maskedConfig = {
    mcpServers: {
      jira: { __goosar_masked__: true },
      confluence: { __goosar_masked__: true },
    },
  };

  it('lists masked servers by name and keeps delete — but not add — available', () => {
    renderTab({ mcp_config: maskedConfig, mcp_config_redacted: true }, vi.fn(), null, true, false);

    expect(screen.getByText('jira')).toBeInTheDocument();
    expect(screen.getByText('confluence')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /delete mcp server jira/i })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /add mcp/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /edit mcp server jira/i })).not.toBeInTheDocument();
    expect(screen.getByText('jira').closest('li')).not.toHaveTextContent(/invalid config/i);
  });

  it('explains why the values are missing instead of claiming admins can read them', () => {
    renderTab({ mcp_config: maskedConfig, mcp_config_redacted: true });

    const notice = screen.getByTestId('mcp-masked-notice');
    expect(notice).toHaveTextContent(/owner/i);
    expect(notice).not.toHaveTextContent(/workspace admin can read/i);
  });

  it('deletes one masked server and sends the other back as a placeholder', async () => {
    const user = userEvent.setup();
    const { onSave } = renderTab({
      mcp_config: maskedConfig,
      mcp_config_redacted: true,
    });

    await user.click(screen.getByRole('button', { name: /delete mcp server jira/i }));
    await user.click(screen.getByRole('button', { name: /delete server/i }));

    expect(onSave).toHaveBeenCalledWith({
      mcp_config: {
        mcpServers: { confluence: { __goosar_masked__: true } },
      },
    });
  });

  it('offers no editing surface at all to a viewer who cannot write the agent', () => {
    renderTab({ mcp_config: { mcpServers: { jira: { command: 'x' } } } }, undefined, null, false);

    expect(screen.getByText('jira')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /add mcp/i })).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /delete mcp server jira/i }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /edit mcp server jira/i })).not.toBeInTheDocument();
  });

  it('still shows the plain lock when there is nothing manageable to list', () => {
    renderTab({ mcp_config: null, mcp_config_redacted: true });

    expect(screen.getByText(/hidden from your view/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /add mcp/i })).not.toBeInTheDocument();
  });
});
