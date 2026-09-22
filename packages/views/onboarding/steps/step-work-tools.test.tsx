import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { Agent, AgentRuntime } from '@goosar/core/types';
import { useWelcomeStore } from '@goosar/core/onboarding';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enOnboarding from '../../locales/en/onboarding.json';

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const mocks = vi.hoisted(() => ({
  listAgents: vi.fn(),
  createAgent: vi.fn(),
  updateAgent: vi.fn(),
  listMembers: vi.fn(),
  toastError: vi.fn(),
}));

const currentMemberRef = vi.hoisted(() => ({
  current: {
    userId: 'user-1',
    role: 'member',
    member: null as { perimeter_access?: boolean } | null,
    isLoading: false,
  },
}));

vi.mock('@goosar/core/permissions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/permissions')>()),
  useCurrentMember: () => currentMemberRef.current,
}));

vi.mock('@goosar/core/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/api')>()),
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

vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: (...args: unknown[]) => mocks.toastError(...args),
  },
}));

import { configStore, EMPTY_DEPLOYMENT_HOSTS } from '@goosar/core/config';
import { WORK_TOOLS_CARD_ORDER } from '../presets';
import { StepWorkTools } from './step-work-tools';

function setDeliveryProfile(value: string | undefined) {
  configStore.setState({ deliveryProfile: value } as unknown as Parameters<
    typeof configStore.setState
  >[0]);
}

const RUNTIME = { id: 'rt_picked', status: 'online' } as unknown as AgentRuntime;

const HELPER = {
  id: 'agent_helper',
  name: 'Goosar Helper',
  system_key: 'goosar_helper',
  visibility: 'workspace',
  archived_at: null,
  avatar_url: '',
  runtime_id: RUNTIME.id,
  mcp_config: null,
  mcp_config_redacted: false,
} as unknown as Agent;

function renderStep(options: { installedMcpNames?: string[]; runtime?: AgentRuntime } = {}) {
  const onFinish = vi.fn();
  const onBack = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <StepWorkTools
        wsId="ws_1"
        runtime={options.runtime ?? RUNTIME}
        onFinish={onFinish}
        onBack={onBack}
        installedMcpNames={options.installedMcpNames}
      />
    </I18nProvider>,
  );
  return { onFinish, onBack };
}

function writtenDocs(): Record<string, unknown>[] {
  return mocks.updateAgent.mock.calls
    .filter(([, body]) => (body as Record<string, unknown>).mcp_config)
    .map(([, body]) => (body as { mcp_config: Record<string, unknown> }).mcp_config);
}

function saveButton() {
  return screen.getByRole('button', { name: /^save$/i });
}

function sentDoc(): {
  mcpServers: Record<string, { enabled: boolean; env?: Record<string, string>; args?: string[] }>;
} {
  const calls = mocks.updateAgent.mock.calls;
  expect(calls.length).toBeGreaterThan(0);
  const [agentId, body] = calls[calls.length - 1]!;
  expect(agentId).toBe(HELPER.id);
  return (body as { mcp_config: ReturnType<typeof sentDoc> }).mcp_config;
}

async function waitForSave(seededOnEntry = true): Promise<void> {
  await waitFor(() => expect(mocks.updateAgent).toHaveBeenCalledTimes(seededOnEntry ? 2 : 1));
}

describe('StepWorkTools', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useWelcomeStore.getState().reset();
    mocks.listAgents.mockResolvedValue([HELPER]);
    mocks.updateAgent.mockResolvedValue(HELPER);
    setDeliveryProfile(undefined);
    configStore.setState({ deploymentHosts: EMPTY_DEPLOYMENT_HOSTS });
    currentMemberRef.current = {
      userId: 'user-1',
      role: 'member',
      member: { perimeter_access: false },
      isLoading: false,
    };
  });

  it("takes the mailbox placeholder from the deployment's mail domain", () => {
    configStore.getState().setDeploymentHosts({ mailDomain: 'corp.example.test' });
    renderStep();
    expect(screen.getByLabelText(/mailbox address/i)).toHaveAttribute(
      'placeholder',
      'name@corp.example.test',
    );
  });

  it('falls back to the reserved example domain when none is configured', () => {
    renderStep();
    expect(screen.getByLabelText(/mailbox address/i)).toHaveAttribute(
      'placeholder',
      'name@example.test',
    );
  });

  it('renders all five preset cards and always offers Skip', async () => {
    renderStep();
    expect(screen.getByText('Jira & Confluence')).toBeInTheDocument();
    expect(screen.getByText('Outlook mail & calendar')).toBeInTheDocument();
    expect(screen.getByText('Bitrix24 & knowledge base')).toBeInTheDocument();
    expect(screen.getByText('Web access')).toBeInTheDocument();
    expect(screen.getByText('Extra work tools')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /skip/i })).toBeEnabled();
    await waitFor(() => expect(mocks.listAgents).toHaveBeenCalledWith({ workspace_id: 'ws_1' }));
    expect(mocks.createAgent).not.toHaveBeenCalled();
  });

  it('keeps Save disabled until at least one preset is COMPLETE', async () => {
    const user = userEvent.setup();
    renderStep();
    expect(saveButton()).toBeDisabled();
    await user.type(screen.getByLabelText(/jira personal token/i), 'jt');
    expect(saveButton()).toBeDisabled();
    await user.type(screen.getByLabelText(/confluence personal token/i), 'ct');
    expect(saveButton()).toBeEnabled();
  });

  it('saves once, enabling only the presets whose required fields are filled', async () => {
    const user = userEvent.setup();
    renderStep();

    await user.type(screen.getByLabelText(/jira personal token/i), 'jira-pat');
    await user.type(screen.getByLabelText(/confluence personal token/i), 'conf-pat');
    await user.click(saveButton());

    await waitForSave();
    const doc = sentDoc();
    expect(doc.mcpServers.atlassian!.enabled).toBe(true);
    expect(doc.mcpServers.atlassian!.env?.JIRA_PERSONAL_TOKEN).toBe('jira-pat');
    expect(doc.mcpServers.atlassian!.env?.CONFLUENCE_PERSONAL_TOKEN).toBe('conf-pat');
    expect(doc.mcpServers.outlook!.enabled).toBe(false);
    expect(doc.mcpServers.outlook!.env?.EWS_EMAIL).toBe('');
    expect(doc.mcpServers.fetch!.enabled).toBe(false);
    expect(doc.mcpServers['mcp-gateway']!.enabled).toBe(false);
    expect(doc.mcpServers.bitrix24!.enabled).toBe(false);

    expect(await screen.findByText(/^enabled$/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /continue/i })).toBeInTheDocument();
  });

  it('half-filled presets ride along untouched when another preset saves', async () => {
    const user = userEvent.setup();
    renderStep();

    await user.type(screen.getByLabelText(/jira personal token/i), 'jira-pat');
    await user.type(screen.getByLabelText(/mailbox address/i), 'name@example.test');
    await user.click(saveButton());

    await waitForSave();
    const doc = sentDoc();
    expect(doc.mcpServers.outlook!.enabled).toBe(true);
    expect(doc.mcpServers.atlassian!.enabled).toBe(false);
    expect(doc.mcpServers.atlassian!.env?.JIRA_PERSONAL_TOKEN).toBe('');
  });

  it('rejects a webhook with query markers and keeps bitrix24 unsavable', async () => {
    const user = userEvent.setup();
    renderStep();

    await user.type(screen.getByLabelText(/inbound webhook/i), '1/a?ts=1');
    await user.type(screen.getByLabelText(/knowledge-base api token/i), 'kb-tok');

    expect(screen.getByText(/must not contain spaces/i)).toBeInTheDocument();
    expect(saveButton()).toBeDisabled();
  });

  it('preserves existing config entries and replaces only presets completed this visit', async () => {
    const existingAtlassian = {
      command: 'mcp-atlassian',
      env: { JIRA_PERSONAL_TOKEN: 'old-jira-token' },
      enabled: true,
    };
    const existingCustom = { command: 'my-custom-mcp', enabled: true };
    mocks.listAgents.mockResolvedValue([
      {
        ...HELPER,
        mcp_config: {
          mcpServers: {
            atlassian: existingAtlassian,
            'my-custom': existingCustom,
          },
        },
      },
    ]);
    const user = userEvent.setup();
    renderStep();

    await user.type(screen.getByLabelText(/gateway access token/i), 'new-gw-token');
    await user.click(saveButton());

    await waitForSave(false);
    const doc = sentDoc();
    expect(doc.mcpServers.atlassian).toEqual(existingAtlassian);
    expect(doc.mcpServers['my-custom']).toEqual(existingCustom);
    expect(doc.mcpServers['mcp-gateway']!.enabled).toBe(true);
    expect(doc.mcpServers['mcp-gateway']!.env?.API_ACCESS_TOKEN).toBe('new-gw-token');
  });

  it("blocks saving entirely when the Helper's config is redacted for this viewer", async () => {
    mocks.listAgents.mockResolvedValue([
      { ...HELPER, mcp_config: undefined, mcp_config_redacted: true },
    ]);
    renderStep();

    expect(
      await screen.findByText(/managed by someone else and hidden from you/i),
    ).toBeInTheDocument();

    expect(screen.getByLabelText(/mailbox address/i)).toBeDisabled();
    expect(saveButton()).toBeDisabled();
    expect(mocks.updateAgent).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: /skip/i })).toBeEnabled();
  });

  it('writes the gateway bearer into the env slot, not args (issue #704)', async () => {
    const user = userEvent.setup();
    renderStep();

    await user.type(screen.getByLabelText(/gateway access token/i), 'gw-token-1');
    await user.click(saveButton());

    await waitForSave();
    const doc = sentDoc();
    expect(doc.mcpServers['mcp-gateway']!.enabled).toBe(true);
    expect(doc.mcpServers['mcp-gateway']!.env?.API_ACCESS_TOKEN).toBe('gw-token-1');
    expect(doc.mcpServers['mcp-gateway']!.args ?? []).not.toContain('Bearer gw-token-1');
  });

  it('completes the Bitrix24 webhook URL and enables the preset with the KB token', async () => {
    const user = userEvent.setup();
    renderStep();

    await user.type(screen.getByLabelText(/inbound webhook/i), '123/abc');
    await user.type(screen.getByLabelText(/knowledge-base api token/i), 'kb-tok');
    await user.click(saveButton());

    await waitForSave();
    const doc = sentDoc();
    expect(doc.mcpServers.bitrix24!.enabled).toBe(true);
    expect(doc.mcpServers.bitrix24!.env?.B24_WEBHOOK_URL).toBe(
      'https://b24.corp.example/rest/123/abc/',
    );
    expect(doc.mcpServers.bitrix24!.env?.KB_API_TOKEN).toBe('kb-tok');
  });

  it('enables outlook from the mailbox address alone', async () => {
    const user = userEvent.setup();
    renderStep();

    await user.type(screen.getByLabelText(/mailbox address/i), 'name@example.test');
    await user.click(saveButton());

    await waitForSave();
    const doc = sentDoc();
    expect(doc.mcpServers.outlook!.enabled).toBe(true);
    expect(doc.mcpServers.outlook!.env?.EWS_EMAIL).toBe('name@example.test');
  });

  it('enables fetch through its explicit toggle', async () => {
    const user = userEvent.setup();
    renderStep();

    await user.click(screen.getByRole('switch', { name: /enable web fetch/i }));
    await user.click(saveButton());

    await waitForSave();
    const doc = sentDoc();
    expect(doc.mcpServers.fetch!.enabled).toBe(true);
  });

  it('never creates the Helper, and fails the save when the workspace has none', async () => {
    mocks.listAgents.mockResolvedValue([]);
    const user = userEvent.setup();
    renderStep();

    await waitFor(() => expect(mocks.listAgents).toHaveBeenCalledWith({ workspace_id: 'ws_1' }));
    await user.type(screen.getByLabelText(/mailbox address/i), 'name@example.test');
    await user.click(saveButton());

    expect(await screen.findByText(/tokens could not be saved/i)).toBeInTheDocument();
    expect(mocks.createAgent).not.toHaveBeenCalled();
    expect(mocks.updateAgent).not.toHaveBeenCalled();
  });

  it('seeds the whole preset catalog on the first save of a fresh Helper', async () => {
    const user = userEvent.setup();
    renderStep();

    await user.type(screen.getByLabelText(/mailbox address/i), 'name@example.test');
    await user.click(saveButton());

    await waitForSave();
    const doc = sentDoc();
    expect(Object.keys(doc.mcpServers).sort()).toEqual([...WORK_TOOLS_CARD_ORDER].sort());
    expect(doc.mcpServers.outlook!.enabled).toBe(true);
    expect(doc.mcpServers.atlassian!.enabled).toBe(false);
  });

  it('seeds the corporate preset catalog on entry, before any save', async () => {
    renderStep();

    await waitFor(() => expect(mocks.updateAgent).toHaveBeenCalledTimes(1));
    const [agentId, body] = mocks.updateAgent.mock.calls[0]!;
    expect(agentId).toBe(HELPER.id);
    const doc = (body as { mcp_config: { mcpServers: Record<string, { enabled: boolean }> } })
      .mcp_config;
    expect(Object.keys(doc.mcpServers).sort()).toEqual([...WORK_TOOLS_CARD_ORDER].sort());
    for (const entry of Object.values(doc.mcpServers)) {
      expect(entry.enabled).toBe(false);
    }
  });

  it('never overwrites a Helper that already has an mcp_config', async () => {
    mocks.listAgents.mockResolvedValue([
      { ...HELPER, mcp_config: { mcpServers: { 'my-custom': { command: 'x' } } } },
    ]);
    renderStep();

    await waitFor(() => expect(mocks.listAgents).toHaveBeenCalledWith({ workspace_id: 'ws_1' }));
    expect(mocks.updateAgent).not.toHaveBeenCalled();
  });

  it('binds the Helper to the runtime the user picked', async () => {
    mocks.listAgents.mockResolvedValue([{ ...HELPER, runtime_id: 'rt_server_chose' }]);
    renderStep();

    await waitFor(() =>
      expect(
        mocks.updateAgent.mock.calls.some(
          ([, body]) => (body as { runtime_id?: string }).runtime_id === RUNTIME.id,
        ),
      ).toBe(true),
    );
    const rebind = mocks.updateAgent.mock.calls.find(
      ([, body]) => (body as { runtime_id?: string }).runtime_id !== undefined,
    );
    expect(rebind![0]).toBe(HELPER.id);
  });

  it('leaves the binding alone when the Helper is already on the picked runtime', async () => {
    renderStep();

    await waitFor(() => expect(mocks.updateAgent).toHaveBeenCalledTimes(1));
    expect(
      (mocks.updateAgent.mock.calls[0]![1] as { runtime_id?: string }).runtime_id,
    ).toBeUndefined();
  });

  it('Skip finishes without writing any credential', async () => {
    const user = userEvent.setup();
    const { onFinish } = renderStep();

    await user.click(screen.getByRole('button', { name: /skip/i }));

    expect(onFinish).toHaveBeenCalledTimes(1);
    for (const doc of writtenDocs()) {
      const servers = doc.mcpServers as Record<string, { enabled: boolean }>;
      for (const entry of Object.values(servers)) {
        expect(entry.enabled).toBe(false);
      }
    }
  });

  it('Continue after a save finishes the step', async () => {
    const user = userEvent.setup();
    const { onFinish } = renderStep();

    await user.type(screen.getByLabelText(/mailbox address/i), 'name@example.test');
    await user.click(saveButton());
    await user.click(await screen.findByRole('button', { name: /continue/i }));

    expect(onFinish).toHaveBeenCalledTimes(1);
  });

  it('masks token fields by default and reveals only on an explicit toggle', async () => {
    const user = userEvent.setup();
    renderStep();

    const jira = screen.getByLabelText(/jira personal token/i);
    expect(jira).toHaveAttribute('type', 'password');
    expect(screen.getByLabelText(/mailbox address/i)).toHaveAttribute('type', 'text');

    const [showJira] = screen.getAllByRole('button', {
      name: /show the token/i,
    });
    await user.click(showJira!);
    expect(jira).toHaveAttribute('type', 'text');
  });

  it('keeps secrets out of every surface except the updateAgent payload', async () => {
    const user = userEvent.setup();
    renderStep();

    await user.type(screen.getByLabelText(/jira personal token/i), 'jira-pat');
    await user.type(screen.getByLabelText(/confluence personal token/i), 'conf-pat');
    await user.click(saveButton());
    await waitForSave();

    const body = JSON.stringify(sentDoc());
    expect(body).toContain('jira-pat');

    expect(useWelcomeStore.getState().signal).toBeNull();
    expect(JSON.stringify(useWelcomeStore.getState())).not.toContain('jira-pat');
    expect(JSON.stringify(mocks.toastError.mock.calls)).not.toContain('jira-pat');
    expect(screen.queryByText(/jira-pat/)).toBeNull();
  });

  it('shows only a generic error when the save fails — never the value', async () => {
    mocks.updateAgent.mockRejectedValue(new Error('500 something exploded: jira-pat'));
    const user = userEvent.setup();
    const { onFinish } = renderStep();

    await user.type(screen.getByLabelText(/jira personal token/i), 'jira-pat');
    await user.type(screen.getByLabelText(/confluence personal token/i), 'conf-pat');
    await user.click(saveButton());

    expect(await screen.findByText(/the tokens could not be saved/i)).toBeInTheDocument();
    expect(screen.queryByText(/something exploded/)).toBeNull();
    expect(JSON.stringify(mocks.toastError.mock.calls)).not.toContain('jira-pat');
    expect(onFinish).not.toHaveBeenCalled();
    expect(saveButton()).toBeEnabled();
  });

  it('expands and collapses the per-preset guide', async () => {
    const user = userEvent.setup();
    renderStep();

    const guideText = /Personal Access Tokens/i;
    expect(screen.queryByText(guideText)).toBeNull();

    const [atlassianGuide] = screen.getAllByRole('button', {
      name: /how to get access/i,
    });
    await user.click(atlassianGuide!);
    expect(screen.getByText(guideText)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /hide the guide/i }));
    expect(screen.queryByText(guideText)).toBeNull();
  });

  it('answers where the key comes from, what it must be allowed to do, and how to check it', async () => {
    const user = userEvent.setup();
    renderStep();

    const [atlassianGuide] = screen.getAllByRole('button', {
      name: /how to get access/i,
    });
    await user.click(atlassianGuide!);

    expect(screen.getByText(/your steps/i)).toBeInTheDocument();
    expect(screen.getByText(/which permissions the key needs/i)).toBeInTheDocument();
    expect(screen.getByText(/how to check it works/i)).toBeInTheDocument();
    expect(screen.getByText(/if something is missing/i)).toBeInTheDocument();
  });

  it("keeps machine-side setup out of the member's own steps", async () => {
    const user = userEvent.setup();
    renderStep();

    const guides = screen.getAllByRole('button', {
      name: /how to get access/i,
    });
    for (const guide of guides) await user.click(guide);

    for (const heading of [
      /your steps/i,
      /which permissions the key needs/i,
      /how to check it works/i,
    ]) {
      for (const section of screen.getAllByText(heading)) {
        const body = section.nextElementSibling;
        expect(body?.textContent ?? '').not.toMatch(/pipx install/i);
        expect(body?.textContent ?? '').not.toMatch(/kinit/i);
        expect(body?.textContent ?? '').not.toMatch(/ca-bundle\.pem/i);
      }
    }
    expect(screen.queryByText(/kinit/i)).toBeNull();
    expect(screen.queryByText(/ca-bundle\.pem/i)).toBeNull();
  });

  it('mentions the Capabilities → MCP tab as the post-onboarding home of these tokens', () => {
    renderStep();
    expect(screen.getByText(/capabilities → mcp/i)).toBeInTheDocument();
  });
});

describe('StepWorkTools perimeter gate', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useWelcomeStore.getState().reset();
    mocks.listAgents.mockResolvedValue([HELPER]);
    setDeliveryProfile('perimeter');
  });

  afterEach(() => {
    setDeliveryProfile(undefined);
  });

  it('perimeter × granted: renders the catalog and seeds it onto the Helper', async () => {
    currentMemberRef.current = {
      userId: 'user-1',
      role: 'member',
      member: { perimeter_access: true },
      isLoading: false,
    };
    authUserRef.current = { id: 'user-1' };
    mocks.listMembers.mockResolvedValue([{ user_id: 'user-1', perimeter_access: true }]);
    const { onFinish } = renderStep();
    expect(screen.getByText('Jira & Confluence')).toBeInTheDocument();
    await waitFor(() => expect(mocks.listAgents).toHaveBeenCalledWith({ workspace_id: 'ws_1' }));
    await waitFor(() => expect(mocks.updateAgent).toHaveBeenCalledTimes(1));
    expect(onFinish).not.toHaveBeenCalled();
  });

  it('perimeter × granted, grant unreadable: withholds the catalog rather than leaking it', async () => {
    currentMemberRef.current = {
      userId: 'user-1',
      role: 'member',
      member: { perimeter_access: true },
      isLoading: false,
    };
    authUserRef.current = { id: 'user-1' };
    mocks.listMembers.mockRejectedValue(new Error('network'));
    renderStep();

    await waitFor(() => expect(mocks.listAgents).toHaveBeenCalledWith({ workspace_id: 'ws_1' }));
    await waitFor(() => expect(mocks.listMembers).toHaveBeenCalled());
    expect(mocks.updateAgent).not.toHaveBeenCalled();
  });

  it('perimeter × ungranted: auto-skips without rendering or touching the Helper', async () => {
    currentMemberRef.current = {
      userId: 'user-1',
      role: 'member',
      member: { perimeter_access: false },
      isLoading: false,
    };
    const { onFinish } = renderStep();
    expect(screen.queryByText('Jira & Confluence')).toBeNull();
    await waitFor(() => expect(onFinish).toHaveBeenCalled());
    expect(mocks.listAgents).not.toHaveBeenCalled();
    expect(mocks.createAgent).not.toHaveBeenCalled();
  });

  it('perimeter × field omitted by an older backend: treated as ungranted', async () => {
    currentMemberRef.current = {
      userId: 'user-1',
      role: 'member',
      member: {},
      isLoading: false,
    };
    const { onFinish } = renderStep();
    await waitFor(() => expect(onFinish).toHaveBeenCalled());
    expect(screen.queryByText('Jira & Confluence')).toBeNull();
  });

  it('perimeter × membership loading: holds blank without skipping or pre-creating', () => {
    currentMemberRef.current = {
      userId: 'user-1',
      role: 'member',
      member: null,
      isLoading: true,
    };
    const { onFinish } = renderStep();
    expect(screen.queryByText('Jira & Confluence')).toBeNull();
    expect(onFinish).not.toHaveBeenCalled();
    expect(mocks.listAgents).not.toHaveBeenCalled();
  });
});

describe('StepWorkTools — installedMcpNames filter (issue #188)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useWelcomeStore.getState().reset();
    mocks.listAgents.mockResolvedValue([HELPER]);
    mocks.updateAgent.mockResolvedValue(HELPER);
    setDeliveryProfile(undefined);
    currentMemberRef.current = {
      userId: 'user-1',
      role: 'member',
      member: { perimeter_access: false },
      isLoading: false,
    };
  });

  it("shows exactly the installed presets' credential cards", async () => {
    renderStep({ installedMcpNames: ['atlassian', 'fetch'] });
    expect(await screen.findByText('Jira & Confluence')).toBeInTheDocument();
    expect(screen.getByText('Web access')).toBeInTheDocument();
    expect(screen.queryByText('Outlook mail & calendar')).toBeNull();
    expect(screen.queryByText('Bitrix24 & knowledge base')).toBeNull();
    expect(screen.queryByText('Extra work tools')).toBeNull();
  });

  it('shows every preset when installedMcpNames is undefined (web / legacy)', async () => {
    renderStep();
    expect(await screen.findByText('Jira & Confluence')).toBeInTheDocument();
    expect(screen.getByText('Outlook mail & calendar')).toBeInTheDocument();
    expect(screen.getByText('Bitrix24 & knowledge base')).toBeInTheDocument();
    expect(screen.getByText('Web access')).toBeInTheDocument();
    expect(screen.getByText('Extra work tools')).toBeInTheDocument();
  });

  it('hides the section entirely and finishes immediately when no installed name matches a known preset', async () => {
    const { onFinish } = renderStep({ installedMcpNames: [] });
    await waitFor(() => expect(onFinish).toHaveBeenCalled());
    expect(screen.queryByText('Jira & Confluence')).toBeNull();
    expect(screen.queryByText('Web access')).toBeNull();
    expect(mocks.listAgents).not.toHaveBeenCalled();
  });

  it("ignores installed names that don't match a known preset", async () => {
    renderStep({ installedMcpNames: ['some-custom-server', 'fetch'] });
    expect(await screen.findByText('Web access')).toBeInTheDocument();
    expect(screen.queryByText('Jira & Confluence')).toBeNull();
  });
});
