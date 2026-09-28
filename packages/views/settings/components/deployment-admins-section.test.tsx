import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { ApiError } from '@goosar/core/api';
import { renderWithI18n } from '../../test/i18n';

const mocks = vi.hoisted(() => ({
  addAdmin: vi.fn(),
  removeAdmin: vi.fn(),
  addPending: false,
  removePending: false,
}));

vi.mock('@goosar/core/deployment/admin', () => ({
  useAddDeploymentAdmin: () => ({
    mutateAsync: mocks.addAdmin,
    isPending: mocks.addPending,
  }),
  useRemoveDeploymentAdmin: () => ({
    mutateAsync: mocks.removeAdmin,
    isPending: mocks.removePending,
  }),
}));

import { DeploymentAdminsSection } from './deployment-admins-section';

const ADMINS = [
  { user_id: 'user-1', email: 'root@corp.example', name: 'Root' },
  { user_id: 'user-2', email: 'dev@corp.example', name: 'Dev' },
];

const REVOKE_PENDING = {
  status: 'pending',
  request_id: 'req-1',
  action: 'revoke' as const,
  target_user_id: 'user-2',
  target_email: undefined,
  confirm_hint: 'goosar_admin confirm req-1',
};

const GRANT_PENDING = {
  status: 'pending',
  request_id: 'req-2',
  action: 'grant' as const,
  target_user_id: '',
  target_email: 'new@corp.example',
  confirm_hint: 'goosar_admin confirm req-2',
};

/** Opens the row overflow menu for `name` and clicks "Request revoke". */
async function requestRemove(name: string) {
  fireEvent.click(screen.getByLabelText(`Actions for ${name}`));
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Request revoke' }));
}

/** Confirms the destructive revoke dialog that requestRemove() opened. */
function confirmRemoveDialog() {
  fireEvent.click(screen.getAllByRole('button', { name: 'Request revoke' }).at(-1)!);
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.addPending = false;
  mocks.removePending = false;
  mocks.addAdmin.mockResolvedValue({ kind: 'pending', pending: GRANT_PENDING });
  mocks.removeAdmin.mockResolvedValue(REVOKE_PENDING);
});

describe('DeploymentAdminsSection remove', () => {
  it('files a pending revoke and keeps the row: nothing changes until the server confirm', async () => {
    renderWithI18n(<DeploymentAdminsSection admins={ADMINS} pending={[]} />);

    await requestRemove('dev@corp.example');
    expect(
      screen.getByText('Request revoking the role from dev@corp.example?'),
    ).toBeTruthy();
    confirmRemoveDialog();

    await waitFor(() => expect(mocks.removeAdmin).toHaveBeenCalledTimes(1));
    expect(mocks.removeAdmin).toHaveBeenCalledWith('user-2');

    expect(screen.getByText('dev@corp.example')).toBeTruthy();
    expect(screen.queryByText('Pending')).toBeNull();
  });

  it("surfaces the server's refusal (last-admin lockout) as an inline notice with the response text", async () => {
    mocks.removeAdmin.mockRejectedValue(
      new ApiError(
        'API error: 409 Conflict',
        409,
        'Conflict',
        undefined,
        'cannot revoke the last deployment admin',
      ),
    );
    renderWithI18n(<DeploymentAdminsSection admins={ADMINS} pending={[]} />);

    await requestRemove('root@corp.example');
    confirmRemoveDialog();

    const notice = await screen.findByRole('alert');
    expect(notice.textContent).toContain('cannot revoke the last deployment admin');
    expect(screen.queryByText('Pending')).toBeNull();
  });

  it('disables the revoke action in the row menu while a revoke is in flight', async () => {
    mocks.removePending = true;
    renderWithI18n(<DeploymentAdminsSection admins={ADMINS} pending={[]} />);

    fireEvent.click(screen.getByLabelText('Actions for dev@corp.example'));
    const remove = (await screen.findByRole('menuitem', {
      name: 'Request revoke',
    })) as HTMLElement;
    expect(remove.getAttribute('data-disabled')).not.toBeNull();
  });
});

describe('DeploymentAdminsSection add', () => {
  it('files a pending grant and clears the input', async () => {
    renderWithI18n(<DeploymentAdminsSection admins={ADMINS} pending={[]} />);

    const email = screen.getByLabelText(
      'Email of the user to grant the role to',
    ) as HTMLInputElement;
    fireEvent.change(email, { target: { value: 'new@corp.example' } });
    fireEvent.click(screen.getByRole('button', { name: 'Request grant' }));

    await waitFor(() => expect(mocks.addAdmin).toHaveBeenCalledTimes(1));
    expect(mocks.addAdmin).toHaveBeenCalledWith('new@corp.example');
    expect(email.value).toBe('');
  });

  it('tells the admin when the target already holds the role', async () => {
    mocks.addAdmin.mockResolvedValue({
      kind: 'already_admin',
      entry: ADMINS[1],
    });
    renderWithI18n(<DeploymentAdminsSection admins={ADMINS} pending={[]} />);

    fireEvent.change(screen.getByLabelText('Email of the user to grant the role to'), {
      target: { value: 'dev@corp.example' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Request grant' }));

    const notice = await screen.findByRole('alert');
    expect(notice.textContent).toContain('dev@corp.example already holds the role');
    expect(screen.queryByText('Pending')).toBeNull();
  });

  it('does not submit an empty email and disables the button', () => {
    renderWithI18n(<DeploymentAdminsSection admins={ADMINS} pending={[]} />);

    const add = screen.getByRole('button', {
      name: 'Request grant',
    }) as HTMLButtonElement;
    expect(add.disabled).toBe(true);

    fireEvent.change(screen.getByLabelText('Email of the user to grant the role to'), {
      target: { value: '   ' },
    });
    expect(add.disabled).toBe(true);
    fireEvent.click(add);
    expect(mocks.addAdmin).not.toHaveBeenCalled();
  });

  it('disables the add button while a grant is in flight', () => {
    mocks.addPending = true;
    renderWithI18n(<DeploymentAdminsSection admins={ADMINS} pending={[]} />);

    fireEvent.change(screen.getByLabelText('Email of the user to grant the role to'), {
      target: { value: 'new@corp.example' },
    });
    const add = screen.getByRole('button', {
      name: 'Request grant',
    }) as HTMLButtonElement;
    expect(add.disabled).toBe(true);
    fireEvent.click(add);
    expect(mocks.addAdmin).not.toHaveBeenCalled();
  });

  it('surfaces an add failure as an inline notice and clears it on the next attempt', async () => {
    mocks.addAdmin.mockRejectedValueOnce(
      new ApiError(
        'API error: 404 Not Found',
        404,
        'Not Found',
        undefined,
        'no user with this email',
      ),
    );
    renderWithI18n(<DeploymentAdminsSection admins={ADMINS} pending={[]} />);

    const email = screen.getByLabelText('Email of the user to grant the role to');
    fireEvent.change(email, { target: { value: 'nobody@corp.example' } });
    fireEvent.click(screen.getByRole('button', { name: 'Request grant' }));

    const notice = await screen.findByRole('alert');
    expect(notice.textContent).toContain('no user with this email');
    expect((email as HTMLInputElement).value).toBe('nobody@corp.example');

    fireEvent.change(email, { target: { value: 'new@corp.example' } });
    fireEvent.click(screen.getByRole('button', { name: 'Request grant' }));
    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull());
  });
});

describe('DeploymentAdminsSection pending queue', () => {
  it("renders the server's filed requests with their confirm hints verbatim", () => {
    renderWithI18n(
      <DeploymentAdminsSection admins={ADMINS} pending={[REVOKE_PENDING, GRANT_PENDING]} />,
    );

    expect(screen.getAllByText('Pending')).toHaveLength(2);
    expect(screen.getByText('Revoke the role from user-2')).toBeTruthy();
    expect(screen.getByText('Grant the role to new@corp.example')).toBeTruthy();
    expect(screen.getByText('goosar_admin confirm req-1')).toBeTruthy();
    expect(screen.getByText('goosar_admin confirm req-2')).toBeTruthy();
  });

  it('renders an unknown action as a grant instead of dropping the row', () => {
    renderWithI18n(
      <DeploymentAdminsSection
        admins={ADMINS}
        pending={[
          {
            ...GRANT_PENDING,
            action: 'some_future_action',
          },
        ]}
      />,
    );
    expect(screen.getByText('Pending')).toBeTruthy();
    expect(screen.getByText('goosar_admin confirm req-2')).toBeTruthy();
  });
});

describe('DeploymentAdminsSection list', () => {
  it('renders the empty state when there are no admins', () => {
    renderWithI18n(<DeploymentAdminsSection admins={[]} pending={[]} />);
    expect(screen.getByText('No deployment administrators.')).toBeTruthy();
  });

  it('filters the table by the search box', () => {
    renderWithI18n(<DeploymentAdminsSection admins={ADMINS} pending={[]} />);
    fireEvent.change(screen.getByLabelText('Search administrators'), {
      target: { value: 'dev@' },
    });
    expect(screen.getByText('dev@corp.example')).toBeTruthy();
    expect(screen.queryByText('root@corp.example')).toBeNull();
  });
});
