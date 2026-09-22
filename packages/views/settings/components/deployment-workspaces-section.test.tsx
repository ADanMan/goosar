import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen, fireEvent, waitFor, within } from '@testing-library/react';
import { ApiError } from '@goosar/core/api';
import { renderWithI18n } from '../../test/i18n';

const mocks = vi.hoisted(() => ({
  updateConfig: vi.fn(),
  setOverride: vi.fn(),
  deleteOverride: vi.fn(),
  deactivateUser: vi.fn(),
  reactivateUser: vi.fn(),
  revokeSessions: vi.fn(async () => ({ revoked: 2, token_version: 3 })),
  offboardPending: false,
}));

type QueryKind = 'workspaces' | 'members' | 'config' | 'overrides' | 'memberOverride';

const state = vi.hoisted(() => ({
  workspaces: undefined as unknown,
  members: undefined as unknown,
  config: undefined as unknown,
  overrides: undefined as unknown,
  memberOverrides: {} as Record<string, unknown>,
  errors: {} as Record<string, unknown>,
  queryKeys: [] as string[],
}));

function queryResult(kind: QueryKind, data: unknown) {
  const error = state.errors[kind];
  if (error !== undefined) {
    return {
      data: undefined,
      error,
      isError: true,
      isPending: false,
      isSuccess: false,
      refetch: vi.fn(),
    };
  }
  return {
    data,
    error: null,
    isError: false,
    isPending: data === undefined,
    isSuccess: data !== undefined,
    refetch: vi.fn(),
  };
}

function dispatchByKey(queryKey: unknown[]) {
  state.queryKeys.push(queryKey.map(String).join('/'));
  const last = String(queryKey[queryKey.length - 1]);
  if (queryKey.length === 2) return queryResult('workspaces', state.workspaces);
  if (last === 'members') return queryResult('members', state.members);
  if (last === 'config') return queryResult('config', state.config);
  if (last === 'overrides') return queryResult('overrides', state.overrides);
  const settled = Object.prototype.hasOwnProperty.call(state.memberOverrides, last);
  return queryResult('memberOverride', settled ? state.memberOverrides[last] : undefined);
}

vi.mock('@tanstack/react-query', () => ({
  useQuery: (opts: { queryKey: unknown[] }) => dispatchByKey(opts.queryKey),
  useQueryClient: () => ({ invalidateQueries: vi.fn(), setQueryData: vi.fn() }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock('@goosar/core/deployment/admin', () => ({
  deploymentWorkspacesOptions: () => ({
    queryKey: ['deployment', 'workspaces'],
    queryFn: vi.fn(),
  }),
  deploymentWorkspaceMembersOptions: (wsId: string) => ({
    queryKey: ['deployment', 'workspaces', wsId, 'members'],
    queryFn: vi.fn(),
  }),
  deploymentWorkspaceConfigOptions: (wsId: string) => ({
    queryKey: ['deployment', 'workspaces', wsId, 'config'],
    queryFn: vi.fn(),
  }),
  deploymentWorkspaceOverridesOptions: (wsId: string) => ({
    queryKey: ['deployment', 'workspaces', wsId, 'overrides'],
    queryFn: vi.fn(),
  }),
  deploymentUserConfigOverrideOptions: (wsId: string, userId: string) => ({
    queryKey: ['deployment', 'workspaces', wsId, 'overrides', userId],
    queryFn: vi.fn(),
  }),
  useUpdateDeploymentWorkspaceConfig: () => ({
    mutateAsync: mocks.updateConfig,
    isPending: false,
  }),
  useSetDeploymentUserConfigOverride: () => ({
    mutateAsync: mocks.setOverride,
    isPending: false,
  }),
  useDeleteDeploymentUserConfigOverride: () => ({
    mutateAsync: mocks.deleteOverride,
    isPending: false,
  }),
  useDeactivateDeploymentUser: () => ({
    mutateAsync: mocks.deactivateUser,
    isPending: mocks.offboardPending,
  }),
  useReactivateDeploymentUser: () => ({
    mutateAsync: mocks.reactivateUser,
    isPending: mocks.offboardPending,
  }),
  useRevokeDeploymentUserSessions: () => ({
    mutateAsync: mocks.revokeSessions,
    isPending: false,
  }),
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { toast } from 'sonner';
import { DeploymentWorkspacesSection } from './deployment-workspaces-section';

const WORKSPACES = [
  { id: 'ws-1', name: 'Acme Team', slug: 'acme', member_count: 3 },
  { id: 'ws-2', name: 'Beta Lab', slug: 'beta-lab', member_count: 1 },
];

const MEMBERS = [
  {
    user_id: 'user-1',
    name: 'Root',
    email: 'root@corp.example',
    role: 'owner',
    deactivated: false,
  },
  {
    user_id: 'user-2',
    name: 'Dev',
    email: 'dev@corp.example',
    role: 'member',
    deactivated: false,
  },
];

const CONFIG = {
  llm_base_url: 'https://gw.acme.example/v1',
  llm_model: 'coding-medium',
  has_llm_api_key: true,
  mcp_defaults: { github: { enabled: true } },
};

const DEV_OVERRIDE = {
  user_id: 'user-2',
  llm_base_url: 'https://personal.example/v1',
  llm_model: 'personal-model',
  has_llm_api_key: false,
};

beforeEach(() => {
  vi.clearAllMocks();
  state.workspaces = WORKSPACES;
  state.members = MEMBERS;
  state.config = CONFIG;
  state.overrides = [DEV_OVERRIDE];
  state.memberOverrides = { 'user-1': null, 'user-2': DEV_OVERRIDE };
  state.errors = {};
  state.queryKeys = [];
  mocks.updateConfig.mockResolvedValue(CONFIG);
  mocks.setOverride.mockResolvedValue({
    user_id: 'user-2',
    has_llm_api_key: false,
  });
  mocks.deleteOverride.mockResolvedValue(undefined);
  mocks.deactivateUser.mockResolvedValue({ user_id: 'user-2' });
  mocks.reactivateUser.mockResolvedValue({ user_id: 'user-2' });
  mocks.offboardPending = false;
});

function openAcmeCard() {
  fireEvent.click(screen.getByLabelText('Open workspace Acme Team'));
}

describe('DeploymentWorkspacesSection directory', () => {
  it('renders every workspace of the deployment with its member count', () => {
    renderWithI18n(<DeploymentWorkspacesSection />);
    expect(screen.getByText('Workspaces')).toBeTruthy();
    expect(screen.getByText('Acme Team')).toBeTruthy();
    expect(screen.getByText('Beta Lab')).toBeTruthy();
    expect(screen.getByText('Members: 3')).toBeTruthy();
    expect(screen.getByText('Members: 1')).toBeTruthy();
  });

  it('filters by name and by slug', () => {
    renderWithI18n(<DeploymentWorkspacesSection />);
    const search = screen.getByLabelText('Search workspaces');

    fireEvent.change(search, { target: { value: 'beta' } });
    expect(screen.queryByText('Acme Team')).toBeNull();
    expect(screen.getByText('Beta Lab')).toBeTruthy();

    fireEvent.change(search, { target: { value: 'acme' } });
    expect(screen.getByText('Acme Team')).toBeTruthy();
    expect(screen.queryByText('Beta Lab')).toBeNull();
  });

  it('says the deployment is empty ONLY after a successful answer', () => {
    state.workspaces = [];
    renderWithI18n(<DeploymentWorkspacesSection />);
    expect(screen.getByText('No workspaces in this deployment.')).toBeTruthy();
  });

  it('does not claim an empty deployment while the read is in flight', () => {
    state.workspaces = undefined;
    renderWithI18n(<DeploymentWorkspacesSection />);
    expect(screen.queryByText('No workspaces in this deployment.')).toBeNull();
    expect(screen.getByText('Loading…')).toBeTruthy();
  });

  it("renders a failed directory read as an error with the server's sentence and a retry", () => {
    state.workspaces = undefined;
    state.errors.workspaces = new ApiError(
      'API error: 500 Internal Server Error',
      500,
      'Internal Server Error',
      undefined,
      'failed to list workspaces',
    );
    renderWithI18n(<DeploymentWorkspacesSection />);

    expect(screen.queryByText('No workspaces in this deployment.')).toBeNull();
    expect(screen.getByRole('alert')).toBeTruthy();
    expect(screen.getByText('failed to list workspaces')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('refuses to open a workspace row the server sent without an id', () => {
    state.workspaces = [{ id: '', name: 'Nameless', slug: 'x', member_count: 0 }];
    renderWithI18n(<DeploymentWorkspacesSection />);
    const open = screen.getByLabelText('Open workspace Nameless') as HTMLButtonElement;
    expect(open.disabled).toBe(true);
    fireEvent.click(open);
    expect(screen.queryByText('Workspace configuration')).toBeNull();
  });
});

describe('DeploymentWorkspacesSection card', () => {
  it('opens the card with the masked config and the member roster', () => {
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    const baseUrl = screen.getByLabelText('Workspace LLM base URL') as HTMLInputElement;
    expect(baseUrl.value).toBe('https://gw.acme.example/v1');

    expect(screen.getByText('root@corp.example')).toBeTruthy();
    expect(screen.getByText('dev@corp.example')).toBeTruthy();
  });

  it('renders the card frame while both of its reads are still in flight', () => {
    state.members = undefined;
    state.config = undefined;
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();
    expect(screen.getByRole('button', { name: 'Close' })).toBeTruthy();
    expect(screen.getAllByText('Loading…').length).toBe(2);
  });

  it('does not render an in-flight config read as an empty, keyless configuration', () => {
    state.config = undefined;
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    expect(screen.queryByLabelText('Workspace LLM base URL')).toBeNull();
    expect(screen.queryByLabelText('Workspace LLM model')).toBeNull();
    expect(screen.queryByText('Key is set')).toBeNull();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.getByText('Loading…')).toBeTruthy();
  });

  it('does not render an unread config as an empty, keyless configuration', () => {
    state.config = undefined;
    state.errors.config = new ApiError(
      'API error: 503 Service Unavailable',
      503,
      'Service Unavailable',
      undefined,
      'stored configuration is sealed and GOOSAR_MCP_SECRET_KEY is not available',
    );
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    expect(screen.queryByLabelText('Workspace LLM base URL')).toBeNull();
    expect(screen.queryByText('Key is set')).toBeNull();
    expect(
      screen.getByText('stored configuration is sealed and GOOSAR_MCP_SECRET_KEY is not available'),
    ).toBeTruthy();
  });

  it('does not PUT the workspace config until the L2 confirm is accepted', async () => {
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    fireEvent.change(screen.getByLabelText('Workspace LLM model'), {
      target: { value: 'new-model' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save configuration' }));

    expect(screen.getByText('Apply the workspace configuration?')).toBeTruthy();
    expect(mocks.updateConfig).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(mocks.updateConfig).toHaveBeenCalledTimes(1));
    expect(mocks.updateConfig).toHaveBeenCalledWith({ llm_model: 'new-model' });
  });

  it('sends exactly what the config confirm promised, even for a field typed back to its original value', async () => {
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    const modelInput = screen.getByLabelText('Workspace LLM model');
    fireEvent.change(modelInput, { target: { value: 'scratch' } });
    fireEvent.change(modelInput, { target: { value: 'coding-medium' } });

    const save = screen.getByRole('button', {
      name: 'Save configuration',
    }) as HTMLButtonElement;
    expect(save.disabled).toBe(false);
    fireEvent.click(save);

    expect(
      within(screen.getByRole('alertdialog')).getByText('LLM model becomes coding-medium'),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(mocks.updateConfig).toHaveBeenCalledTimes(1));
    expect(mocks.updateConfig).toHaveBeenCalledWith({
      llm_model: 'coding-medium',
    });
  });

  it('sends nothing when the config confirm is declined', async () => {
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    fireEvent.change(screen.getByLabelText('Workspace LLM model'), {
      target: { value: 'new-model' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save configuration' }));

    fireEvent.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Cancel',
      }),
    );
    await waitFor(() =>
      expect(screen.queryByText('Apply the workspace configuration?')).toBeNull(),
    );
    expect(mocks.updateConfig).not.toHaveBeenCalled();
    expect((screen.getByLabelText('Workspace LLM model') as HTMLInputElement).value).toBe(
      'new-model',
    );
  });

  it("shows the server's refusal instead of swallowing it", async () => {
    mocks.updateConfig.mockRejectedValue(
      new ApiError(
        'API error: 503 Service Unavailable',
        503,
        'Service Unavailable',
        undefined,
        'cannot edit mcp configuration: stored document is sealed and GOOSAR_MCP_SECRET_KEY is not configured on this server',
      ),
    );
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    fireEvent.change(screen.getByLabelText('Workspace LLM model'), {
      target: { value: 'new-model' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save configuration' }));
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() =>
      expect(
        screen.getByText(
          'cannot edit mcp configuration: stored document is sealed and GOOSAR_MCP_SECRET_KEY is not configured on this server',
        ),
      ).toBeTruthy(),
    );
    expect(toast.error).toHaveBeenCalledWith(
      'cannot edit mcp configuration: stored document is sealed and GOOSAR_MCP_SECRET_KEY is not configured on this server',
    );
  });
});

describe('DeploymentWorkspacesSection overrides', () => {
  it('does not PUT the override until the L2 confirm is accepted', async () => {
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    fireEvent.click(screen.getByLabelText('Configure override for Dev'));
    expect(screen.getByText('Override for Dev')).toBeTruthy();

    fireEvent.change(screen.getByLabelText('Override LLM base URL'), {
      target: { value: 'https://new.example/v1' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(screen.getByText('Apply the override for Dev?')).toBeTruthy();
    expect(mocks.setOverride).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(mocks.setOverride).toHaveBeenCalledTimes(1));
    expect(mocks.setOverride).toHaveBeenCalledWith({
      userId: 'user-2',
      patch: { llm_base_url: 'https://new.example/v1' },
    });
  });

  it('removes an override only after its confirm step', async () => {
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    expect(screen.queryByLabelText('Remove override for Root')).toBeNull();
    fireEvent.click(screen.getByLabelText('Remove override for Dev'));

    expect(screen.getByText('Remove the override for Dev?')).toBeTruthy();
    expect(mocks.deleteOverride).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Remove' }));
    await waitFor(() => expect(mocks.deleteOverride).toHaveBeenCalledTimes(1));
    expect(mocks.deleteOverride).toHaveBeenCalledWith('user-2');
  });

  it('sends nothing when the override confirm is declined', async () => {
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    fireEvent.click(screen.getByLabelText('Configure override for Dev'));
    fireEvent.change(screen.getByLabelText('Override LLM base URL'), {
      target: { value: 'https://new.example/v1' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    fireEvent.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Cancel',
      }),
    );
    await waitFor(() => expect(screen.queryByText('Apply the override for Dev?')).toBeNull());
    expect(mocks.setOverride).not.toHaveBeenCalled();
    expect(screen.getByText('Override for Dev')).toBeTruthy();
  });
});

describe('DeploymentOverrideDialog against a late answer', () => {
  it('sends only the field the admin edited when the override arrives after the dialog opened', async () => {
    state.memberOverrides = { 'user-1': null };
    const { rerender } = renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    fireEvent.click(screen.getByLabelText('Configure override for Dev'));
    expect((screen.getByLabelText('Override LLM base URL') as HTMLInputElement).value).toBe('');

    state.memberOverrides = { 'user-1': null, 'user-2': DEV_OVERRIDE };
    rerender(<DeploymentWorkspacesSection />);

    expect((screen.getByLabelText('Override LLM base URL') as HTMLInputElement).value).toBe(
      'https://personal.example/v1',
    );

    fireEvent.change(screen.getByLabelText('Override LLM model'), {
      target: { value: 'new-model' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => expect(mocks.setOverride).toHaveBeenCalledTimes(1));
    expect(mocks.setOverride).toHaveBeenCalledWith({
      userId: 'user-2',
      patch: { llm_model: 'new-model' },
    });
  });

  it('sends nothing at all when the admin opens the dialog and saves without editing', async () => {
    state.memberOverrides = { 'user-1': null };
    const { rerender } = renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    fireEvent.click(screen.getByLabelText('Configure override for Dev'));
    state.memberOverrides = { 'user-1': null, 'user-2': DEV_OVERRIDE };
    rerender(<DeploymentWorkspacesSection />);

    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(screen.queryByText('Apply the override for Dev?')).toBeNull();
    await waitFor(() => expect(screen.queryByText('Override for Dev')).toBeNull());
    expect(mocks.setOverride).not.toHaveBeenCalled();
  });

  it('sends exactly what the confirm promised, even for a field typed back to its original value', async () => {
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    fireEvent.click(screen.getByLabelText('Configure override for Dev'));
    const modelInput = screen.getByLabelText('Override LLM model');
    fireEvent.change(modelInput, { target: { value: 'scratch' } });
    fireEvent.change(modelInput, { target: { value: 'personal-model' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(
      within(screen.getByRole('alertdialog')).getByText('LLM model becomes personal-model'),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(mocks.setOverride).toHaveBeenCalledTimes(1));
    expect(mocks.setOverride).toHaveBeenCalledWith({
      userId: 'user-2',
      patch: { llm_model: 'personal-model' },
    });
  });

  it('names the clearing explicitly in the confirm before anything is sent', async () => {
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    fireEvent.click(screen.getByLabelText('Configure override for Dev'));
    fireEvent.change(screen.getByLabelText('Override LLM base URL'), {
      target: { value: '' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    const confirm = screen.getByRole('alertdialog');
    expect(
      within(confirm).getByText('LLM base URL: the stored value will be CLEARED'),
    ).toBeTruthy();
    expect(within(confirm).queryByText(/LLM model/)).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(mocks.setOverride).toHaveBeenCalledTimes(1));
    expect(mocks.setOverride).toHaveBeenCalledWith({
      userId: 'user-2',
      patch: { llm_base_url: '' },
    });
  });
});

describe('DeploymentWorkspacesSection override knowledge', () => {
  it("says 'unknown', not 'No override', when the override read failed", () => {
    state.overrides = undefined;
    state.errors.overrides = new ApiError(
      'API error: 500 Internal Server Error',
      500,
      'Internal Server Error',
      undefined,
      'failed to journal configuration access',
    );
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    expect(screen.queryByText('No override')).toBeNull();
    expect(screen.getAllByText('Unknown').length).toBe(2);
    expect(screen.getByText('failed to journal configuration access')).toBeTruthy();
    expect(screen.getAllByRole('button', { name: 'Try again' }).length).toBe(1);
    expect(screen.queryByLabelText('Remove override for Dev')).toBeNull();
  });

  it("keeps each member's dialog reachable when the roster-wide override read failed", () => {
    state.overrides = undefined;
    state.errors.overrides = new ApiError(
      'API error: 404 Not Found',
      404,
      'Not Found',
      undefined,
      'unknown endpoint',
    );
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    const configure = screen.getByLabelText('Configure override for Dev') as HTMLButtonElement;
    expect(configure.disabled).toBe(false);

    fireEvent.click(configure);
    expect(screen.getByText('Override for Dev')).toBeTruthy();
    expect((screen.getByLabelText('Override LLM base URL') as HTMLInputElement).value).toBe(
      'https://personal.example/v1',
    );
  });

  it("does not say 'No override' while the roster's override read is in flight", () => {
    state.overrides = undefined;
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    expect(screen.queryByText('No override')).toBeNull();
    expect(screen.getAllByText('Unknown').length).toBe(2);
    expect(screen.queryByLabelText('Remove override for Dev')).toBeNull();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it("says 'unknown' for everyone when a row of the listing arrived without a user_id", () => {
    state.overrides = [{ ...DEV_OVERRIDE, user_id: '' }];
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    expect(screen.queryByText('No override')).toBeNull();
    expect(screen.getAllByText('Unknown').length).toBe(2);
    expect(screen.queryByLabelText('Remove override for Dev')).toBeNull();
  });

  it("reads the workspace's overrides ONCE, not once per member", () => {
    state.members = Array.from({ length: 25 }, (_, i) => ({
      user_id: `user-${i}`,
      name: `Member ${i}`,
      email: `m${i}@corp.example`,
      role: 'member',
    }));
    state.overrides = [];
    state.memberOverrides = {};
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    const overrideReads = (): string[] => [
      ...new Set(state.queryKeys.filter((k) => k.includes('/overrides'))),
    ];
    expect(overrideReads()).toEqual(['deployment/workspaces/ws-1/overrides']);

    fireEvent.click(screen.getByLabelText('Configure override for Member 7'));
    expect(overrideReads().sort()).toEqual([
      'deployment/workspaces/ws-1/overrides',
      'deployment/workspaces/ws-1/overrides/user-7',
    ]);
  });

  it("says 'No override' only when the server actually answered", () => {
    state.overrides = [];
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();
    expect(screen.getAllByText('No override').length).toBe(2);
    expect(screen.queryByText('Unknown')).toBeNull();
  });

  it('gives a member row without a user_id no actions', () => {
    state.members = [{ user_id: '', name: 'Ghost', email: '', role: 'member' }];
    state.overrides = [];
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    const configure = screen.getByLabelText('Configure override for Ghost') as HTMLButtonElement;
    expect(configure.disabled).toBe(true);
    expect(screen.queryByLabelText('Remove override for Ghost')).toBeNull();
    const deactivate = screen.getByLabelText('Deactivate Ghost') as HTMLButtonElement;
    expect(deactivate.disabled).toBe(true);
  });
});

describe('DeploymentWorkspacesSection offboarding', () => {
  it('deactivates a member only after the confirm step', async () => {
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    fireEvent.click(screen.getByLabelText('Deactivate Dev'));
    expect(screen.getByText('Deactivate Dev?')).toBeTruthy();
    expect(mocks.deactivateUser).not.toHaveBeenCalled();

    fireEvent.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Deactivate',
      }),
    );
    await waitFor(() => expect(mocks.deactivateUser).toHaveBeenCalledTimes(1));
    expect(mocks.deactivateUser).toHaveBeenCalledWith('user-2');
    expect(toast.success).toHaveBeenCalledWith('Account deactivated');
  });

  it('sends nothing when the offboarding confirm is declined', () => {
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    fireEvent.click(screen.getByLabelText('Deactivate Dev'));
    fireEvent.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Cancel',
      }),
    );
    expect(mocks.deactivateUser).not.toHaveBeenCalled();
  });

  it('shows a deactivated member as blocked and offers the way back', async () => {
    state.members = [{ ...MEMBERS[0] }, { ...MEMBERS[1], deactivated: true }];
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    expect(screen.getByText('Deactivated')).toBeTruthy();
    expect(screen.queryByLabelText('Deactivate Dev')).toBeNull();

    fireEvent.click(screen.getByLabelText('Restore access for Dev'));
    expect(screen.getByText('Restore access for Dev?')).toBeTruthy();
    fireEvent.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Restore access',
      }),
    );
    await waitFor(() => expect(mocks.reactivateUser).toHaveBeenCalledTimes(1));
    expect(mocks.reactivateUser).toHaveBeenCalledWith('user-2');
    expect(mocks.deactivateUser).not.toHaveBeenCalled();
    expect(toast.success).toHaveBeenCalledWith('Access restored');
  });

  it('shows the server refusal instead of a silent failure', async () => {
    mocks.deactivateUser.mockRejectedValue(
      new ApiError(
        'API error: 409 Conflict',
        409,
        'Conflict',
        { error: 'cannot deactivate your own account' },
        'cannot deactivate your own account',
      ),
    );
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    fireEvent.click(screen.getByLabelText('Deactivate Dev'));
    fireEvent.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Deactivate',
      }),
    );

    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1));
    expect(String(vi.mocked(toast.error).mock.calls[0]?.[0])).toContain(
      'cannot deactivate your own account',
    );
  });

  it('locks the action while a call is in flight', () => {
    mocks.offboardPending = true;
    renderWithI18n(<DeploymentWorkspacesSection />);
    openAcmeCard();

    const button = screen.getByLabelText('Deactivate Dev') as HTMLButtonElement;
    expect(button.disabled).toBe(true);
  });
});
