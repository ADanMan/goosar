// @vitest-environment jsdom

import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '@goosar/core/i18n/react';
import { workspaceKeys } from '@goosar/core/workspace/queries';
import enCommon from '../locales/en/common.json';
import enOnboarding from '../locales/en/onboarding.json';
import enWorkspace from '../locales/en/workspace.json';
import { NavigationProvider, type NavigationAdapter } from '../navigation';

const TEST_RESOURCES = {
  en: { common: enCommon, onboarding: enOnboarding, workspace: enWorkspace },
};

const mocks = vi.hoisted(() => ({
  getEffectiveConfig: vi.fn(),
  listAgents: vi.fn(),
  listMembers: vi.fn(),
  updateAgent: vi.fn(),
}));

vi.mock('@goosar/core/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/api')>()),
  api: {
    getEffectiveConfig: mocks.getEffectiveConfig,
    listAgents: mocks.listAgents,
    listMembers: mocks.listMembers,
    updateAgent: mocks.updateAgent,
  },
}));

const memberRef = vi.hoisted(() => ({
  current: {
    userId: 'user-1',
    role: 'member',
    member: { perimeter_access: true } as { perimeter_access?: boolean } | null,
    isLoading: false,
  },
}));

vi.mock('@goosar/core/permissions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/permissions')>()),
  useCurrentMember: () => memberRef.current,
}));

vi.mock('@goosar/core/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/auth')>()),
  useAuthStore: Object.assign(
    (selector: (s: { user: unknown }) => unknown) => selector({ user: { id: 'user-1' } }),
    { getState: () => ({ user: { id: 'user-1' } }) },
  ),
}));

const workspaceRef = vi.hoisted(() => ({
  current: { id: 'ws-1', slug: 'acme' } as { id: string; slug: string } | null,
}));

vi.mock('@goosar/core/paths', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/paths')>();
  return {
    ...actual,
    useCurrentWorkspace: () => workspaceRef.current,
    useWorkspaceSlug: () => workspaceRef.current?.slug ?? null,
  };
});

import { buildHelperMcpConfig } from '../onboarding/presets';
import { _resetCredentialBannerDismissForTests } from './credential-banner-dismiss';
import { MissingCredentialsBanner } from './missing-credentials-banner';

function helperWith(config: unknown, extra: Record<string, unknown> = {}) {
  return {
    id: 'agent_helper',
    name: 'Goosar Helper',
    system_key: 'goosar_helper',
    visibility: 'workspace',
    owner_id: 'user-1',
    archived_at: null,
    runtime_id: 'rt-1',
    mcp_config: config,
    mcp_config_redacted: false,
    ...extra,
  };
}

const ROLE_WITH_BOTH_SERVERS = {
  schema_version: 1,
  mcp: {
    'ews-mcp': { enabled: true, has_env: false, origin: 'workspace' },
    'b24-agent': { enabled: true, has_env: false, origin: 'workspace' },
  },
};

function catalogInUse(): Record<string, unknown> {
  const doc = JSON.parse(JSON.stringify(buildHelperMcpConfig()));
  const servers = doc.mcpServers as Record<string, { enabled?: boolean }>;
  for (const name of ['atlassian', 'fetch', 'mcp-gateway']) {
    servers[name] = { ...servers[name], enabled: true };
  }
  return doc;
}

function fullyConfigured(): Record<string, unknown> {
  const doc = catalogInUse();
  const servers = doc.mcpServers as Record<
    string,
    { env?: Record<string, string>; args?: string[] }
  >;
  servers.atlassian!.env!.JIRA_PERSONAL_TOKEN = 'j';
  servers.atlassian!.env!.CONFLUENCE_PERSONAL_TOKEN = 'c';
  servers.outlook!.env!.EWS_EMAIL = 'me@corp.example';
  servers.bitrix24!.env!.B24_WEBHOOK_URL = `${servers.bitrix24!.env!.B24_WEBHOOK_URL}1/abc/`;
  servers.bitrix24!.env!.KB_API_TOKEN = 'kb';
  servers['mcp-gateway']!.env!.API_ACCESS_TOKEN = 't';
  return doc;
}

function renderBanner() {
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: '/acme/issues',
    searchParams: new URLSearchParams(),
    getShareableUrl: (path) => path,
  };
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const ui = (children: ReactNode) => (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={client}>
        <NavigationProvider value={navigation}>{children}</NavigationProvider>
      </QueryClientProvider>
    </I18nProvider>
  );
  const view = render(ui(<MissingCredentialsBanner />));
  return { navigation, client, unmount: view.unmount };
}

const bannerTitle = /some work tools are still missing your keys/i;

async function expectSilent() {
  await waitFor(() => expect(mocks.listAgents).toHaveBeenCalled());
  for (let i = 0; i < 2; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  }
  expect(screen.queryByText(bannerTitle)).toBeNull();
}

beforeEach(() => {
  vi.clearAllMocks();
  _resetCredentialBannerDismissForTests();
  workspaceRef.current = { id: 'ws-1', slug: 'acme' };
  memberRef.current = {
    userId: 'user-1',
    role: 'member',
    member: { perimeter_access: true },
    isLoading: false,
  };
  mocks.listMembers.mockResolvedValue([{ user_id: 'user-1', perimeter_access: true }]);
  mocks.updateAgent.mockImplementation((_id: string, patch: unknown) =>
    Promise.resolve(helperWith((patch as { mcp_config?: unknown }).mcp_config)),
  );
  mocks.getEffectiveConfig.mockResolvedValue(ROLE_WITH_BOTH_SERVERS);
  mocks.listAgents.mockResolvedValue([helperWith(catalogInUse())]);
});

afterEach(() => {
  _resetCredentialBannerDismissForTests();
});

describe('what the banner claims', () => {
  it('names exactly the services still missing a personal key', async () => {
    const doc = catalogInUse();
    const servers = doc.mcpServers as Record<string, { env: Record<string, string> }>;
    servers.atlassian!.env.JIRA_PERSONAL_TOKEN = 'j';
    servers.atlassian!.env.CONFLUENCE_PERSONAL_TOKEN = 'c';
    servers.outlook!.env.EWS_EMAIL = 'me@corp.example';
    mocks.listAgents.mockResolvedValue([helperWith(doc)]);

    renderBanner();

    await screen.findByText(bannerTitle);
    const body = screen.getByText(/the assistant cannot work with these yet/i);
    expect(body.textContent).toContain('Bitrix24 & knowledge base');
    expect(body.textContent).toContain('Extra work tools');
    expect(body.textContent).not.toContain('Jira & Confluence');
    expect(body.textContent).not.toContain('Outlook mail & calendar');
    expect(body.textContent).not.toContain('Web access');
  });

  it('disappears once every key is entered', async () => {
    mocks.listAgents.mockResolvedValue([helperWith(fullyConfigured())]);
    renderBanner();

    await expectSilent();
  });

  it('leads to the capabilities page rather than back through onboarding', async () => {
    const { navigation } = renderBanner();
    await screen.findByText(bannerTitle);

    await userEvent.click(screen.getByRole('button', { name: /show me what to do/i }));
    expect(navigation.push).toHaveBeenCalledWith('/acme/capabilities?from=keys');
  });

  it('goes quiet the moment the saved key lands in the shared agent cache', async () => {
    const { client } = renderBanner();
    await screen.findByText(bannerTitle);

    mocks.listAgents.mockResolvedValue([helperWith(fullyConfigured())]);
    await act(async () => {
      await client.invalidateQueries({
        queryKey: workspaceKeys.agents('ws-1'),
      });
    });

    await waitFor(() => expect(screen.queryByText(bannerTitle)).toBeNull());
  });
});

describe('when the banner must stay silent', () => {
  it('says nothing about services this workspace does not have', async () => {
    mocks.getEffectiveConfig.mockResolvedValue({ schema_version: 1 });
    mocks.listAgents.mockResolvedValue([helperWith(buildHelperMcpConfig())]);
    renderBanner();
    await expectSilent();
  });

  it('says nothing when a configuration layer already supplies the values', async () => {
    mocks.getEffectiveConfig.mockResolvedValue({
      schema_version: 1,
      mcp: { 'ews-mcp': { enabled: true, has_env: true, origin: 'workspace' } },
    });
    mocks.listAgents.mockResolvedValue([helperWith(buildHelperMcpConfig())]);
    renderBanner();
    await expectSilent();
  });

  it('says nothing when the member has no Helper yet', async () => {
    mocks.listAgents.mockResolvedValue([]);
    renderBanner();
    await expectSilent();
  });

  it('says nothing when the configuration is redacted for this viewer', async () => {
    mocks.listAgents.mockResolvedValue([helperWith(null, { mcp_config_redacted: true })]);
    renderBanner();
    await expectSilent();
  });

  it('says nothing to a member without access to the corporate tools', async () => {
    memberRef.current = {
      userId: 'user-1',
      role: 'member',
      member: { perimeter_access: false },
      isLoading: false,
    };
    const { configStore } = await import('@goosar/core/config');
    configStore.setState({ deliveryProfile: 'perimeter' } as never);
    try {
      renderBanner();
      await expectSilent();
    } finally {
      configStore.setState({ deliveryProfile: undefined } as never);
    }
  });

  it('says nothing before there is a workspace to speak about', async () => {
    workspaceRef.current = null;
    renderBanner();
    expect(screen.queryByText(bannerTitle)).toBeNull();
    expect(mocks.listAgents).not.toHaveBeenCalled();
  });
});

describe('what it costs to be reminded', () => {
  it("never writes to the member's agent", async () => {
    mocks.listAgents.mockResolvedValue([helperWith(null)]);
    renderBanner();
    await expectSilent();
    expect(mocks.updateAgent).not.toHaveBeenCalled();
    expect(mocks.listMembers).not.toHaveBeenCalled();
  });
});

describe('how often it speaks', () => {
  it('stops speaking about a list the reader dismissed, without asking again', async () => {
    const { unmount } = renderBanner();
    await screen.findByText(bannerTitle);
    await userEvent.click(screen.getByRole('button', { name: /^dismiss$/i }));
    expect(screen.queryByText(bannerTitle)).toBeNull();
    unmount();

    renderBanner();
    await expectSilent();
  });

  it('runs out on its own for a reader who never touches it', async () => {
    for (let visit = 0; visit < 3; visit++) {
      const { unmount } = renderBanner();
      await screen.findByText(bannerTitle);
      unmount();
    }

    renderBanner();
    await expectSilent();
  });

  it("keeps one workspace's silence out of another workspace's reminder", async () => {
    const first = renderBanner();
    await screen.findByText(bannerTitle);
    await userEvent.click(screen.getByRole('button', { name: /^dismiss$/i }));
    first.unmount();

    workspaceRef.current = { id: 'ws-2', slug: 'beta' };
    renderBanner();
    expect(await screen.findByText(bannerTitle)).toBeInTheDocument();
  });

  it('comes back when a NEW service starts needing a key', async () => {
    const almostDone = fullyConfigured();
    const servers = (almostDone as { mcpServers: Record<string, { env?: Record<string, string> }> })
      .mcpServers;
    servers.outlook!.env!.EWS_EMAIL = '';
    mocks.listAgents.mockResolvedValue([helperWith(almostDone)]);

    const first = renderBanner();
    await screen.findByText(bannerTitle);
    await userEvent.click(screen.getByRole('button', { name: /^dismiss$/i }));
    first.unmount();

    const quiet = renderBanner();
    await expectSilent();
    quiet.unmount();

    servers.atlassian!.env!.JIRA_PERSONAL_TOKEN = '';
    mocks.listAgents.mockResolvedValue([helperWith(almostDone)]);
    renderBanner();
    expect(await screen.findByText(bannerTitle)).toBeInTheDocument();
  });
});
