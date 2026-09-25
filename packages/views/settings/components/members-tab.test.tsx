import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enSettings from '../../locales/en/settings.json';

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

const mocks = vi.hoisted(() => ({
  createMember: vi.fn(),
  updateMember: vi.fn(),
  deleteMember: vi.fn(),
  revokeInvitation: vi.fn(),
  invalidate: vi.fn(),
}));

type TestMember = {
  id: string;
  user_id: string;
  role: 'owner' | 'admin' | 'member';
  name: string;
  email: string;
  perimeter_access?: boolean;
};

const membersRef = vi.hoisted(() => ({ current: [] as unknown[] }));
const invitationsRef = vi.hoisted(() => ({ current: [] as unknown[] }));
const viewerRef = vi.hoisted(() => ({ current: { id: 'user-1' } }));

vi.mock('@tanstack/react-query', () => ({
  useQuery: (opts: { queryKey: unknown[] }) => {
    const key = JSON.stringify(opts.queryKey);
    if (key.includes('members')) {
      return { data: membersRef.current, isLoading: false };
    }
    if (key.includes('invitations')) {
      return { data: invitationsRef.current, isLoading: false };
    }
    return { data: [], isLoading: false };
  },
  useQueryClient: () => ({ invalidateQueries: mocks.invalidate }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock('@goosar/core/hooks', () => ({
  useWorkspaceId: () => 'ws-1',
}));

vi.mock('@goosar/core/workspace/queries', () => ({
  memberListOptions: () => ({ queryKey: ['members'], queryFn: vi.fn() }),
  invitationListOptions: () => ({ queryKey: ['invitations'], queryFn: vi.fn() }),
  workspaceKeys: {
    members: (wsId: string) => ['members', wsId],
    invitations: (wsId: string) => ['invitations', wsId],
  },
}));

vi.mock('@goosar/core/paths', () => ({
  useCurrentWorkspace: () => ({ id: 'ws-1', name: 'Acme' }),
}));

vi.mock('@goosar/core/auth', () => {
  const state = () => ({ user: viewerRef.current });
  const store = (selector?: (s: ReturnType<typeof state>) => unknown) =>
    selector ? selector(state()) : state();
  store.getState = state;
  return { useAuthStore: store };
});

vi.mock('@goosar/core/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/api')>();
  return {
    ...actual,
    api: {
      createMember: mocks.createMember,
      updateMember: mocks.updateMember,
      deleteMember: mocks.deleteMember,
      revokeInvitation: mocks.revokeInvitation,
    },
  };
});

vi.mock('../../common/actor-avatar', () => ({
  ActorAvatar: () => null,
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { configStore } from '@goosar/core/config';
import { MembersTab } from './members-tab';

function setDeliveryProfile(value: string | undefined) {
  configStore.setState({ deliveryProfile: value } as unknown as Parameters<
    typeof configStore.setState
  >[0]);
}

function member(
  partial: Partial<TestMember> & Pick<TestMember, 'id' | 'user_id' | 'role'>,
): TestMember {
  return {
    name: `Name ${partial.id}`,
    email: `${partial.id}@example.com`,
    ...partial,
  };
}

function renderTab() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <MembersTab />
    </I18nProvider>,
  );
}

function menuTriggers() {
  return screen.queryAllByRole('button').filter((b) => (b.textContent ?? '').trim() === '');
}

function clickConfirm() {
  fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
}

function clickCancel() {
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
}

const ROLE_DESCRIPTION = {
  owner: 'Full access, manage all settings',
  admin: 'Manage members and settings',
  member: 'Create and work on tasks',
} as const;

function pickRole(role: keyof typeof ROLE_DESCRIPTION) {
  fireEvent.click(screen.getByText('Change role'));
  fireEvent.click(screen.getByText(ROLE_DESCRIPTION[role]));
}

describe('MembersTab perimeter grant (issue #47)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.updateMember.mockResolvedValue({});
    viewerRef.current = { id: 'user-1' };
  });

  afterEach(() => {
    setDeliveryProfile(undefined);
  });

  it('cloud profile: no perimeter action and no badge, even for a granted member', () => {
    setDeliveryProfile(undefined);
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'owner' }),
      member({ id: 'm-2', user_id: 'user-2', role: 'member', perimeter_access: true }),
    ];
    renderTab();
    expect(screen.queryByText('Perimeter')).toBeNull();
    const [trigger] = menuTriggers();
    expect(trigger).toBeTruthy();
    fireEvent.click(trigger!);
    expect(screen.queryByText(/grant perimeter access/i)).toBeNull();
    expect(screen.getByText(/change role/i)).toBeInTheDocument();
  });

  it('perimeter profile: owner grants a member from the row menu', async () => {
    setDeliveryProfile('perimeter');
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'owner' }),
      member({ id: 'm-2', user_id: 'user-2', role: 'member' }),
    ];
    renderTab();
    const triggers = menuTriggers();
    fireEvent.click(triggers[triggers.length - 1]!);
    fireEvent.click(screen.getByText(/grant perimeter access/i));
    expect(mocks.updateMember).not.toHaveBeenCalled();
    clickConfirm();
    await waitFor(() =>
      expect(mocks.updateMember).toHaveBeenCalledWith('ws-1', 'm-2', {
        perimeter_access: true,
      }),
    );
    expect(mocks.invalidate).toHaveBeenCalled();
  });

  it('perimeter profile: a granted member shows the badge and a revoke action', async () => {
    setDeliveryProfile('perimeter');
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'owner' }),
      member({ id: 'm-2', user_id: 'user-2', role: 'member', perimeter_access: true }),
    ];
    renderTab();
    expect(screen.getByText('Perimeter')).toBeInTheDocument();
    const triggers = menuTriggers();
    fireEvent.click(triggers[triggers.length - 1]!);
    fireEvent.click(screen.getByText(/revoke perimeter access/i));
    expect(mocks.updateMember).not.toHaveBeenCalled();
    clickConfirm();
    await waitFor(() =>
      expect(mocks.updateMember).toHaveBeenCalledWith('ws-1', 'm-2', {
        perimeter_access: false,
      }),
    );
  });

  it("perimeter profile: admin cannot touch the owner's grant but can self-grant", async () => {
    setDeliveryProfile('perimeter');
    viewerRef.current = { id: 'user-1' };
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'admin' }),
      member({ id: 'm-9', user_id: 'user-9', role: 'owner' }),
    ];
    renderTab();
    const triggers = menuTriggers();
    expect(triggers).toHaveLength(1);
    fireEvent.click(triggers[0]!);
    fireEvent.click(screen.getByText(/grant perimeter access/i));
    clickConfirm();
    await waitFor(() =>
      expect(mocks.updateMember).toHaveBeenCalledWith('ws-1', 'm-1', {
        perimeter_access: true,
      }),
    );
  });

  it('perimeter profile: plain members get no menus at all', () => {
    setDeliveryProfile('perimeter');
    viewerRef.current = { id: 'user-2' };
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'owner' }),
      member({ id: 'm-2', user_id: 'user-2', role: 'member' }),
    ];
    renderTab();
    expect(menuTriggers()).toHaveLength(0);
  });
});

describe('MembersTab member removal (§3 L3 typed target)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.updateMember.mockResolvedValue({});
    mocks.deleteMember.mockResolvedValue(undefined);
    mocks.revokeInvitation.mockResolvedValue(undefined);
    setDeliveryProfile(undefined);
    viewerRef.current = { id: 'user-1' };
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'owner', name: 'Alice' }),
      member({ id: 'm-2', user_id: 'user-2', role: 'member', name: 'Bob' }),
    ];
    invitationsRef.current = [];
  });

  function openRemoveDialog() {
    const triggers = menuTriggers();
    fireEvent.click(triggers[triggers.length - 1]!);
    fireEvent.click(screen.getByText('Remove from workspace'));
  }

  it("does not delete until the member's display name is typed", async () => {
    renderTab();
    openRemoveDialog();
    expect(screen.getByText('Remove Bob')).toBeInTheDocument();
    const confirmButton = screen.getByRole('button', { name: 'Confirm' });

    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.deleteMember).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Alice' },
    });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.deleteMember).not.toHaveBeenCalled();
  });

  it('deletes only on an exact match — no trimming, like every other typed gate', async () => {
    renderTab();
    openRemoveDialog();
    const confirmButton = screen.getByRole('button', { name: 'Confirm' });

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: '  Bob  ' },
    });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.deleteMember).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Bob' },
    });
    expect(confirmButton).toBeEnabled();
    fireEvent.click(confirmButton);
    await waitFor(() => expect(mocks.deleteMember).toHaveBeenCalledWith('ws-1', 'm-2'));
    expect(mocks.invalidate).toHaveBeenCalled();
  });

  it('an empty display name falls back to the email instead of disarming the gate', async () => {
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'owner', name: 'Alice' }),
      member({ id: 'm-2', user_id: 'user-2', role: 'member', name: '' }),
    ];
    renderTab();
    openRemoveDialog();

    const input = screen.getByLabelText(/Type/);
    const confirmButton = screen.getByRole('button', { name: 'Confirm' });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.deleteMember).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: '' } });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.deleteMember).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: 'm-2@example.com' } });
    fireEvent.click(confirmButton);
    await waitFor(() => expect(mocks.deleteMember).toHaveBeenCalledWith('ws-1', 'm-2'));
  });

  it('removes a member whose display name carries surrounding whitespace', async () => {
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'owner', name: 'Alice' }),
      member({ id: 'm-2', user_id: 'user-2', role: 'member', name: '  Bob  ' }),
    ];
    renderTab();
    openRemoveDialog();
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Bob' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => expect(mocks.deleteMember).toHaveBeenCalledWith('ws-1', 'm-2'));
  });

  it('cannot remove a member with neither a name nor an email — fail closed', () => {
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'owner', name: 'Alice' }),
      member({ id: 'm-2', user_id: 'user-2', role: 'member', name: '', email: '' }),
    ];
    renderTab();
    openRemoveDialog();

    const confirmButton = screen.getByRole('button', { name: 'Confirm' });
    expect(confirmButton).toBeDisabled();
    fireEvent.click(confirmButton);
    expect(mocks.deleteMember).not.toHaveBeenCalled();

    expect(screen.queryByLabelText(/Type/)).toBeNull();
    expect(
      screen.getByText(
        'This member has neither a display name nor an email to type, so the removal cannot be confirmed here.',
      ),
    ).toBeInTheDocument();
  });

  it('confirms with Enter through the same gate as every other typed dialog', async () => {
    renderTab();
    openRemoveDialog();
    const input = screen.getByLabelText(/Type/);

    fireEvent.keyDown(input, { key: 'Enter' });
    expect(mocks.deleteMember).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: 'Bo' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(mocks.deleteMember).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: 'Bob' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(mocks.deleteMember).toHaveBeenCalledWith('ws-1', 'm-2'));
  });

  it('puts focus inside the dialog even though it opens from the row menu', async () => {
    renderTab();
    openRemoveDialog();
    await waitFor(() => expect(document.activeElement?.closest('[role=dialog]')).not.toBeNull());
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(document.activeElement?.closest('[role=dialog]')).not.toBeNull();
  });

  it('does not send a second DELETE while the first is in flight', async () => {
    let release: (() => void) | undefined;
    mocks.deleteMember.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          release = () => resolve();
        }),
    );
    renderTab();
    openRemoveDialog();
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Bob' },
    });
    const confirmButton = screen.getByRole('button', { name: 'Confirm' });
    fireEvent.click(confirmButton);

    await waitFor(() => expect(confirmButton).toBeDisabled());
    fireEvent.click(confirmButton);
    expect(mocks.deleteMember).toHaveBeenCalledTimes(1);
    release?.();
  });

  it('keeps the dialog and the typed target after a failed delete', async () => {
    mocks.deleteMember.mockRejectedValueOnce(new Error('boom'));
    renderTab();
    openRemoveDialog();
    const input = screen.getByLabelText(/Type/) as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'Bob' } });
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));

    await waitFor(() => expect(mocks.deleteMember).toHaveBeenCalledTimes(1));
    expect(screen.getByText('Remove Bob')).toBeInTheDocument();
    expect((screen.getByLabelText(/Type/) as HTMLInputElement).value).toBe('Bob');
  });

  it('keeps a long target inside the dialog and out of the placeholder', () => {
    const long = 'b'.repeat(200);
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'owner', name: 'Alice' }),
      member({ id: 'm-2', user_id: 'user-2', role: 'member', name: long }),
    ];
    renderTab();
    openRemoveDialog();

    const input = screen.getByLabelText(/Type/) as HTMLInputElement;
    expect(input.placeholder).not.toContain(long);
    const code = screen.getByText(long, { selector: 'code' });
    expect(code.className).toContain('break-all');
    expect(code.closest('label')!.className).toContain('flex-wrap');

    const dialog = screen.getByRole('dialog');
    expect(dialog.querySelector('[data-slot=dialog-title]')!.className).toContain('break-words');
    expect(dialog.querySelector('[data-slot=dialog-description]')!.className).toContain(
      'break-words',
    );
  });

  it('a reopened dialog starts with a fresh, unmatched input', async () => {
    renderTab();
    openRemoveDialog();
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Bob' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(mocks.deleteMember).not.toHaveBeenCalled();

    openRemoveDialog();
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    expect(mocks.deleteMember).not.toHaveBeenCalled();
  });

  it('revoking an invitation keeps the plain confirm, no typed gate', async () => {
    invitationsRef.current = [{ id: 'inv-1', invitee_email: 'carol@example.com', role: 'member' }];
    renderTab();
    fireEvent.click(screen.getByTitle('Revoke invitation'));
    expect(screen.queryByLabelText(/Type/)).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => expect(mocks.revokeInvitation).toHaveBeenCalledWith('ws-1', 'inv-1'));
  });
});

describe('MembersTab role changes (issue #248, §3 classes)', () => {
  const ROW = { bob: 0, carol: 1, dave: 2 } as const;

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.updateMember.mockResolvedValue({});
    mocks.createMember.mockResolvedValue({});
    setDeliveryProfile(undefined);
    viewerRef.current = { id: 'user-1' };
    invitationsRef.current = [];
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'owner', name: 'Alice' }),
      member({ id: 'm-2', user_id: 'user-2', role: 'member', name: 'Bob' }),
      member({ id: 'm-3', user_id: 'user-3', role: 'admin', name: 'Carol' }),
      member({ id: 'm-4', user_id: 'user-4', role: 'owner', name: 'Dave' }),
    ];
  });

  function openRowMenu(index: number) {
    fireEvent.click(menuTriggers()[index]!);
  }

  it("promoting a member to admin is typed against the member's name", () => {
    renderTab();
    openRowMenu(ROW.bob);
    pickRole('admin');

    const confirm = screen.getByRole('button', { name: 'Confirm' });
    expect(confirm).toBeDisabled();
    fireEvent.click(confirm);
    expect(mocks.updateMember).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Carol' },
    });
    expect(confirm).toBeDisabled();
    fireEvent.click(confirm);
    expect(mocks.updateMember).not.toHaveBeenCalled();
  });

  it("sends the promotion once the member's name matches", async () => {
    renderTab();
    openRowMenu(ROW.bob);
    pickRole('admin');
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Bob' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() =>
      expect(mocks.updateMember).toHaveBeenCalledWith('ws-1', 'm-2', {
        role: 'admin',
      }),
    );
    expect(mocks.invalidate).toHaveBeenCalled();
  });

  it('the promotion dialog names the powers handed over, not the click', () => {
    renderTab();
    openRowMenu(ROW.bob);
    pickRole('admin');
    expect(
      screen.getByText(/gateway address and key for the whole workspace/i),
    ).toBeInTheDocument();
  });

  it('the promotion dialog describes key access as managing, not reading', () => {
    renderTab();
    openRowMenu(ROW.bob);
    pickRole('admin');
    expect(screen.queryByText(/keys already stored stay hidden/i)).toBeNull();
    expect(
      screen.queryByText(/read the integration keys other members have already entered/i),
    ).toBeNull();
    expect(
      screen.getByText(/manage the integration keys other members have already entered/i),
    ).toBeInTheDocument();
    expect(screen.getByText(/without Goosar showing them a stored value/i)).toBeInTheDocument();
    expect(
      screen.getByText(
        /Adding or changing an MCP server on an agent they do not own stays with that agent's owner/i,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/whoever hosts that runtime receives the agent's values in plaintext/i),
    ).toBeInTheDocument();
  });

  it('handing out ownership is typed against the WORKSPACE name', async () => {
    renderTab();
    openRowMenu(ROW.bob);
    pickRole('owner');

    const confirm = screen.getByRole('button', { name: 'Confirm' });
    const input = screen.getByLabelText(/Type/);
    fireEvent.change(input, { target: { value: 'Bob' } });
    expect(confirm).toBeDisabled();
    fireEvent.click(confirm);
    expect(mocks.updateMember).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: 'Acme' } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(mocks.updateMember).toHaveBeenCalledWith('ws-1', 'm-2', {
        role: 'owner',
      }),
    );
  });

  it('the ownership dialog warns the grant may not be reversible', () => {
    renderTab();
    openRowMenu(ROW.bob);
    pickRole('owner');
    expect(screen.getByText(/including you/i)).toBeInTheDocument();
  });

  it("taking ownership away is typed against that owner's name", async () => {
    renderTab();
    openRowMenu(ROW.dave);
    pickRole('admin');

    const confirm = screen.getByRole('button', { name: 'Confirm' });
    expect(confirm).toBeDisabled();
    fireEvent.click(confirm);
    expect(mocks.updateMember).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Dave' },
    });
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(mocks.updateMember).toHaveBeenCalledWith('ws-1', 'm-4', {
        role: 'admin',
      }),
    );
  });

  it('demoting an admin to member is a plain confirmation, not a typed one', async () => {
    renderTab();
    openRowMenu(ROW.carol);
    pickRole('member');

    expect(screen.queryByLabelText(/Type/)).toBeNull();
    expect(mocks.updateMember).not.toHaveBeenCalled();
    clickConfirm();
    await waitFor(() =>
      expect(mocks.updateMember).toHaveBeenCalledWith('ws-1', 'm-3', {
        role: 'member',
      }),
    );
  });

  it('the demotion dialog says perimeter access does NOT go with the role', () => {
    renderTab();
    openRowMenu(ROW.carol);
    pickRole('member');
    expect(screen.getByText(/perimeter access, if granted, stays granted/i)).toBeInTheDocument();
  });

  it('picking the role a member already holds asks nothing and sends nothing', () => {
    renderTab();
    openRowMenu(ROW.carol);
    pickRole('admin');
    expect(screen.queryByRole('button', { name: 'Confirm' })).toBeNull();
    expect(mocks.updateMember).not.toHaveBeenCalled();
  });

  it('cancelling a plain role confirmation mutates nothing', () => {
    renderTab();
    openRowMenu(ROW.carol);
    pickRole('member');
    clickCancel();
    expect(mocks.updateMember).not.toHaveBeenCalled();
  });

  it('cancelling a typed role confirmation mutates nothing and resets the field', () => {
    renderTab();
    openRowMenu(ROW.bob);
    pickRole('admin');
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Bob' },
    });
    clickCancel();
    expect(mocks.updateMember).not.toHaveBeenCalled();

    openRowMenu(ROW.bob);
    pickRole('admin');
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    expect(mocks.updateMember).not.toHaveBeenCalled();
  });

  it('does not send a second role PATCH while the plain confirm is in flight', async () => {
    let release: (() => void) | undefined;
    mocks.updateMember.mockImplementation(
      () =>
        new Promise((resolve) => {
          release = () => resolve({});
        }),
    );
    renderTab();
    openRowMenu(ROW.carol);
    pickRole('member');
    const confirm = screen.getByRole('button', { name: 'Confirm' });
    fireEvent.click(confirm);

    await waitFor(() => expect(confirm).toBeDisabled());
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled();
    fireEvent.click(confirm);
    expect(mocks.updateMember).toHaveBeenCalledTimes(1);
    release?.();
  });

  it('does not send a second role PATCH while the typed confirm is in flight', async () => {
    let release: (() => void) | undefined;
    mocks.updateMember.mockImplementation(
      () =>
        new Promise((resolve) => {
          release = () => resolve({});
        }),
    );
    renderTab();
    openRowMenu(ROW.bob);
    pickRole('admin');
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'Bob' },
    });
    const confirm = screen.getByRole('button', { name: 'Confirm' });
    fireEvent.click(confirm);

    await waitFor(() => expect(confirm).toBeDisabled());
    fireEvent.click(confirm);
    expect(mocks.updateMember).toHaveBeenCalledTimes(1);
    release?.();
  });

  it('names a member without a display name by email in the role gate', async () => {
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'owner', name: 'Alice' }),
      member({ id: 'm-2', user_id: 'user-2', role: 'member', name: '' }),
    ];
    renderTab();
    openRowMenu(0);
    pickRole('admin');

    const confirm = screen.getByRole('button', { name: 'Confirm' });
    expect(confirm).toBeDisabled();
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'm-2@example.com' },
    });
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(mocks.updateMember).toHaveBeenCalledWith('ws-1', 'm-2', {
        role: 'admin',
      }),
    );
  });

  it('fails closed when a member has neither a name nor an email', () => {
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'owner', name: 'Alice' }),
      member({
        id: 'm-2',
        user_id: 'user-2',
        role: 'member',
        name: '',
        email: '',
      }),
    ];
    renderTab();
    openRowMenu(0);
    pickRole('admin');

    expect(screen.queryByLabelText(/Type/)).toBeNull();
    const confirm = screen.getByRole('button', { name: 'Confirm' });
    expect(confirm).toBeDisabled();
    fireEvent.click(confirm);
    expect(mocks.updateMember).not.toHaveBeenCalled();
    expect(
      screen.getByText(
        'This member has neither a display name nor an email to type, so the role change cannot be confirmed here.',
      ),
    ).toBeInTheDocument();
  });
});

describe('MembersTab perimeter and invitation confirmations (issue #248)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.updateMember.mockResolvedValue({});
    mocks.createMember.mockResolvedValue({});
    viewerRef.current = { id: 'user-1' };
    invitationsRef.current = [];
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'owner', name: 'Alice' }),
      member({ id: 'm-2', user_id: 'user-2', role: 'member', name: 'Bob' }),
    ];
  });

  afterEach(() => {
    setDeliveryProfile(undefined);
  });

  function openPerimeterDialog(action: RegExp) {
    const triggers = menuTriggers();
    fireEvent.click(triggers[triggers.length - 1]!);
    fireEvent.click(screen.getByText(action));
  }

  it('granting says the seeding is not taken back by revoking', () => {
    setDeliveryProfile('perimeter');
    renderTab();
    openPerimeterDialog(/grant perimeter access/i);
    expect(screen.getByText(/is not undone by revoking access/i)).toBeInTheDocument();
  });

  it('revoking says nothing already delivered is withdrawn', () => {
    setDeliveryProfile('perimeter');
    membersRef.current = [
      member({ id: 'm-1', user_id: 'user-1', role: 'owner', name: 'Alice' }),
      member({
        id: 'm-2',
        user_id: 'user-2',
        role: 'member',
        name: 'Bob',
        perimeter_access: true,
      }),
    ];
    renderTab();
    openPerimeterDialog(/revoke perimeter access/i);
    expect(screen.getByText(/nothing already delivered is withdrawn/i)).toBeInTheDocument();
    expect(screen.queryByLabelText(/Type/)).toBeNull();
  });

  it('cancelling the perimeter dialog mutates nothing', () => {
    setDeliveryProfile('perimeter');
    renderTab();
    openPerimeterDialog(/grant perimeter access/i);
    clickCancel();
    expect(mocks.updateMember).not.toHaveBeenCalled();
  });

  it('does not send a second perimeter PATCH while the first is in flight', async () => {
    let release: (() => void) | undefined;
    mocks.updateMember.mockImplementation(
      () =>
        new Promise((resolve) => {
          release = () => resolve({});
        }),
    );
    setDeliveryProfile('perimeter');
    renderTab();
    openPerimeterDialog(/grant perimeter access/i);
    const confirm = screen.getByRole('button', { name: 'Confirm' });
    fireEvent.click(confirm);

    await waitFor(() => expect(confirm).toBeDisabled());
    fireEvent.click(confirm);
    expect(mocks.updateMember).toHaveBeenCalledTimes(1);
    release?.();
  });

  it('inviting an admin is confirmed before the invitation leaves', async () => {
    const user = userEvent.setup();
    renderTab();
    fireEvent.change(screen.getByLabelText('user@company.com'), {
      target: { value: 'dan@example.com' },
    });
    await user.click(screen.getByRole('combobox'));
    await user.click(await screen.findByRole('option', { name: 'Admin' }));
    await user.click(screen.getByRole('button', { name: 'Invite' }));

    expect(mocks.createMember).not.toHaveBeenCalled();
    expect(
      screen.getByText(/gateway address and key for the whole workspace/i),
    ).toBeInTheDocument();
    clickConfirm();
    await waitFor(() =>
      expect(mocks.createMember).toHaveBeenCalledWith('ws-1', {
        email: 'dan@example.com',
        role: 'admin',
      }),
    );
  });

  it('cancelling an admin invitation sends nothing', async () => {
    const user = userEvent.setup();
    renderTab();
    fireEvent.change(screen.getByLabelText('user@company.com'), {
      target: { value: 'dan@example.com' },
    });
    await user.click(screen.getByRole('combobox'));
    await user.click(await screen.findByRole('option', { name: 'Admin' }));
    await user.click(screen.getByRole('button', { name: 'Invite' }));
    clickCancel();
    expect(mocks.createMember).not.toHaveBeenCalled();
  });

  it('inviting a plain member goes straight out — the form is the act', async () => {
    renderTab();
    fireEvent.change(screen.getByLabelText('user@company.com'), {
      target: { value: 'erin@example.com' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Invite' }));
    await waitFor(() =>
      expect(mocks.createMember).toHaveBeenCalledWith('ws-1', {
        email: 'erin@example.com',
        role: 'member',
      }),
    );
  });
});
