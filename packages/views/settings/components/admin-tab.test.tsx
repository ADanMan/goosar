import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen, fireEvent, waitFor, within } from '@testing-library/react';
import { renderWithI18n } from '../../test/i18n';

const mocks = vi.hoisted(() => ({
  updateConfig: vi.fn(),
  setOverride: vi.fn(),
  deleteOverride: vi.fn(),
  updatePins: vi.fn(),
}));

type TestMember = {
  id: string;
  user_id: string;
  role: 'owner' | 'admin' | 'member';
  name: string;
  email: string;
};

const membersRef = vi.hoisted(() => ({ current: [] as TestMember[] }));
const viewerRef = vi.hoisted(() => ({ current: { id: 'user-1' } }));
const configRef = vi.hoisted(() => ({ current: {} as unknown }));
const overridesRef = vi.hoisted(() => ({
  current: {} as Record<string, unknown>,
}));
const pinsRef = vi.hoisted(() => ({ current: { pins: [] as unknown[] } }));
const catalogRef = vi.hoisted(() => ({
  current: { packages: [] as unknown[] } as { packages: unknown[] } | undefined,
}));

function dispatchByKey(queryKey: unknown[]): { data: unknown; isLoading: boolean } {
  const key = JSON.stringify(queryKey);
  if (key.includes('members')) return { data: membersRef.current, isLoading: false };
  if (key.includes('admin-config')) return { data: configRef.current, isLoading: false };
  if (key.includes('override:')) {
    const userId = String(queryKey[queryKey.length - 1]).replace('override:', '');
    return { data: overridesRef.current[userId] ?? null, isLoading: false };
  }
  if (key.includes('pins')) return { data: pinsRef.current, isLoading: false };
  if (key.includes('catalog')) return { data: catalogRef.current, isLoading: false };
  return { data: undefined, isLoading: false };
}

vi.mock('@tanstack/react-query', () => ({
  useQuery: (opts: { queryKey: unknown[] }) => dispatchByKey(opts.queryKey),
  useQueries: ({ queries }: { queries: { queryKey: unknown[] }[] }) =>
    queries.map((q) => dispatchByKey(q.queryKey)),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock('@goosar/core/workspace/queries', () => ({
  memberListOptions: () => ({ queryKey: ['members'], queryFn: vi.fn() }),
}));

vi.mock('@goosar/core/workspace/admin-config', () => ({
  workspaceConfigOptions: (wsId: string) => ({
    queryKey: ['admin-config', wsId],
    queryFn: vi.fn(),
  }),
  userConfigOverrideOptions: (_wsId: string, userId: string) => ({
    queryKey: ['overrides', `override:${userId}`],
    queryFn: vi.fn(),
  }),
  provisioningPinsOptions: (wsId: string) => ({
    queryKey: ['pins', wsId],
    queryFn: vi.fn(),
  }),
  provisioningCatalogOptions: (wsId: string) => ({
    queryKey: ['catalog', wsId],
    queryFn: vi.fn(),
  }),
  useUpdateWorkspaceConfig: () => ({
    mutateAsync: mocks.updateConfig,
    isPending: false,
  }),
  useSetUserConfigOverride: () => ({
    mutateAsync: mocks.setOverride,
    isPending: false,
  }),
  useDeleteUserConfigOverride: () => ({
    mutateAsync: mocks.deleteOverride,
    isPending: false,
  }),
  useUpdateProvisioningPins: () => ({
    mutateAsync: mocks.updatePins,
    isPending: false,
  }),
}));

vi.mock('@goosar/core/paths', () => ({
  useCurrentWorkspace: () => ({ id: 'ws-1', name: 'Acme', slug: 'acme' }),
}));

vi.mock('@goosar/core/auth', () => {
  const state = () => ({ user: viewerRef.current });
  const store = (selector?: (s: ReturnType<typeof state>) => unknown) =>
    selector ? selector(state()) : state();
  store.getState = state;
  return { useAuthStore: store };
});

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { AdminTab } from './admin-tab';

function member(
  partial: Partial<TestMember> & Pick<TestMember, 'id' | 'user_id' | 'role'>,
): TestMember {
  return {
    name: `Name ${partial.id}`,
    email: `${partial.id}@goosar.test`,
    ...partial,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.updateConfig.mockResolvedValue({});
  mocks.setOverride.mockResolvedValue({});
  mocks.deleteOverride.mockResolvedValue(undefined);
  mocks.updatePins.mockResolvedValue({ pins: [] });

  viewerRef.current = { id: 'user-1' };
  membersRef.current = [
    member({ id: 'm1', user_id: 'user-1', role: 'owner', name: 'Alice' }),
    member({ id: 'm2', user_id: 'user-2', role: 'member', name: 'Bob' }),
    member({ id: 'm3', user_id: 'user-3', role: 'member', name: 'Carol' }),
  ];
  configRef.current = {
    llm_base_url: 'https://gw.corp.example/v1',
    llm_model: 'openai/coding-medium',
    has_llm_api_key: true,
    mcp_defaults: {
      github: { enabled: true, env: { GITHUB_TOKEN: true } },
      jira: { enabled: false },
    },
  };
  overridesRef.current = {
    'user-2': {
      user_id: 'user-2',
      llm_model: 'openai/mini',
      has_llm_api_key: true,
    },
  };
  pinsRef.current = { pins: [] };
  catalogRef.current = {
    packages: [
      { name: 'docx-skill', version: '1.2.0', type: 'skill', platform: 'any' },
      { name: 'python', version: '3.12.1', type: 'runtime', platform: 'any' },
    ],
  };
});

describe('AdminTab role gate', () => {
  it('renders nothing for a plain member', () => {
    viewerRef.current = { id: 'user-2' }; 
    const { container } = renderWithI18n(<AdminTab />);
    expect(container.firstChild).toBeNull();
  });

  it('renders the admin surface for an owner', () => {
    renderWithI18n(<AdminTab />);
    expect(screen.getByText('Workspace LLM gateway')).toBeTruthy();
  });
});

describe('AdminTab LLM section', () => {
  it('shows base URL and model, key indicator only — never the key', () => {
    renderWithI18n(<AdminTab />);
    expect(screen.getByDisplayValue('https://gw.corp.example/v1')).toBeTruthy();
    expect(screen.getByDisplayValue('openai/coding-medium')).toBeTruthy();
    expect(screen.getByText('Key is set')).toBeTruthy();
    const keyInput = screen.getByLabelText('API key') as HTMLInputElement;
    expect(keyInput.value).toBe('');
  });

  it('sends only the fields the admin actually changed', async () => {
    renderWithI18n(<AdminTab />);
    fireEvent.change(screen.getByLabelText('Base URL'), {
      target: { value: 'https://new.example/v1' },
    });
    fireEvent.change(screen.getByLabelText('API key'), {
      target: { value: 'sk-new-key' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => expect(mocks.updateConfig).toHaveBeenCalledTimes(1));
    const patch = mocks.updateConfig.mock.calls[0]![0] as Record<string, unknown>;
    expect(patch.llm_base_url).toBe('https://new.example/v1');
    expect(patch.llm_api_key).toBe('sk-new-key');
    expect('llm_model' in patch).toBe(false);
    expect('mcp_defaults' in patch).toBe(false);
  });

  it('does not send the key when the input was left untouched', async () => {
    renderWithI18n(<AdminTab />);
    fireEvent.change(screen.getByLabelText('Model'), {
      target: { value: 'openai/coding-large' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => expect(mocks.updateConfig).toHaveBeenCalledTimes(1));
    const patch = mocks.updateConfig.mock.calls[0]![0] as Record<string, unknown>;
    expect(patch.llm_model).toBe('openai/coding-large');
    expect('llm_api_key' in patch).toBe(false);
  });

  it('does not PUT the workspace config until the confirm is accepted (§3 L2)', async () => {
    renderWithI18n(<AdminTab />);
    fireEvent.change(screen.getByLabelText('Base URL'), {
      target: { value: 'https://new.example/v1' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(screen.getByText('Apply the workspace LLM configuration?')).toBeTruthy();
    expect(mocks.updateConfig).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(mocks.updateConfig).toHaveBeenCalledTimes(1));
  });

  it('cancelling the config confirm sends nothing', () => {
    renderWithI18n(<AdminTab />);
    fireEvent.change(screen.getByLabelText('Base URL'), {
      target: { value: 'https://new.example/v1' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

    expect(mocks.updateConfig).not.toHaveBeenCalled();
    expect(screen.queryByText('Apply the workspace LLM configuration?')).toBeNull();
  });

  it('clears the stored key with an explicit empty-string write', async () => {
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Remove key' }));
    fireEvent.click(screen.getByRole('button', { name: 'Remove' }));

    await waitFor(() => expect(mocks.updateConfig).toHaveBeenCalledTimes(1));
    expect(mocks.updateConfig.mock.calls[0]![0]).toEqual({ llm_api_key: '' });
  });

  it('does not clear the stored key until the confirm is accepted (§3 L2)', async () => {
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Remove key' }));

    expect(screen.getByText('Remove the workspace API key?')).toBeTruthy();
    expect(mocks.updateConfig).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Remove' }));
    await waitFor(() => expect(mocks.updateConfig).toHaveBeenCalledTimes(1));
  });

  it('cancelling the key-removal confirm sends nothing', () => {
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Remove key' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

    expect(mocks.updateConfig).not.toHaveBeenCalled();
    expect(screen.queryByText('Remove the workspace API key?')).toBeNull();
  });
});

describe('AdminTab MCP defaults', () => {
  it('lists servers and env var names from the masked view', () => {
    renderWithI18n(<AdminTab />);
    expect(screen.getByText('github')).toBeTruthy();
    expect(screen.getByText('jira')).toBeTruthy();
    expect(screen.getByText('GITHUB_TOKEN')).toBeTruthy();
  });

  it("toggling a server sends only that entry's diff (merge-PUT)", async () => {
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('switch', { name: 'Enable jira' }));

    await waitFor(() => expect(mocks.updateConfig).toHaveBeenCalledTimes(1));
    expect(mocks.updateConfig.mock.calls[0]![0]).toEqual({
      mcp_defaults: { jira: { enabled: true } },
    });
  });

  it('announces the switch of a delivered MCP server as the removal it is', () => {
    renderWithI18n(<AdminTab />);
    expect(screen.getByRole('switch', { name: 'Stop delivering github' })).toBeTruthy();
    expect(screen.queryByRole('switch', { name: 'Enable github' })).toBeNull();
    expect(screen.getByRole('switch', { name: 'Enable jira' })).toBeTruthy();
  });

  it('labels the env value field as a field, not as the button beside it', () => {
    renderWithI18n(<AdminTab />);
    expect(screen.getByLabelText('Value of the new variable for github')).toBeTruthy();
    expect(screen.queryByLabelText('Add variable to github')).toBeNull();
  });

  it('disabling a server does not PUT until the confirm is accepted (§3 L2)', async () => {
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('switch', { name: 'Stop delivering github' }));

    expect(mocks.updateConfig).not.toHaveBeenCalled();
    expect(screen.getByText('Stop delivering github?')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Update MCP defaults' }));
    await waitFor(() => expect(mocks.updateConfig).toHaveBeenCalledTimes(1));
    expect(mocks.updateConfig.mock.calls[0]![0]).toEqual({
      mcp_defaults: { github: { enabled: false } },
    });
  });

  it('cancelling an MCP confirm sends nothing', () => {
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Remove github' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

    expect(mocks.updateConfig).not.toHaveBeenCalled();
    expect(screen.queryByText('Remove github from the MCP defaults?')).toBeNull();
  });

  it('tells the four MCP writes apart in the confirm (§3 L2)', () => {
    const { unmount } = renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Remove GITHUB_TOKEN from github' }));
    expect(screen.getByText('Remove GITHUB_TOKEN from github?')).toBeTruthy();
    unmount();

    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Remove github' }));
    expect(screen.getByText('Remove github from the MCP defaults?')).toBeTruthy();
    expect(screen.queryByText('Remove GITHUB_TOKEN from github?')).toBeNull();
  });

  it('names the variable and the server when storing a value', async () => {
    renderWithI18n(<AdminTab />);
    fireEvent.change(screen.getAllByLabelText('NAME')[0]!, {
      target: { value: 'JIRA_TOKEN' },
    });
    fireEvent.click(screen.getAllByRole('button', { name: 'Add variable' })[0]!);

    expect(mocks.updateConfig).not.toHaveBeenCalled();
    expect(screen.getByText('Store JIRA_TOKEN for github?')).toBeTruthy();
  });

  it('removing an env var sends an explicit null for just that key', async () => {
    renderWithI18n(<AdminTab />);
    fireEvent.click(
      screen.getByRole('button', {
        name: 'Remove GITHUB_TOKEN from github',
      }),
    );
    expect(mocks.updateConfig).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Update MCP defaults' }));

    await waitFor(() => expect(mocks.updateConfig).toHaveBeenCalledTimes(1));
    expect(mocks.updateConfig.mock.calls[0]![0]).toEqual({
      mcp_defaults: { github: { env: { GITHUB_TOKEN: null } } },
    });
  });

  it('removing a server sends an explicit null entry', async () => {
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Remove github' }));
    expect(mocks.updateConfig).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Update MCP defaults' }));

    await waitFor(() => expect(mocks.updateConfig).toHaveBeenCalledTimes(1));
    expect(mocks.updateConfig.mock.calls[0]![0]).toEqual({
      mcp_defaults: { github: null },
    });
  });
});

describe('AdminTab member overrides', () => {
  it('shows who has what overridden', () => {
    renderWithI18n(<AdminTab />);
    const bobRow = screen.getByText('Bob').closest('[data-member-row]')!;
    expect(within(bobRow as HTMLElement).getByText('Model')).toBeTruthy();
    expect(within(bobRow as HTMLElement).getByText('API key')).toBeTruthy();
    const carolRow = screen.getByText('Carol').closest('[data-member-row]')!;
    expect(within(carolRow as HTMLElement).getByText('No override')).toBeTruthy();
  });

  it('removes an override', async () => {
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Remove override for Bob' }));
    expect(mocks.deleteOverride).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Remove override' }));
    await waitFor(() => expect(mocks.deleteOverride).toHaveBeenCalledWith('user-2'));
  });

  it('does not remove an override until a confirm naming the member is accepted (§3 L2)', () => {
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Remove override for Bob' }));

    expect(screen.getByText('Remove the override for Bob?')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

    expect(mocks.deleteOverride).not.toHaveBeenCalled();
    expect(screen.queryByText('Remove the override for Bob?')).toBeNull();
  });

  it('sets an override through the dialog, sending only entered fields', async () => {
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Configure override for Carol' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: 'https://carol.example/v1' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save override' }));
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => expect(mocks.setOverride).toHaveBeenCalledTimes(1));
    const call = mocks.setOverride.mock.calls[0]![0] as {
      userId: string;
      patch: Record<string, unknown>;
    };
    expect(call.userId).toBe('user-3');
    expect(call.patch.llm_base_url).toBe('https://carol.example/v1');
    expect('llm_model' in call.patch).toBe(false);
    expect('llm_api_key' in call.patch).toBe(false);
  });

  it('asks for a confirm naming the member before applying (§3 L2)', async () => {
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Configure override for Carol' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: 'https://carol.example/v1' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save override' }));

    expect(screen.getByText('Apply the override for Carol?')).toBeTruthy();
    expect(mocks.setOverride).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(mocks.setOverride).toHaveBeenCalledTimes(1));
  });

  it('backing out of the override confirm applies nothing', async () => {
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Configure override for Carol' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: 'https://carol.example/v1' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save override' }));
    fireEvent.click(screen.getByRole('button', { name: 'Back' }));

    expect(mocks.setOverride).not.toHaveBeenCalled();
    expect(within(dialog).getByRole('button', { name: 'Save override' })).toBeTruthy();
    expect(screen.queryByText('Apply the override for Carol?')).toBeNull();
  });

  it('names a member with no display name by their email, everywhere', () => {
    membersRef.current = [
      member({ id: 'm1', user_id: 'user-1', role: 'owner', name: 'Alice' }),
      member({
        id: 'm2',
        user_id: 'user-2',
        role: 'member',
        name: '  ',
        email: 'bob@goosar.test',
      }),
    ];
    renderWithI18n(<AdminTab />);

    expect(
      screen.getByRole('button', {
        name: 'Configure override for bob@goosar.test',
      }),
    ).toBeTruthy();
    fireEvent.click(
      screen.getByRole('button', {
        name: 'Remove override for bob@goosar.test',
      }),
    );
    expect(screen.getByText('Remove the override for bob@goosar.test?')).toBeTruthy();
    expect(
      screen.getByText(
        "bob@goosar.test's machines go back to the workspace layer on their next sync. A key stored only in this override is deleted and cannot be read back.",
      ),
    ).toBeTruthy();
  });

  it('names the same fallback in the override form and its confirm', async () => {
    membersRef.current = [
      member({ id: 'm1', user_id: 'user-1', role: 'owner', name: 'Alice' }),
      member({
        id: 'm2',
        user_id: 'user-2',
        role: 'member',
        name: '',
        email: 'bob@goosar.test',
      }),
    ];
    renderWithI18n(<AdminTab />);
    fireEvent.click(
      screen.getByRole('button', {
        name: 'Configure override for bob@goosar.test',
      }),
    );
    const dialog = await screen.findByRole('dialog');
    expect(screen.getByText('Override for bob@goosar.test')).toBeTruthy();

    fireEvent.change(within(dialog).getByLabelText('Model'), {
      target: { value: 'openai/coding-small' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save override' }));
    expect(screen.getByText('Apply the override for bob@goosar.test?')).toBeTruthy();
  });
});

function docxPin(): Record<string, unknown> {
  return {
    package_name: 'docx-skill',
    package_type: 'skill',
    version: '1.2.0',
    enabled: true,
  };
}

function pythonPin(): Record<string, unknown> {
  return {
    package_name: 'python',
    package_type: 'runtime',
    version: '3.12.1',
    enabled: true,
  };
}

function pinDialog() {
  return within(screen.getByRole('dialog'));
}

describe('AdminTab provisioning pins', () => {
  it('states honestly that no pins means the whole catalog (#187)', () => {
    renderWithI18n(<AdminTab />);
    expect(
      screen.getByText('No pins: the machines receive the whole deployment catalog.'),
    ).toBeTruthy();
    expect(screen.getByText('docx-skill')).toBeTruthy();
    expect(screen.getByText('python')).toBeTruthy();
  });

  it('pinning the FIRST package is a mass revocation and needs the workspace name (§3 L3)', async () => {
    renderWithI18n(<AdminTab />);
    expect(screen.queryByRole('switch', { name: 'Pin docx-skill' })).toBeNull();
    fireEvent.click(screen.getByRole('switch', { name: 'Narrow delivery to docx-skill' }));

    expect(mocks.updatePins).not.toHaveBeenCalled();
    expect(screen.getByText('Narrow delivery to docx-skill?')).toBeTruthy();
    expect(pinDialog().getByText(/every other package already delivered is removed/)).toBeTruthy();

    const confirmButton = pinDialog().getByRole('button', {
      name: 'Narrow delivery',
    });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.updatePins).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'docx-skill' },
    });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.updatePins).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Acme' },
    });
    fireEvent.click(confirmButton);
    await waitFor(() => expect(mocks.updatePins).toHaveBeenCalledTimes(1));
    expect(mocks.updatePins.mock.calls[0]![0]).toEqual([
      {
        package_name: 'docx-skill',
        package_type: 'skill',
        version: '1.2.0',
        enabled: true,
      },
    ]);
  });

  it('pinning a package next to existing pins stays one click', async () => {
    pinsRef.current = { pins: [docxPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('switch', { name: 'Pin python' }));

    expect(screen.queryByRole('dialog')).toBeNull();
    await waitFor(() => expect(mocks.updatePins).toHaveBeenCalledTimes(1));
    expect(mocks.updatePins.mock.calls[0]![0]).toEqual([
      {
        package_name: 'docx-skill',
        package_type: 'skill',
        version: '1.2.0',
        enabled: true,
      },
      {
        package_name: 'python',
        package_type: 'runtime',
        version: '3.12.1',
        enabled: true,
      },
    ]);
  });

  it('re-enabling a disabled pin needs no confirmation', async () => {
    pinsRef.current = { pins: [{ ...docxPin(), enabled: false }] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('switch', { name: 'Pin docx-skill' }));

    await waitFor(() => expect(mocks.updatePins).toHaveBeenCalledTimes(1));
    expect(mocks.updatePins.mock.calls[0]![0]).toEqual([
      {
        package_name: 'docx-skill',
        package_type: 'skill',
        version: '1.2.0',
        enabled: true,
      },
    ]);
  });

  it('cannot re-enable a pin whose package left the catalog', () => {
    catalogRef.current = { packages: [] };
    pinsRef.current = {
      pins: [{ ...docxPin(), enabled: false }],
    };
    renderWithI18n(<AdminTab />);

    const toggle = screen.getByRole('switch', { name: 'Pin docx-skill' });
    expect(toggle).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(toggle);
    expect(mocks.updatePins).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(
      screen.getByText(
        'This package is no longer in the deployment catalog. Turning the pin on would break provisioning for the whole workspace, so it stays off until the package is published again.',
      ),
    ).toBeTruthy();
  });

  it('still lets a stale pin be turned OFF — that is the way out of the outage', () => {
    catalogRef.current = { packages: [] };
    pinsRef.current = { pins: [docxPin(), { ...pythonPin(), enabled: false }] };
    renderWithI18n(<AdminTab />);

    const toggle = screen.getByRole('switch', {
      name: 'Stop delivering docx-skill',
    });
    expect(toggle).not.toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(toggle);
    expect(screen.getByRole('dialog')).toBeTruthy();
  });

  it('announces the switch of a delivered package as the revocation it is', () => {
    pinsRef.current = { pins: [docxPin()] };
    renderWithI18n(<AdminTab />);
    expect(screen.getByRole('switch', { name: 'Stop delivering docx-skill' })).toBeTruthy();
    expect(screen.queryByRole('switch', { name: 'Pin docx-skill' })).toBeNull();
  });

  it('disabling a pinned package is a revocation and needs the typed name (§3 L3)', () => {
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('switch', { name: 'Stop delivering docx-skill' }));

    expect(mocks.updatePins).not.toHaveBeenCalled();
    expect(screen.getByText('Stop delivering docx-skill?')).toBeTruthy();

    const confirmButton = screen.getByRole('button', { name: 'Disable pin' });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.updatePins).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'docx' },
    });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.updatePins).not.toHaveBeenCalled();
  });

  it('disabling a pinned package keeps the pin instead of dropping it', async () => {
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    expect(
      screen.queryByText('No pins: the machines receive the whole deployment catalog.'),
    ).toBeNull();

    fireEvent.click(screen.getByRole('switch', { name: 'Stop delivering docx-skill' }));
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'docx-skill' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Disable pin' }));
    await waitFor(() => expect(mocks.updatePins).toHaveBeenCalledTimes(1));
    expect(mocks.updatePins.mock.calls[0]![0]).toEqual([
      {
        package_name: 'docx-skill',
        package_type: 'skill',
        version: '1.2.0',
        enabled: false,
      },
      {
        package_name: 'python',
        package_type: 'runtime',
        version: '3.12.1',
        enabled: true,
      },
    ]);
  });

  it('disabling the LAST enabled pin says the machines will get nothing (§3 L3)', async () => {
    pinsRef.current = { pins: [docxPin(), { ...pythonPin(), enabled: false }] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('switch', { name: 'Stop delivering docx-skill' }));

    expect(screen.getByText('Stop provisioning this workspace?')).toBeTruthy();
    expect(pinDialog().getByText(/its machines provision nothing/)).toBeTruthy();

    const confirmButton = pinDialog().getByRole('button', {
      name: 'Stop provisioning',
    });
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'docx-skill' },
    });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.updatePins).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Acme' },
    });
    fireEvent.click(confirmButton);
    await waitFor(() => expect(mocks.updatePins).toHaveBeenCalledTimes(1));
    expect(mocks.updatePins.mock.calls[0]![0]).toEqual([
      {
        package_name: 'docx-skill',
        package_type: 'skill',
        version: '1.2.0',
        enabled: false,
      },
      {
        package_name: 'python',
        package_type: 'runtime',
        version: '3.12.1',
        enabled: false,
      },
    ]);
  });

  it('unpinning drops the pin from the lockfile', async () => {
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin docx-skill' }));
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'docx-skill' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Revoke package' }));
    await waitFor(() => expect(mocks.updatePins).toHaveBeenCalledTimes(1));
    expect(mocks.updatePins.mock.calls[0]![0]).toEqual([
      {
        package_name: 'python',
        package_type: 'runtime',
        version: '3.12.1',
        enabled: true,
      },
    ]);
  });

  it('unpinning an already disabled pin does not promise a revocation', () => {
    pinsRef.current = { pins: [{ ...docxPin(), enabled: false }, pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin docx-skill' }));

    expect(pinDialog().getByText(/Removing the pin changes nothing there/)).toBeTruthy();
    expect(pinDialog().queryByText(/are removed on their next sync/)).toBeNull();
  });

  it('unpinning the LAST pin widens delivery instead of revoking (§3 L3)', async () => {
    pinsRef.current = { pins: [docxPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin docx-skill' }));

    expect(screen.getByText('Remove the last pin, docx-skill?')).toBeTruthy();
    expect(pinDialog().getByText(/removing it does not revoke docx-skill/)).toBeTruthy();

    const confirmButton = pinDialog().getByRole('button', {
      name: 'Remove the last pin',
    });
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'docx-skill' },
    });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.updatePins).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Acme' },
    });
    fireEvent.click(confirmButton);
    await waitFor(() => expect(mocks.updatePins).toHaveBeenCalledTimes(1));
    expect(mocks.updatePins.mock.calls[0]![0]).toEqual([]);
  });

  it('unpins by (type, name) even when a refetch replaces the pin objects mid-dialog', async () => {
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    const { rerender } = renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin docx-skill' }));

    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    rerender(<AdminTab />);

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'docx-skill' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Revoke package' }));

    await waitFor(() => expect(mocks.updatePins).toHaveBeenCalledTimes(1));
    expect(mocks.updatePins.mock.calls[0]![0]).toEqual([
      {
        package_name: 'python',
        package_type: 'runtime',
        version: '3.12.1',
        enabled: true,
      },
    ]);
  });

  it('cannot revoke a pin whose package name is empty — fail closed', () => {
    catalogRef.current = { packages: [] };
    pinsRef.current = {
      pins: [
        {
          package_name: '',
          package_type: 'skill',
          version: '1.2.0',
          enabled: true,
        },
        pythonPin(),
      ],
    };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin' }));

    const confirmButton = screen.getByRole('button', {
      name: 'Update pins',
    });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.updatePins).not.toHaveBeenCalled();

    expect(screen.queryByLabelText(/Type/)).toBeNull();
    expect(
      screen.getByText(
        'This pin has no package name, so there is nothing to type and it cannot be revoked here. Removing all pins would not revoke it either — that only widens delivery back to the whole catalog.',
      ),
    ).toBeTruthy();
  });

  it('refuses to remove all pins until the workspace name is typed (§3 L3)', () => {
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Remove all pins' }));

    expect(screen.getByText('Remove all pins?')).toBeTruthy();
    expect(screen.getByText(/now: 2/)).toBeTruthy();
    expect(mocks.updatePins).not.toHaveBeenCalled();

    const confirmButton = pinDialog().getByRole('button', {
      name: 'Remove all pins',
    });

    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.updatePins).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: '2' },
    });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.updatePins).not.toHaveBeenCalled();
  });

  it('says removing all pins widens delivery instead of revoking', () => {
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Remove all pins' }));

    const dialog = pinDialog();
    expect(dialog.getByText(/This is not a revocation/)).toBeTruthy();
    expect(dialog.getByText(/provisions the whole deployment catalog again/)).toBeTruthy();
    expect(dialog.queryByText(/revoked from members' machines/)).toBeNull();
  });

  it('removes every pin once the workspace name is typed (§3 L3)', async () => {
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Remove all pins' }));
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Acme' },
    });
    fireEvent.click(pinDialog().getByRole('button', { name: 'Remove all pins' }));

    await waitFor(() => expect(mocks.updatePins).toHaveBeenCalledTimes(1));
    expect(mocks.updatePins.mock.calls[0]![0]).toEqual([]);
  });

  it('refuses to unpin without the exact package name typed (§3 L3)', () => {
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin docx-skill' }));

    expect(screen.getByText('Revoke docx-skill?')).toBeTruthy();
    const confirmButton = screen.getByRole('button', {
      name: 'Revoke package',
    });

    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.updatePins).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'docx' },
    });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.updatePins).not.toHaveBeenCalled();
  });

  it('submitting the typed field with Enter obeys the same gate', async () => {
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin docx-skill' }));
    const input = screen.getByLabelText(/Type/);

    fireEvent.keyDown(input, { key: 'Enter' });
    expect(mocks.updatePins).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: 'docx' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(mocks.updatePins).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: 'docx-skill' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(mocks.updatePins).toHaveBeenCalledTimes(1));
  });

  it('never prints the typed target into the input placeholder', () => {
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin docx-skill' }));
    const input = screen.getByLabelText(/Type/) as HTMLInputElement;
    expect(input.placeholder).not.toContain('docx-skill');
  });

  it('keeps the dialog and the typed target after a failed write', async () => {
    mocks.updatePins.mockRejectedValueOnce(new Error('boom'));
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin docx-skill' }));
    const input = screen.getByLabelText(/Type/) as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'docx-skill' } });
    fireEvent.click(screen.getByRole('button', { name: 'Revoke package' }));

    await waitFor(() => expect(mocks.updatePins).toHaveBeenCalledTimes(1));
    expect(screen.getByText('Revoke docx-skill?')).toBeTruthy();
    expect((screen.getByLabelText(/Type/) as HTMLInputElement).value).toBe('docx-skill');
  });

  it('closes the dialog once the write lands', async () => {
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin docx-skill' }));
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'docx-skill' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Revoke package' }));
    await waitFor(() => expect(screen.queryByText('Revoke docx-skill?')).toBeNull());
  });

  it('wraps a long target instead of pushing the label out of the dialog', () => {
    const long = 'a'.repeat(200);
    catalogRef.current = { packages: [] };
    pinsRef.current = {
      pins: [
        {
          package_name: long,
          package_type: 'skill',
          version: '1.0.0',
          enabled: true,
        },
        pythonPin(),
      ],
    };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: `Unpin ${long}` }));
    const code = screen.getByText(long, { selector: 'code' });
    expect(code.className).toContain('break-all');
    expect(code.closest('label')!.className).toContain('flex-wrap');

    const dialog = screen.getByRole('dialog');
    expect(dialog.querySelector('[data-slot=dialog-title]')!.className).toContain('break-words');
    expect(dialog.querySelector('[data-slot=dialog-description]')!.className).toContain(
      'break-words',
    );
  });
});

describe('AdminTab provisioning pins: the outcome is what is shown', () => {
  it('cannot re-enable a pin whose VERSION left the catalog', () => {
    catalogRef.current = {
      packages: [{ name: 'docx-skill', version: '2.0.0', type: 'skill', platform: '*' }],
    };
    pinsRef.current = { pins: [{ ...docxPin(), enabled: false }] };
    renderWithI18n(<AdminTab />);

    const toggle = screen.getByRole('switch', { name: 'Pin docx-skill' });
    expect(toggle).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(toggle);
    expect(mocks.updatePins).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).toBeNull();

    expect(screen.getByText('Version not in catalog')).toBeTruthy();
    expect(
      screen.getByText(
        /no longer publishes version 1\.2\.0 of docx-skill.*break delivery for the whole workspace/,
      ),
    ).toBeTruthy();
  });

  it('counts the last pin by the ENABLED ones when unpinning, not by all of them', () => {
    pinsRef.current = { pins: [docxPin(), { ...pythonPin(), enabled: false }] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin docx-skill' }));

    expect(screen.getByText('Stop provisioning this workspace?')).toBeTruthy();
    expect(pinDialog().getByText(/the workspace delivers nothing at all/)).toBeTruthy();
    expect(pinDialog().queryByText(/other pins are untouched/)).toBeNull();

    const confirmButton = pinDialog().getByRole('button', {
      name: 'Stop provisioning',
    });
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'docx-skill' },
    });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.updatePins).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Acme' },
    });
    fireEvent.click(confirmButton);
    return waitFor(() => expect(mocks.updatePins).toHaveBeenCalledTimes(1));
  });

  it('does not promise a revocation for a package another enabled pin requires', () => {
    catalogRef.current = {
      packages: [
        {
          name: 'docx-skill',
          version: '1.2.0',
          type: 'skill',
          platform: '*',
          requires: ['runtime:python@3.12.1'],
        },
        { name: 'python', version: '3.12.1', type: 'runtime', platform: '*' },
      ],
    };
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin python' }));

    expect(screen.getByText('Remove the pin for python?')).toBeTruthy();
    expect(pinDialog().getByText(/python keeps being delivered/)).toBeTruthy();
    expect(pinDialog().queryByText(/removed on their next sync/)).toBeNull();
  });

  it('names the packages that leave together with the target', () => {
    catalogRef.current = {
      packages: [
        {
          name: 'docx-skill',
          version: '1.2.0',
          type: 'skill',
          platform: '*',
          requires: ['runtime:python@3.12.1'],
        },
        { name: 'python', version: '3.12.1', type: 'runtime', platform: '*' },
        { name: 'research', version: '1.0.0', type: 'skill', platform: '*' },
      ],
    };
    pinsRef.current = {
      pins: [
        docxPin(),
        {
          package_type: 'skill',
          package_name: 'research',
          version: '1.0.0',
          enabled: true,
        },
      ],
    };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin docx-skill' }));

    expect(pinDialog().getByText(/python@3\.12\.1/)).toBeTruthy();
  });

  it('does not promise a revocation on the disable path either', () => {
    catalogRef.current = {
      packages: [
        {
          name: 'docx-skill',
          version: '1.2.0',
          type: 'skill',
          platform: '*',
          requires: ['runtime:python@3.12.1'],
        },
        { name: 'python', version: '3.12.1', type: 'runtime', platform: '*' },
      ],
    };
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('switch', { name: 'Stop delivering python' }));

    expect(pinDialog().getByText(/python keeps being delivered/)).toBeTruthy();
    expect(pinDialog().queryByText(/removed on their next sync/)).toBeNull();
  });

  it('does not call the first pin the only package the machines receive', () => {
    catalogRef.current = {
      packages: [
        {
          name: 'docx-skill',
          version: '1.2.0',
          type: 'skill',
          platform: '*',
          requires: ['runtime:python@3.12.1'],
        },
        { name: 'python', version: '3.12.1', type: 'runtime', platform: '*' },
      ],
    };
    pinsRef.current = { pins: [] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('switch', { name: 'Pin docx-skill' }));

    expect(pinDialog().getByText(/changes nothing for them/)).toBeTruthy();
    expect(pinDialog().queryByText(/the only package/)).toBeNull();
    expect(pinDialog().queryByText(/every other package already delivered is removed/)).toBeNull();
  });

  it('says delivery is down right now when an enabled pin left the catalog', () => {
    catalogRef.current = {
      packages: [{ name: 'python', version: '3.12.1', type: 'runtime', platform: '*' }],
    };
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);

    expect(
      screen.getByText(/Delivery is down for this workspace: docx-skill is pinned and turned on/),
    ).toBeTruthy();
    expect(screen.getByText(/Turning that pin off — or removing it/)).toBeTruthy();
  });

  it('calls turning the stale pin off the repair it is, not a revocation', () => {
    catalogRef.current = {
      packages: [{ name: 'python', version: '3.12.1', type: 'runtime', platform: '*' }],
    };
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('switch', { name: 'Stop delivering docx-skill' }));

    expect(screen.getByText('Restore delivery to this workspace?')).toBeTruthy();
    expect(pinDialog().getByRole('button', { name: 'Restore delivery' })).toBeTruthy();
    expect(pinDialog().getByText(/lets the other enabled pins be delivered again/)).toBeTruthy();
    expect(pinDialog().queryByText(/The other enabled pins keep being delivered/)).toBeNull();
  });

  it('does not claim that removing the last stale pin leaves it alone', () => {
    catalogRef.current = {
      packages: [{ name: 'python', version: '3.12.1', type: 'runtime', platform: '*' }],
    };
    pinsRef.current = {
      pins: [
        {
          package_name: 'legacy',
          package_type: 'skill',
          version: '1.0.0',
          enabled: true,
        },
      ],
    };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin legacy' }));

    expect(pinDialog().queryByText(/does not revoke legacy/)).toBeNull();
    expect(pinDialog().getByText(/legacy itself stops being delivered/)).toBeTruthy();
    expect(pinDialog().getByText(/delivers the whole deployment catalog again/)).toBeTruthy();
  });
});

describe('AdminTab provisioning pins: an unreadable catalog is its own state', () => {
  it('does not mark anything missing while the catalog cannot be read', () => {
    catalogRef.current = undefined;
    pinsRef.current = { pins: [{ ...docxPin(), enabled: false }] };
    renderWithI18n(<AdminTab />);

    expect(screen.queryByText('Not in catalog')).toBeNull();
    expect(screen.queryByText('Version not in catalog')).toBeNull();
    expect(screen.getByText(/The deployment catalog cannot be read right now/)).toBeTruthy();

    const toggle = screen.getByRole('switch', { name: 'Pin docx-skill' });
    expect(toggle).not.toHaveAttribute('aria-disabled', 'true');
  });

  it('refuses to predict what machines receive while the catalog is unknown', () => {
    catalogRef.current = undefined;
    pinsRef.current = { pins: [docxPin(), pythonPin()] };
    renderWithI18n(<AdminTab />);
    fireEvent.click(screen.getByRole('button', { name: 'Unpin docx-skill' }));

    expect(pinDialog().getByText(/The pin for docx-skill is removed\./)).toBeTruthy();
    expect(
      pinDialog().getByText(/what members' machines start or stop receiving cannot be shown here/),
    ).toBeTruthy();
    expect(pinDialog().queryByText(/removed on their next sync/)).toBeNull();
  });
});
