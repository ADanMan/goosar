import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { renderWithI18n } from '../../test/i18n';

const mocks = vi.hoisted(() => ({
  addAdmin: vi.fn(),
  removeAdmin: vi.fn(),
  updatePolicy: vi.fn(),
}));

const adminsRef = vi.hoisted(() => ({ current: null as unknown }));
const pendingRef = vi.hoisted(() => ({ current: [] as unknown }));
const auditRef = vi.hoisted(() => ({ current: [] as unknown }));
const policyRef = vi.hoisted(() => ({ current: { policy: {} } as unknown }));
const workspacesRef = vi.hoisted(() => ({ current: [] as unknown }));

function dispatchByKey(queryKey: unknown[]): { data: unknown } {
  const key = JSON.stringify(queryKey);
  if (key.includes('pending')) return { data: pendingRef.current };
  if (key.includes('audit')) return { data: auditRef.current };
  if (key.includes('admins')) return { data: adminsRef.current };
  if (key.includes('policy')) return { data: policyRef.current };
  if (key.includes('workspaces')) return { data: workspacesRef.current };
  return { data: undefined };
}

vi.mock('@tanstack/react-query', () => ({
  useQuery: (opts: { queryKey: unknown[] }) => dispatchByKey(opts.queryKey),
  useQueries: ({ queries }: { queries: { queryKey: unknown[] }[] }) =>
    queries.map((q) => dispatchByKey(q.queryKey)),
  useQueryClient: () => ({ invalidateQueries: vi.fn(), setQueryData: vi.fn() }),
  useMutation: () => ({ mutateAsync: vi.fn(), isPending: false }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock('@goosar/core/deployment/mcp-servers', () => ({
  deploymentMcpServersOptions: () => ({
    queryKey: ['deployment', 'mcp-servers'],
    queryFn: vi.fn(),
  }),
  useCreateDeploymentMcpServer: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
  useUpdateDeploymentMcpServer: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
  useDeleteDeploymentMcpServer: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
}));

vi.mock('@goosar/core/deployment/admin', () => ({
  deploymentAdminsOptions: () => ({
    queryKey: ['deployment', 'admins'],
    queryFn: vi.fn(),
  }),
  deploymentAdminPendingOptions: () => ({
    queryKey: ['deployment', 'admins', 'pending'],
    queryFn: vi.fn(),
  }),
  deploymentAuditOptions: () => ({
    queryKey: ['deployment', 'audit', null],
    queryFn: vi.fn(),
  }),
  deploymentPolicyOptions: () => ({
    queryKey: ['deployment', 'policy'],
    queryFn: vi.fn(),
  }),
  useAddDeploymentAdmin: () => ({
    mutateAsync: mocks.addAdmin,
    isPending: false,
  }),
  useRemoveDeploymentAdmin: () => ({
    mutateAsync: mocks.removeAdmin,
    isPending: false,
  }),
  useUpdateDeploymentPolicy: () => ({
    mutateAsync: mocks.updatePolicy,
    isPending: false,
  }),
  deploymentWorkspacesOptions: () => ({
    queryKey: ['deployment', 'workspaces'],
    queryFn: vi.fn(),
  }),
  DEPLOYMENT_FLEET_REFRESH_MS: 30_000,
  deploymentFleetOptions: () => ({
    queryKey: ['deployment', 'fleet'],
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
  deploymentUserConfigOverrideOptions: (wsId: string, userId: string) => ({
    queryKey: ['deployment', 'workspaces', wsId, 'overrides', userId],
    queryFn: vi.fn(),
  }),
  useUpdateDeploymentWorkspaceConfig: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
  useSetDeploymentUserConfigOverride: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
  useDeleteDeploymentUserConfigOverride: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { ApiError } from '@goosar/core/api';
import { DeploymentTab } from './deployment-tab';

const ADMINS = [
  {
    user_id: 'user-1',
    email: 'root@corp.example',
    name: 'Root',
    granted_at: '2026-08-01T12:00:00Z',
  },
  {
    user_id: 'user-2',
    email: 'second@corp.example',
    name: 'Second',
    granted_at: '2026-08-02T12:00:00Z',
  },
];

const PENDING_GRANT = {
  status: 'pending',
  request_id: 'req-1',
  action: 'grant',
  target_user_id: 'user-9',
  target_email: 'new-admin@corp.example',
  requested_by: 'user-1',
  requested_at: '2026-08-27T10:00:00Z',
  confirm_hint: 'run on the server: `docker compose exec backend ./goosar_admin confirm req-1`',
};

beforeEach(() => {
  vi.clearAllMocks();
  adminsRef.current = ADMINS;
  pendingRef.current = [];
  auditRef.current = [];
  policyRef.current = {
    policy: {
      llm: { base_url: 'https://gw.corp.example/v1', model: 'openai/coding-medium' },
      mcp: { github: { enabled: true } },
    },
  };
  mocks.addAdmin.mockResolvedValue({ kind: 'pending', pending: PENDING_GRANT });
  mocks.removeAdmin.mockResolvedValue({
    ...PENDING_GRANT,
    request_id: 'req-2',
    action: 'revoke',
    target_user_id: 'user-2',
  });
  mocks.updatePolicy.mockResolvedValue({ policy: {} });
});

describe('DeploymentTab gate', () => {
  it('renders nothing when the server answered 403 (query data null)', () => {
    adminsRef.current = null;
    const { container } = renderWithI18n(<DeploymentTab />);
    expect(container.firstChild).toBeNull();
  });

  it('renders nothing while the probe is still in flight', () => {
    adminsRef.current = undefined;
    const { container } = renderWithI18n(<DeploymentTab />);
    expect(container.firstChild).toBeNull();
  });

  it('renders the surface when the server answered with the admin list', () => {
    renderWithI18n(<DeploymentTab />);
    expect(screen.getByText('Deployment administrators')).toBeTruthy();
    expect(screen.getByText('root@corp.example')).toBeTruthy();
    expect(screen.getByText('Deployment policy')).toBeTruthy();
    expect(screen.getByText('Workspaces')).toBeTruthy();
  });
});

describe('DeploymentTab administrators', () => {
  it("files a grant and shows the pending status with the server's confirm command", async () => {
    renderWithI18n(<DeploymentTab />);
    fireEvent.change(screen.getByLabelText('Email of the user to grant the role to'), {
      target: { value: 'new-admin@corp.example' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Request grant' }));

    await waitFor(() => expect(mocks.addAdmin).toHaveBeenCalledTimes(1));
    expect(mocks.addAdmin).toHaveBeenCalledWith('new-admin@corp.example');
    expect(screen.queryByText('new-admin@corp.example')).toBeNull();
  });

  it("renders the server's pending queue with its confirm command verbatim", () => {
    pendingRef.current = [PENDING_GRANT];
    renderWithI18n(<DeploymentTab />);

    expect(screen.getByText('Pending')).toBeTruthy();
    expect(screen.getByText('Grant the role to new-admin@corp.example')).toBeTruthy();
    expect(screen.getByText(PENDING_GRANT.confirm_hint)).toBeTruthy();
    expect(screen.getByText('To apply, confirm on the server:')).toBeTruthy();
  });

  it('files a revoke as pending — the row does not disappear', async () => {
    renderWithI18n(<DeploymentTab />);
    fireEvent.click(
      screen.getByLabelText('Request revoking the deployment admin role from second@corp.example'),
    );

    await waitFor(() => expect(mocks.removeAdmin).toHaveBeenCalledTimes(1));
    expect(mocks.removeAdmin).toHaveBeenCalledWith('user-2');
    expect(screen.getByText('second@corp.example')).toBeTruthy();
  });

  it("surfaces the server's own sentence when removing the last admin", async () => {
    const sentence = 'cannot remove the last deployment administrator';
    mocks.removeAdmin.mockRejectedValue(
      new ApiError(sentence, 409, 'Conflict', { error: sentence }, sentence),
    );
    renderWithI18n(<DeploymentTab />);
    fireEvent.click(
      screen.getByLabelText('Request revoking the deployment admin role from root@corp.example'),
    );

    await waitFor(() => expect(screen.getByRole('alert')).toBeTruthy());
    expect(screen.getByRole('alert').textContent).toBe(sentence);
  });
});

describe('DeploymentTab audit journal', () => {
  it('says so when the journal is empty', () => {
    renderWithI18n(<DeploymentTab />);
    expect(screen.getByText('Audit journal')).toBeTruthy();
    expect(screen.getByText('No audit entries yet.')).toBeTruthy();
  });

  it('renders a server-channel row without inventing an actor', () => {
    auditRef.current = [
      {
        id: 'audit-1',
        action: 'deployment_admin.grant.confirmed',
        target_type: 'user',
        target_id: 'user-9',
        created_at: '2026-09-07T10:00:00Z',
      },
    ];
    renderWithI18n(<DeploymentTab />);

    expect(screen.getByText('deployment_admin.grant.confirmed')).toBeTruthy();
    expect(screen.getByText('actor server channel → user:user-9')).toBeTruthy();
  });
});

describe('DeploymentTab policy L2 confirmation', () => {
  it('does not PUT until the confirm dialog is accepted', async () => {
    renderWithI18n(<DeploymentTab />);
    fireEvent.change(screen.getByLabelText('LLM base URL'), {
      target: { value: 'https://new-gw.corp.example/v1' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(screen.getByText('Apply the LLM policy?')).toBeTruthy();
    expect(mocks.updatePolicy).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(mocks.updatePolicy).toHaveBeenCalledTimes(1));

    const doc = mocks.updatePolicy.mock.calls[0]![0] as Record<string, unknown>;
    expect(doc).toEqual({
      llm: {
        base_url: 'https://new-gw.corp.example/v1',
        model: 'openai/coding-medium',
      },
      mcp: { github: { enabled: true } },
    });
  });
});

describe('DeploymentTab policy L3 kill switch', () => {
  it('refuses to PUT without the typed target phrase', async () => {
    renderWithI18n(<DeploymentTab />);
    fireEvent.click(screen.getByLabelText('Disable MCP for everyone'));

    expect(screen.getByText('Disable MCP for the whole deployment?')).toBeTruthy();
    const confirmButton = screen.getByRole('button', { name: 'Disable MCP' });

    fireEvent.click(confirmButton);
    expect(mocks.updatePolicy).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'disable' },
    });
    fireEvent.click(confirmButton);
    expect(mocks.updatePolicy).not.toHaveBeenCalled();
  });

  it('PUTs the one wildcard form the server admits after the exact phrase', async () => {
    renderWithI18n(<DeploymentTab />);
    fireEvent.click(screen.getByLabelText('Disable MCP for everyone'));
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'DISABLE' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Disable MCP' }));

    await waitFor(() => expect(mocks.updatePolicy).toHaveBeenCalledTimes(1));
    const doc = mocks.updatePolicy.mock.calls[0]![0] as {
      mcp?: Record<string, unknown>;
    };
    expect(doc.mcp?.['*']).toEqual({ enabled: false, locked: true });
    expect(doc.mcp?.github).toEqual({ enabled: true });
  });

  it('lifting the kill switch asks a plain confirm and removes the wildcard', async () => {
    policyRef.current = {
      policy: {
        mcp: { '*': { enabled: false, locked: true }, github: { enabled: true } },
      },
    };
    renderWithI18n(<DeploymentTab />);
    fireEvent.click(screen.getByLabelText('Disable MCP for everyone'));

    expect(screen.getByText('Re-enable MCP?')).toBeTruthy();
    expect(mocks.updatePolicy).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Re-enable' }));
    await waitFor(() => expect(mocks.updatePolicy).toHaveBeenCalledTimes(1));
    const doc = mocks.updatePolicy.mock.calls[0]![0] as {
      mcp?: Record<string, unknown>;
    };
    expect(doc.mcp?.['*']).toBeUndefined();
    expect(doc.mcp?.github).toEqual({ enabled: true });
  });
});
