import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ApiError } from '@goosar/core/api';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enOnboarding from '../../locales/en/onboarding.json';

const mocks = vi.hoisted(() => ({
  retryOnboardingCompletionDelivery: vi.fn(),
  listMyInvitations: vi.fn(),
  push: vi.fn(),
  logout: vi.fn(),
}));

vi.mock('@goosar/core/onboarding', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/onboarding')>()),
  retryOnboardingCompletionDelivery: mocks.retryOnboardingCompletionDelivery,
}));

vi.mock('@goosar/core/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/api')>();
  return {
    ...actual,
    api: { listMyInvitations: mocks.listMyInvitations },
  };
});

vi.mock('../../navigation', () => ({
  useNavigation: () => ({ push: mocks.push, replace: mocks.push }),
}));

vi.mock('../../auth', () => ({
  useLogout: () => mocks.logout,
}));

import { StepCompletionRecovery } from './step-completion-recovery';

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };
const COPY = enOnboarding.step_completion_recovery;

const INVITE = {
  id: 'inv-1',
  workspace_id: 'ws-1',
  workspace_name: 'Acme',
  inviter_name: 'Alice',
  role: 'member',
  status: 'pending',
};

function renderStep() {
  const onRecovered = vi.fn();
  const onStartOver = vi.fn();
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const tree = (next: () => void | Promise<void>) => (
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepCompletionRecovery onRecovered={next} onStartOver={onStartOver} />
      </I18nProvider>
    </QueryClientProvider>
  );
  const { unmount, rerender } = render(tree(onRecovered));
  const withExit = (next: () => void) => rerender(tree(next));
  return { onRecovered, onStartOver, unmount, withExit };
}

function deferred<T>() {
  let settle!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    settle = res;
  });
  return { promise, settle };
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listMyInvitations.mockResolvedValue([]);
});

describe('StepCompletionRecovery (issue #258)', () => {
  it('re-delivers once on mount and exits when the server confirms', async () => {
    mocks.retryOnboardingCompletionDelivery.mockResolvedValue('confirmed');

    const { onRecovered } = renderStep();

    await waitFor(() => expect(onRecovered).toHaveBeenCalledTimes(1));
    expect(mocks.retryOnboardingCompletionDelivery).toHaveBeenCalledTimes(1);
  });

  it('does not announce a failure before the first attempt is answered', async () => {
    const gate = deferred<string>();
    mocks.retryOnboardingCompletionDelivery.mockReturnValue(gate.promise);

    renderStep();

    expect(await screen.findByText(COPY.status_sending)).toBeInTheDocument();
    expect(screen.getByRole('heading').textContent).toBe(COPY.headline_sending);
    expect(screen.queryByText(COPY.headline)).toBeNull();

    gate.settle('confirmed');
  });

  it('shows the success as a state of its own while the exit is still resolving', async () => {
    const exitHangs = deferred<void>();
    mocks.retryOnboardingCompletionDelivery.mockResolvedValue('confirmed');

    const onRecovered = vi.fn(() => exitHangs.promise);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <I18nProvider locale="en" resources={TEST_RESOURCES}>
          <StepCompletionRecovery onRecovered={onRecovered} onStartOver={vi.fn()} />
        </I18nProvider>
      </QueryClientProvider>,
    );

    await screen.findByText(COPY.status_confirmed);
    expect(screen.getByRole('heading').textContent).toBe(COPY.headline_confirmed);
    expect(onRecovered).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('button', { name: COPY.retry })).toBeNull();
    expect(screen.queryByRole('button', { name: COPY.start_over })).toBeNull();
    expect(screen.queryByText(COPY.status_sending)).toBeNull();

    exitHangs.settle(undefined);
  });

  it('stays in a visible failure state with a retry button when delivery fails', async () => {
    mocks.retryOnboardingCompletionDelivery.mockRejectedValue(new Error('Failed to fetch'));

    const { onRecovered } = renderStep();

    await screen.findByText(COPY.status_unreachable);
    expect(onRecovered).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: COPY.retry })).toBeEnabled();
    expect(screen.getByRole('heading').textContent).toBe(COPY.headline);
  });

  it('keeps the raw transport text behind a disclosure, not in the prose', async () => {
    mocks.retryOnboardingCompletionDelivery.mockRejectedValue(new Error('Failed to fetch'));
    const user = userEvent.setup();

    renderStep();

    await screen.findByText(COPY.status_unreachable);
    expect(screen.queryByText('Failed to fetch')).toBeNull();

    await user.click(screen.getByRole('button', { name: COPY.details }));
    expect(await screen.findByText('Failed to fetch')).toBeInTheDocument();
  });

  describe('says which of the six things actually happened', () => {
    it('nothing answered → the connection, not the confirmation', async () => {
      mocks.retryOnboardingCompletionDelivery.mockRejectedValue(new TypeError('Failed to fetch'));

      renderStep();

      await screen.findByText(COPY.status_unreachable);
      expect(screen.queryByText(COPY.status_server_error)).toBeNull();
      expect(screen.queryByText(COPY.status_unconfirmed)).toBeNull();
    });

    it('the server answered and refused → the server, not the wire', async () => {
      mocks.retryOnboardingCompletionDelivery.mockRejectedValue(
        new ApiError(
          'API error: 500 Internal Server Error',
          500,
          'Internal Server Error',
          undefined,
          'onboarding is closed for maintenance',
        ),
      );
      const user = userEvent.setup();

      renderStep();

      await screen.findByText(COPY.status_server_error);
      expect(screen.queryByText(COPY.status_unreachable)).toBeNull();
      await user.click(screen.getByRole('button', { name: COPY.details }));
      expect(await screen.findByText('onboarding is closed for maintenance')).toBeInTheDocument();
    });

    it('the request landed and the flag did not → say exactly that', async () => {
      mocks.retryOnboardingCompletionDelivery.mockResolvedValue('not_confirmed');

      const { onRecovered } = renderStep();

      await screen.findByText(COPY.status_unconfirmed);
      expect(screen.queryByText(COPY.status_unreachable)).toBeNull();
      expect(onRecovered).not.toHaveBeenCalled();
    });

    it('delivered but unread is neither a failed delivery nor a refused one', async () => {
      mocks.retryOnboardingCompletionDelivery.mockResolvedValue('delivered_unverified');

      renderStep();

      await screen.findByText(COPY.status_delivered_unverified);
      expect(screen.queryByText(COPY.status_unreachable)).toBeNull();
      expect(screen.queryByText(COPY.status_unconfirmed)).toBeNull();
    });

    it('a fault on our side is not blamed on the server', async () => {
      mocks.retryOnboardingCompletionDelivery.mockRejectedValue(new Error('boom'));

      renderStep();

      await screen.findByText(COPY.status_unknown);
      expect(screen.queryByText(COPY.status_server_error)).toBeNull();
      expect(screen.queryByText(COPY.status_unreachable)).toBeNull();
    });

    it('an expired session is named as an expired session', async () => {
      mocks.retryOnboardingCompletionDelivery.mockRejectedValue(
        new ApiError('API error: 401 Unauthorized', 401, 'Unauthorized'),
      );

      renderStep();

      await screen.findByText(COPY.status_session_expired);
      expect(screen.queryByText(COPY.status_unreachable)).toBeNull();
      expect(screen.queryByText(COPY.status_server_error)).toBeNull();
    });
  });

  describe('an expired session has a way out of the screen', () => {
    it('offers a sign-out instead of a retry that can only 401', async () => {
      mocks.retryOnboardingCompletionDelivery.mockRejectedValue(
        new ApiError('API error: 401 Unauthorized', 401, 'Unauthorized'),
      );
      const user = userEvent.setup();

      renderStep();
      await screen.findByText(COPY.status_session_expired);

      expect(screen.queryByRole('button', { name: COPY.retry })).toBeNull();
      await user.click(screen.getByRole('button', { name: COPY.sign_out }));

      expect(mocks.logout).toHaveBeenCalledTimes(1);
    });

    it('keeps retry for the failures a retry can actually fix', async () => {
      mocks.retryOnboardingCompletionDelivery.mockRejectedValue(
        new ApiError('API error: 503 Service Unavailable', 503, 'Service Unavailable'),
      );

      renderStep();
      await screen.findByText(COPY.status_server_error);

      expect(screen.getByRole('button', { name: COPY.retry })).toBeEnabled();
      expect(screen.queryByRole('button', { name: COPY.sign_out })).toBeNull();
    });
  });

  describe('a waiting invitation stays reachable', () => {
    it('offers the invitations screen when the server reports one', async () => {
      mocks.listMyInvitations.mockResolvedValue([INVITE]);
      mocks.retryOnboardingCompletionDelivery.mockRejectedValue(new Error('Failed to fetch'));
      const user = userEvent.setup();

      renderStep();
      await screen.findByText(COPY.status_unreachable);

      expect(
        await screen.findByRole('button', { name: COPY.open_invitations }),
      ).toBeInTheDocument();
      await user.click(screen.getByRole('button', { name: COPY.open_invitations }));

      expect(mocks.push).toHaveBeenCalledWith('/invitations');
    });

    it('offers nothing when there is no invitation to accept', async () => {
      mocks.retryOnboardingCompletionDelivery.mockRejectedValue(new Error('Failed to fetch'));

      renderStep();
      await screen.findByText(COPY.status_unreachable);

      expect(screen.queryByText(COPY.invitation_notice)).toBeNull();
      expect(screen.queryByRole('button', { name: COPY.open_invitations })).toBeNull();
    });

    it('does not offer it before the attempt has failed', async () => {
      mocks.listMyInvitations.mockResolvedValue([INVITE]);
      const gate = deferred<string>();
      mocks.retryOnboardingCompletionDelivery.mockReturnValue(gate.promise);

      renderStep();
      await screen.findByText(COPY.status_sending);
      await waitFor(() => expect(mocks.listMyInvitations).toHaveBeenCalled());

      expect(screen.queryByRole('button', { name: COPY.open_invitations })).toBeNull();

      gate.settle('confirmed');
    });
  });

  it('retries only on demand after the first automatic attempt', async () => {
    mocks.retryOnboardingCompletionDelivery.mockRejectedValue(
      new ApiError('API error: 503 Service Unavailable', 503, 'Service Unavailable'),
    );
    const user = userEvent.setup();
    renderStep();
    await screen.findByText(COPY.status_server_error);
    expect(mocks.retryOnboardingCompletionDelivery).toHaveBeenCalledTimes(1);

    await user.click(screen.getByRole('button', { name: COPY.retry }));

    await waitFor(() => expect(mocks.retryOnboardingCompletionDelivery).toHaveBeenCalledTimes(2));
    await screen.findByText(COPY.status_server_error);
  });

  it('offers the full scenario as an explicit second choice', async () => {
    mocks.retryOnboardingCompletionDelivery.mockRejectedValue(
      new ApiError('API error: 503 Service Unavailable', 503, 'Service Unavailable'),
    );
    const user = userEvent.setup();
    const { onStartOver } = renderStep();
    await screen.findByText(COPY.status_server_error);

    await user.click(screen.getByRole('button', { name: COPY.start_over }));

    expect(onStartOver).toHaveBeenCalledTimes(1);
  });

  it('exits through the current callback, not the one from the first render', async () => {
    const gate = deferred<string>();
    mocks.retryOnboardingCompletionDelivery.mockReturnValue(gate.promise);
    const { onRecovered, withExit } = renderStep();
    await screen.findByText(COPY.status_sending);

    const laterExit = vi.fn();
    withExit(laterExit);
    gate.settle('confirmed');

    await waitFor(() => expect(laterExit).toHaveBeenCalledTimes(1));
    expect(onRecovered).not.toHaveBeenCalled();
  });

  it('a late answer cannot yank the user out of the screen they left', async () => {
    const gate = deferred<string>();
    mocks.retryOnboardingCompletionDelivery.mockReturnValue(gate.promise);
    const user = userEvent.setup();
    const { onRecovered, onStartOver, unmount } = renderStep();
    await screen.findByText(COPY.status_sending);

    await user.click(screen.getByRole('button', { name: COPY.start_over }));
    expect(onStartOver).toHaveBeenCalledTimes(1);
    unmount();

    gate.settle('confirmed');
    await Promise.resolve();
    await Promise.resolve();

    expect(onRecovered).not.toHaveBeenCalled();
  });
});
