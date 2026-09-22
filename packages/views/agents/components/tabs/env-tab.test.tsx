// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { Agent } from '@goosar/core/types';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../../locales/en/common.json';
import enAgents from '../../../locales/en/agents.json';

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mocks = vi.hoisted(() => ({
  getAgentEnv: vi.fn(),
  updateAgentEnv: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
  currentUserId: 'admin-user',
}));

vi.mock('sonner', () => ({
  toast: { error: mocks.toastError, success: mocks.toastSuccess },
}));

vi.mock('@goosar/core/api', () => ({
  api: {
    getAgentEnv: mocks.getAgentEnv,
    updateAgentEnv: mocks.updateAgentEnv,
  },
}));

vi.mock('@goosar/core/auth', () => {
  const state = () => ({ user: { id: mocks.currentUserId } });
  const useAuthStore = Object.assign(
    (sel?: (s: ReturnType<typeof state>) => unknown) => (sel ? sel(state()) : state()),
    { getState: state },
  );
  return { useAuthStore };
});

import { EnvTab } from './env-tab';

const foreignAgent: Agent = {
  id: 'agent-1',
  workspace_id: 'ws-1',
  runtime_id: 'runtime-1',
  name: "Bob's Helper",
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
  owner_id: 'bob-user',
  skills: [],
  has_custom_env: true,
  custom_env_key_count: 2,
  created_at: '2026-05-28T00:00:00Z',
  updated_at: '2026-05-28T00:00:00Z',
  archived_at: null,
  archived_by: null,
} as Agent;

function renderTab(overrides: Partial<Agent> = {}) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <EnvTab agent={{ ...foreignAgent, ...overrides }} />
    </I18nProvider>,
  );
}

const MASKED_RESPONSE = {
  agent_id: 'agent-1',
  custom_env: { JIRA_PERSONAL_TOKEN: '****', OTHER_KEY: '****' },
  values_masked: true,
};

function nth<T>(list: readonly T[], index: number): T {
  const item = list[index];
  if (item === undefined) {
    throw new Error(`expected an element at index ${index}, got ${list.length}`);
  }
  return item;
}

function savedEnv(): Record<string, string> {
  const call = nth(mocks.updateAgentEnv.mock.calls, 0);
  return (call[1] as { custom_env: Record<string, string> }).custom_env;
}

async function openTab() {
  await userEvent.click(screen.getByRole('button', { name: /manage keys/i }));
  await waitFor(() => expect(mocks.getAgentEnv).toHaveBeenCalled());
}

describe('EnvTab masked (non-owner) mode', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.currentUserId = 'admin-user';
    mocks.getAgentEnv.mockResolvedValue(MASKED_RESPONSE);
    mocks.updateAgentEnv.mockResolvedValue(MASKED_RESPONSE);
  });

  it('offers to manage rather than reveal when the user is not the owner', () => {
    renderTab();
    expect(screen.getByRole('button', { name: /manage keys/i })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /reveal & edit/i })).toBeNull();
  });

  it('says the values are hidden and why', async () => {
    renderTab();
    await openTab();
    expect(screen.getByTestId('env-masked-notice')).toBeInTheDocument();
    expect(screen.getByText(/not this agent's owner/i)).toBeInTheDocument();
  });

  it('renders masked values as empty fields, never as a **** the user holds', async () => {
    renderTab();
    await openTab();
    const fields = screen.getAllByLabelText(/value hidden/i);
    expect(fields).toHaveLength(2);
    for (const field of fields) {
      expect(field).toHaveValue('');
    }
    expect(screen.queryByDisplayValue('****')).toBeNull();
  });

  it('offers no reveal toggle over an untouched masked entry', async () => {
    renderTab();
    await openTab();
    expect(screen.queryByRole('button', { name: /show value/i })).toBeNull();
  });

  it('locks the key name of a masked entry', async () => {
    renderTab();
    await openTab();
    const keyField = screen.getByDisplayValue('JIRA_PERSONAL_TOKEN');
    expect(keyField).toBeDisabled();
  });

  it('offers no add affordance and no editable value inputs (GH #273)', async () => {
    renderTab();
    await openTab();

    expect(screen.queryByRole('button', { name: /^add$/i })).toBeNull();
    for (const field of screen.getAllByLabelText(/value hidden/i)) {
      expect(field).toBeDisabled();
    }
  });

  it('removes a key and preserves the rest via the placeholder', async () => {
    renderTab();
    await openTab();

    const removeButtons = screen.getAllByRole('button', { name: /remove/i });
    await userEvent.click(nth(removeButtons, 1));
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));

    await waitFor(() => expect(mocks.updateAgentEnv).toHaveBeenCalled());
    expect(savedEnv()).toEqual({ JIRA_PERSONAL_TOKEN: '****' });
  });

  it('treats an all-**** body with no flag as masked too', async () => {
    mocks.getAgentEnv.mockResolvedValue({
      agent_id: 'agent-1',
      custom_env: { JIRA_PERSONAL_TOKEN: '****', OTHER_KEY: '****' },
    });
    renderTab();
    await openTab();
    expect(screen.getByTestId('env-masked-notice')).toBeInTheDocument();
  });
});

describe('EnvTab owner mode', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.currentUserId = 'bob-user'; 
    mocks.getAgentEnv.mockResolvedValue({
      agent_id: 'agent-1',
      custom_env: { JIRA_PERSONAL_TOKEN: 'pat-live' },
      values_masked: false,
    });
  });

  it("reveals real values to the agent's owner with no masked notice", async () => {
    renderTab();
    await userEvent.click(screen.getByRole('button', { name: /reveal & edit/i }));
    await waitFor(() => expect(mocks.getAgentEnv).toHaveBeenCalled());

    expect(screen.queryByTestId('env-masked-notice')).toBeNull();
    expect(screen.getByDisplayValue('pat-live')).toBeInTheDocument();
    expect(screen.getByDisplayValue('JIRA_PERSONAL_TOKEN')).toBeEnabled();
  });

  it('treats an explicit values_masked:false as plaintext even when a value looks like the marker', async () => {
    mocks.getAgentEnv.mockResolvedValue({
      agent_id: 'agent-1',
      custom_env: { DEBUG_MARK: '****' },
      values_masked: false,
    });

    renderTab();
    await userEvent.click(screen.getByRole('button', { name: /reveal & edit/i }));
    await waitFor(() => expect(mocks.getAgentEnv).toHaveBeenCalled());

    expect(screen.queryByTestId('env-masked-notice')).toBeNull();
    expect(screen.getByDisplayValue('DEBUG_MARK')).toBeEnabled();
    expect(screen.getByDisplayValue('****')).toBeInTheDocument();
  });

  it("lets the owner of a genuinely-stored **** keep editing that agent's env", async () => {
    mocks.getAgentEnv.mockResolvedValue({
      agent_id: 'agent-1',
      custom_env: { DEBUG_MARK: '****' },
      values_masked: false,
    });
    mocks.updateAgentEnv.mockResolvedValue({
      agent_id: 'agent-1',
      custom_env: { DEBUG_MARK: '****', ANTHROPIC_API_KEY: 'sk-new' },
      values_masked: false,
    });

    renderTab();
    await userEvent.click(screen.getByRole('button', { name: /reveal & edit/i }));
    await waitFor(() => expect(mocks.getAgentEnv).toHaveBeenCalled());

    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));
    const keyFields = screen.getAllByPlaceholderText('KEY');
    await userEvent.type(keyFields[keyFields.length - 1]!, 'ANTHROPIC_API_KEY');
    const valueFields = screen.getAllByPlaceholderText('value');
    await userEvent.type(valueFields[valueFields.length - 1]!, 'sk-new');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));

    await waitFor(() => expect(mocks.updateAgentEnv).toHaveBeenCalled());
    expect(mocks.updateAgentEnv.mock.calls[0]![1]).toEqual({
      custom_env: { DEBUG_MARK: '****', ANTHROPIC_API_KEY: 'sk-new' },
    });
  });

  it('refuses to send a hand-typed **** as if it were a value', async () => {
    renderTab();
    await userEvent.click(screen.getByRole('button', { name: /reveal & edit/i }));
    await waitFor(() => expect(mocks.getAgentEnv).toHaveBeenCalled());

    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));
    const keyFields = screen.getAllByPlaceholderText('KEY');
    await userEvent.type(nth(keyFields, keyFields.length - 1), 'SNEAKY');
    const valueFields = screen.getAllByPlaceholderText('value');
    await userEvent.type(nth(valueFields, valueFields.length - 1), '****');

    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));

    expect(mocks.updateAgentEnv).not.toHaveBeenCalled();
    expect(mocks.toastError).toHaveBeenCalled();
  });
});
