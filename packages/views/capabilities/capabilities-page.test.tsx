// @vitest-environment jsdom

import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '@goosar/core/i18n/react';
import type { WorkspaceCapabilities } from '@goosar/core/types';
import enCommon from '../locales/en/common.json';
import enOnboarding from '../locales/en/onboarding.json';
import enWorkspace from '../locales/en/workspace.json';
import { NavigationProvider, type NavigationAdapter } from '../navigation';

const TEST_RESOURCES = {
  en: { common: enCommon, onboarding: enOnboarding, workspace: enWorkspace },
};

const mocks = vi.hoisted(() => ({
  getWorkspaceCapabilities: vi.fn(),
  getEffectiveConfig: vi.fn(),
  listAgents: vi.fn(),
  listMembers: vi.fn(),
  updateAgent: vi.fn(),
}));

vi.mock('@goosar/core/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/api')>()),
  api: {
    getWorkspaceCapabilities: mocks.getWorkspaceCapabilities,
    getEffectiveConfig: mocks.getEffectiveConfig,
    listAgents: mocks.listAgents,
    listMembers: mocks.listMembers,
    updateAgent: mocks.updateAgent,
  },
}));

vi.mock('@goosar/core/hooks', () => ({ useWorkspaceId: () => 'ws-1' }));

vi.mock('@goosar/core/paths', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/paths')>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace('acme') };
});

vi.mock('@goosar/core/permissions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/permissions')>()),
  useCurrentMember: () => ({
    userId: 'user-1',
    role: 'member',
    member: { perimeter_access: true },
    isLoading: false,
  }),
}));

vi.mock('@goosar/core/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/auth')>()),
  useAuthStore: Object.assign(
    (selector: (s: { user: unknown }) => unknown) => selector({ user: { id: 'user-1' } }),
    { getState: () => ({ user: { id: 'user-1' } }) },
  ),
}));

import { buildHelperMcpConfig } from '../onboarding/presets';
import { CapabilitiesPage } from './capabilities-page';

const HELPER = {
  id: 'agent_helper',
  name: 'Goosar Helper',
  system_key: 'goosar_helper',
  visibility: 'workspace',
  owner_id: 'user-1',
  archived_at: null,
  runtime_id: 'rt-1',
  mcp_config: buildHelperMcpConfig(),
  mcp_config_redacted: false,
};

const HR_SERVICES = {
  schema_version: 1,
  mcp: { 'ews-mcp': { enabled: true, has_env: false, origin: 'workspace' } },
};

const SALES_SERVICES = {
  schema_version: 1,
  mcp: {
    'ews-mcp': { enabled: true, has_env: false, origin: 'workspace' },
    'b24-agent': { enabled: true, has_env: false, origin: 'workspace' },
  },
};

const HR_ROLE: WorkspaceCapabilities = {
  template_key: 'hr',
  role_name: 'HR',
  role_summary: 'People and paperwork',
  capabilities: [
    { key: 'onboarding', title: 'Onboarding new hires', body: 'First days.' },
    { key: 'mail', title: 'Working through mail', body: 'Inbox triage.' },
  ],
  sample_tasks: [],
};

const SALES_ROLE: WorkspaceCapabilities = {
  template_key: 'sales',
  role_name: 'Sales',
  role_summary: 'Clients and deals',
  capabilities: [{ key: 'deals', title: 'Deals and pipeline', body: 'Deal history.' }],
  sample_tasks: [],
};

const NO_ROLE: WorkspaceCapabilities = {
  template_key: '',
  role_name: '',
  role_summary: '',
  capabilities: [],
  sample_tasks: [],
};

function renderPage(search = '') {
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: '/acme/capabilities',
    searchParams: new URLSearchParams(search),
    getShareableUrl: (path) => path,
  };
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={client}>
        <NavigationProvider value={navigation}>{children}</NavigationProvider>
      </QueryClientProvider>
    </I18nProvider>
  );
  render(wrapper({ children: <CapabilitiesPage /> }));
  return { navigation };
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listAgents.mockResolvedValue([HELPER]);
  mocks.listMembers.mockResolvedValue([{ user_id: 'user-1', perimeter_access: true }]);
  mocks.updateAgent.mockResolvedValue(HELPER);
  mocks.getEffectiveConfig.mockResolvedValue(HR_SERVICES);
  mocks.getWorkspaceCapabilities.mockResolvedValue(NO_ROLE);
});

describe('the page is assembled from the role, not from code', () => {
  it('renders the role the server reports', async () => {
    mocks.getWorkspaceCapabilities.mockResolvedValue(HR_ROLE);
    renderPage();

    expect(await screen.findByText('HR')).toBeInTheDocument();
    expect(screen.getByText('People and paperwork')).toBeInTheDocument();
    expect(screen.getByText('Onboarding new hires')).toBeInTheDocument();
    expect(screen.getByText('Working through mail')).toBeInTheDocument();
  });

  it('renders a different composition for a different role, with no client-side branch', async () => {
    mocks.getWorkspaceCapabilities.mockResolvedValue(SALES_ROLE);
    renderPage();

    expect(await screen.findByText('Sales')).toBeInTheDocument();
    expect(screen.getByText('Deals and pipeline')).toBeInTheDocument();
    expect(screen.queryByText('Onboarding new hires')).toBeNull();
    expect(screen.queryByText('Working through mail')).toBeNull();
  });

  it('renders a role this build has never heard of', async () => {
    mocks.getWorkspaceCapabilities.mockResolvedValue({
      template_key: 'devops',
      role_name: 'DevOps',
      role_summary: 'Keeping things running',
      capabilities: [{ key: 'deploys', title: 'Shipping releases', body: 'Rolls one out.' }],
    });
    renderPage();

    expect(await screen.findByText('DevOps')).toBeInTheDocument();
    expect(screen.getByText('Shipping releases')).toBeInTheDocument();
  });

  it('keeps naming a role whose capability list is empty', async () => {
    mocks.getWorkspaceCapabilities.mockResolvedValue({
      template_key: 'legal',
      role_name: 'Legal',
      role_summary: 'Contracts and documents',
      capabilities: [],
    });
    renderPage();

    expect(await screen.findByText('Legal')).toBeInTheDocument();
    expect(screen.getByText('Contracts and documents')).toBeInTheDocument();
    expect(screen.queryByText(/not tied to a particular role/i)).toBeNull();
    expect(
      screen.getByText(/nobody has written down yet what the assistant does/i),
    ).toBeInTheDocument();
  });

  it('renders no empty heading for a role the server can no longer name', async () => {
    const role = Promise.resolve({
      template_key: 'hr',
      role_name: '',
      role_summary: '',
      capabilities: [],
    });
    mocks.getWorkspaceCapabilities.mockReturnValue(role);
    renderPage();
    await act(async () => {
      await role;
    });

    expect(await screen.findByText('Ask in plain words')).toBeInTheDocument();
    expect(screen.queryByText(/not tied to a particular role/i)).toBeNull();
    expect(screen.queryByText(/^your role$/i)).toBeNull();
    for (const heading of screen.queryAllByRole('heading')) {
      expect(heading.textContent?.trim()).not.toBe('');
    }
  });

  it('still says something useful when the workspace has no role at all', async () => {
    renderPage();

    expect(await screen.findByText(/not tied to a particular role/i)).toBeInTheDocument();
    expect(screen.queryByText(/has been chosen/i)).toBeNull();
    expect(screen.getByText('Ask in plain words')).toBeInTheDocument();
    expect(screen.getByText('Work on documents')).toBeInTheDocument();
  });

  it('keeps the operational vocabulary off the page', async () => {
    mocks.getWorkspaceCapabilities.mockResolvedValue(HR_ROLE);
    renderPage();
    await screen.findByText('HR');

    for (const forbidden of [/perimeter/i, /runtime/i, /provisioning/i]) {
      expect(screen.queryByText(forbidden)).toBeNull();
    }
  });
});

describe("the work tools are the role's too, not a fixed catalog", () => {
  it('shows the one service an HR workspace actually has', async () => {
    mocks.getWorkspaceCapabilities.mockResolvedValue(HR_ROLE);
    renderPage();

    expect(
      await screen.findByRole('region', { name: /outlook mail & calendar/i }),
    ).toBeInTheDocument();
    expect(screen.queryByRole('region', { name: /jira & confluence/i })).toBeNull();
    expect(screen.queryByRole('region', { name: /bitrix24 & knowledge base/i })).toBeNull();
  });

  it('shows a different set for a role that carries a different set', async () => {
    mocks.getWorkspaceCapabilities.mockResolvedValue(SALES_ROLE);
    mocks.getEffectiveConfig.mockResolvedValue(SALES_SERVICES);
    renderPage();

    expect(
      await screen.findByRole('region', { name: /bitrix24 & knowledge base/i }),
    ).toBeInTheDocument();
    expect(screen.getByRole('region', { name: /outlook mail & calendar/i })).toBeInTheDocument();
  });

  it('shows no work-tools section at all for a role that carries none', async () => {
    mocks.getEffectiveConfig.mockResolvedValue({ schema_version: 1 });
    renderPage();

    await screen.findByText(/not tied to a particular role/i);
    await waitFor(() => expect(mocks.listAgents).toHaveBeenCalled());
    expect(screen.queryByRole('heading', { name: /your work tools/i })).toBeNull();
    expect(screen.queryByRole('region', { name: /outlook mail & calendar/i })).toBeNull();
    expect(screen.queryByText(/means the value was saved/i)).toBeNull();
  });
});

describe('the responsibility boundary', () => {
  it('shows a service an administrator set up as set up, and one still needing a key as needing it', async () => {
    mocks.getEffectiveConfig.mockResolvedValue({
      ...HR_SERVICES,
      llm: { has_api_key: true, origin: 'workspace', locked: false },
    });
    renderPage();

    const outlookCard = await screen.findByRole('region', {
      name: /outlook mail & calendar/i,
    });
    expect(within(outlookCard).getByText(/set up by an administrator/i)).toBeInTheDocument();
    expect(within(outlookCard).getByText(/needs your key/i)).toBeInTheDocument();

    expect(screen.getByText(/ai connection is set up/i)).toBeInTheDocument();
  });

  it('says nothing about the AI connection it cannot see, rather than blaming an administrator', async () => {
    renderPage();

    await screen.findByText(/there is nothing here for you to understand or set up/i);
    expect(screen.queryByText(/ai connection is set up/i)).toBeNull();
    expect(screen.queryByText(/administrator will connect/i)).toBeNull();
  });

  it('stays silent about an administrator half it can never observe', async () => {
    const inUse = JSON.parse(JSON.stringify(buildHelperMcpConfig()));
    inUse.mcpServers.atlassian.enabled = true;
    mocks.listAgents.mockResolvedValue([{ ...HELPER, mcp_config: inUse }]);
    mocks.getEffectiveConfig.mockResolvedValue({ schema_version: 1 });
    renderPage();

    const card = await screen.findByRole('region', {
      name: /jira & confluence/i,
    });
    expect(within(card).getByText(/needs your key/i)).toBeInTheDocument();
    expect(within(card).queryByText(/administrator/i)).toBeNull();
  });

  it('shows a filled-in key as entered, not as working', async () => {
    const filled = JSON.parse(JSON.stringify(buildHelperMcpConfig()));
    filled.mcpServers.outlook.env.EWS_EMAIL = 'me@corp.example';
    mocks.listAgents.mockResolvedValue([{ ...HELPER, mcp_config: filled }]);
    renderPage();

    const outlookCard = await screen.findByRole('region', {
      name: /outlook mail & calendar/i,
    });
    expect(within(outlookCard).getByText(/key entered/i)).toBeInTheDocument();
    expect(within(outlookCard).queryByText(/needs your key/i)).toBeNull();
    expect(screen.getByText(/means the value was saved/i)).toBeInTheDocument();
  });

  it('does not offer a key for a service that has none', async () => {
    const inUse = JSON.parse(JSON.stringify(buildHelperMcpConfig()));
    inUse.mcpServers.fetch.enabled = true;
    mocks.listAgents.mockResolvedValue([{ ...HELPER, mcp_config: inUse }]);
    renderPage();

    const fetchCard = await screen.findByRole('region', {
      name: /web access/i,
    });
    expect(within(fetchCard).getByText(/no key needed/i)).toBeInTheDocument();
  });
});

describe('the page reads, it does not collect', () => {
  it('renders no credential inputs of its own', async () => {
    mocks.getWorkspaceCapabilities.mockResolvedValue(HR_ROLE);
    renderPage();
    await screen.findByText('HR');

    await waitFor(() =>
      expect(screen.getByRole('region', { name: /outlook mail & calendar/i })).toBeInTheDocument(),
    );
    expect(screen.queryByRole('textbox')).toBeNull();
  });

  it("never writes to the member's agent, whatever state it is in", async () => {
    mocks.listAgents.mockResolvedValue([{ ...HELPER, mcp_config: null }]);
    renderPage();

    await waitFor(() => expect(mocks.listAgents).toHaveBeenCalled());
    await waitFor(() => expect(mocks.getEffectiveConfig).toHaveBeenCalled());
    expect(mocks.updateAgent).not.toHaveBeenCalled();
    expect(mocks.listMembers).not.toHaveBeenCalled();
  });

  it('points at the one place the value is entered — the MCP tab, not the workbench', async () => {
    const { navigation } = renderPage();
    const outlookCard = await screen.findByRole('region', {
      name: /outlook mail & calendar/i,
    });
    within(outlookCard)
      .getByRole('button', { name: /where to enter the key/i })
      .click();
    expect(navigation.push).toHaveBeenCalledWith('/acme/agents/agent_helper?view=mcp_config');
  });
});

describe('what the page opens on', () => {
  it('stays at the top for somebody who came to read it', async () => {
    const scrollIntoView = vi.fn();
    Element.prototype.scrollIntoView = scrollIntoView;
    mocks.getWorkspaceCapabilities.mockResolvedValue(HR_ROLE);
    renderPage();

    await screen.findByRole('region', { name: /outlook mail & calendar/i });
    await waitFor(() => expect(screen.getByText(/needs your key/i)).toBeInTheDocument());
    expect(scrollIntoView).not.toHaveBeenCalled();
  });

  it('scrolls to the tools for somebody the reminder sent here', async () => {
    const scrollIntoView = vi.fn();
    Element.prototype.scrollIntoView = scrollIntoView;
    mocks.getWorkspaceCapabilities.mockResolvedValue(HR_ROLE);
    renderPage('from=keys');

    await screen.findByRole('region', { name: /outlook mail & calendar/i });
    await waitFor(() => expect(scrollIntoView).toHaveBeenCalled());
  });
});

describe("what the page claims about somebody else's work", () => {
  it('does not promise a member their setup was done for them without evidence', async () => {
    mocks.getEffectiveConfig.mockResolvedValue({ schema_version: 1 });
    renderPage();

    await waitFor(() => expect(mocks.getEffectiveConfig).toHaveBeenCalled());
    expect(screen.queryByText(/there is nothing here for you to understand or set up/i)).toBeNull();
    expect(screen.getByText(/set up once for the whole workspace/i)).toBeInTheDocument();
  });

  it('says it plainly once a server layer proves somebody did the work', async () => {
    mocks.getEffectiveConfig.mockResolvedValue({
      ...HR_SERVICES,
      llm: { has_api_key: true, origin: 'workspace', locked: false },
    });
    renderPage();

    expect(
      await screen.findByText(/there is nothing here for you to understand or set up/i),
    ).toBeInTheDocument();
  });

  it('does not ask for a key to a service an administrator switched off', async () => {
    mocks.getEffectiveConfig.mockResolvedValue({
      schema_version: 1,
      mcp: { 'ews-mcp': { enabled: false, locked: true, origin: 'policy' } },
    });
    renderPage();

    const card = await screen.findByRole('region', {
      name: /outlook mail & calendar/i,
    });
    expect(within(card).getByText(/switched off by an administrator/i)).toBeInTheDocument();
    expect(within(card).queryByText(/needs your key/i)).toBeNull();
    expect(within(card).queryByRole('button', { name: /where to enter the key/i })).toBeNull();
  });

  it("offers no key entry for a slot it cannot say is the member's", async () => {
    mocks.getEffectiveConfig.mockResolvedValue({
      schema_version: 1,
      mcp: { 'ews-mcp': { enabled: true, has_env: true, origin: 'workspace' } },
    });
    renderPage();

    const card = await screen.findByRole('region', {
      name: /outlook mail & calendar/i,
    });
    expect(within(card).queryByText(/needs your key/i)).toBeNull();
    expect(within(card).queryByRole('button', { name: /where to enter the key/i })).toBeNull();
  });
});

describe('the instructions travel with the card', () => {
  it('carries the onboarding guide, rights and verification sections', async () => {
    renderPage();
    const outlookCard = await screen.findByRole('region', {
      name: /outlook mail & calendar/i,
    });
    within(outlookCard)
      .getByRole('button', { name: /how to get access/i })
      .click();

    await waitFor(() =>
      expect(within(outlookCard).getByText(/which permissions the key needs/i)).toBeInTheDocument(),
    );
    expect(within(outlookCard).getByText(/how to check it works/i)).toBeInTheDocument();
    expect(within(outlookCard).getByText(/work mailbox address/i)).toBeInTheDocument();
  });

  it('tells a reader with no administrator where the missing program comes from', async () => {
    const inUse = JSON.parse(JSON.stringify(buildHelperMcpConfig()));
    inUse.mcpServers.atlassian.enabled = true;
    mocks.listAgents.mockResolvedValue([{ ...HELPER, mcp_config: inUse }]);
    renderPage();

    const card = await screen.findByRole('region', {
      name: /jira & confluence/i,
    });
    within(card)
      .getByRole('button', { name: /how to get access/i })
      .click();

    await waitFor(() =>
      expect(
        within(card).getByText(/mcp-atlassian program itself does not need installing/i),
      ).toBeInTheDocument(),
    );
  });
});
