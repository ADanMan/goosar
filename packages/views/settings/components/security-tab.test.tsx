// @vitest-environment jsdom

import { describe, expect, it, vi, beforeEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enSettings from '../../locales/en/settings.json';

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

vi.mock('react-qr-code', () => ({
  default: ({ value }: { value: string }) => <div data-testid="qr" data-value={value} />,
}));

const mockGetMFAStatus = vi.hoisted(() => vi.fn());
const mockEnrollTOTP = vi.hoisted(() => vi.fn());
const mockConfirmTOTP = vi.hoisted(() => vi.fn());
const mockDisableTOTP = vi.hoisted(() => vi.fn());
const mockRegenerate = vi.hoisted(() => vi.fn());
const mockListSessions = vi.hoisted(() => vi.fn());
const mockRevokeSession = vi.hoisted(() => vi.fn());
const mockRevokeAll = vi.hoisted(() => vi.fn());

vi.mock('@goosar/core/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/api')>();
  return {
    ...actual,
    api: {
      getMFAStatus: mockGetMFAStatus,
      enrollTOTP: mockEnrollTOTP,
      confirmTOTP: mockConfirmTOTP,
      disableTOTP: mockDisableTOTP,
      regenerateRecoveryCodes: mockRegenerate,
      listSessions: mockListSessions,
      revokeSession: mockRevokeSession,
      revokeAllSessions: mockRevokeAll,
    },
  };
});

import { SecurityTab } from './security-tab';

function renderTab() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <SecurityTab />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

const NO_FACTOR = {
  enabled: false,
  pending_enrollment: false,
  enabled_at: null,
  recovery_codes_remaining: 0,
  required: false,
  available: true,
};

describe('SecurityTab', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetMFAStatus.mockResolvedValue(NO_FACTOR);
    mockListSessions.mockResolvedValue([]);
  });

  it('walks enrollment: QR, code, and the one-time recovery codes', async () => {
    mockEnrollTOTP.mockResolvedValue({
      secret: 'JBSWY3DPEHPK3PXP',
      otpauth_uri: 'otpauth://totp/Hermes:me@example.com?secret=JBSWY3DPEHPK3PXP',
      issuer: 'Hermes',
      account: 'me@example.com',
      digits: 6,
      period_seconds: 30,
      algorithm: 'SHA1',
    });
    mockConfirmTOTP.mockResolvedValue({
      enabled: true,
      recovery_codes: ['AAAABBBB', 'CCCCDDDD'],
    });

    renderTab();
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: /set up two-step/i }));

    const qr = await screen.findByTestId('qr');
    expect(qr.getAttribute('data-value')).toContain('otpauth://totp/');
    expect(screen.getByText('JBSWY3DPEHPK3PXP')).toBeInTheDocument();

    await user.type(screen.getByLabelText(/code from the app/i), '123456');
    await user.click(screen.getByRole('button', { name: /^activate$/i }));

    await waitFor(() => {
      expect(mockConfirmTOTP).toHaveBeenCalledWith('123456');
    });
    expect(await screen.findByText('AAAABBBB')).toBeInTheDocument();
    expect(screen.getByText('CCCCDDDD')).toBeInTheDocument();
  });

  it("shows the server's own refusal when the confirmation code is wrong", async () => {
    mockEnrollTOTP.mockResolvedValue({
      secret: 'JBSWY3DPEHPK3PXP',
      otpauth_uri: 'otpauth://totp/x?secret=JBSWY3DPEHPK3PXP',
      issuer: '',
      account: '',
      digits: 6,
      period_seconds: 30,
      algorithm: 'SHA1',
    });
    mockConfirmTOTP.mockRejectedValue(
      Object.assign(new Error('that code is not valid'), {
        body: { code: 'mfa_invalid_code' },
      }),
    );

    renderTab();
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: /set up two-step/i }));
    await user.type(await screen.findByLabelText(/code from the app/i), '000000');
    await user.click(screen.getByRole('button', { name: /^activate$/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/that code is wrong/i);
  });

  it('requires a current code before turning the factor off', async () => {
    mockGetMFAStatus.mockResolvedValue({
      ...NO_FACTOR,
      enabled: true,
      recovery_codes_remaining: 9,
    });
    mockDisableTOTP.mockResolvedValue(undefined);

    renderTab();
    const user = userEvent.setup();

    const off = await screen.findByRole('button', { name: /turn off/i });
    expect(off).toBeDisabled();

    await user.type(screen.getByLabelText(/current code/i), '654321');
    await user.click(screen.getByRole('button', { name: /turn off/i }));
    await waitFor(() => expect(mockDisableTOTP).toHaveBeenCalledWith('654321'));
  });

  it('renders the unavailable explanation instead of a broken button', async () => {
    mockGetMFAStatus.mockResolvedValue({ ...NO_FACTOR, available: false });
    renderTab();
    expect(await screen.findByRole('status')).toHaveTextContent(/cannot store a second factor/i);
    expect(await screen.findByRole('button', { name: /set up two-step/i })).toBeDisabled();
  });

  it('lists sessions, marks this device, and offers to end the others', async () => {
    mockListSessions.mockResolvedValue([
      {
        id: 's1',
        user_agent: 'Hermes Desktop/1.0',
        created_at: '2026-01-01T00:00:00Z',
        last_seen_at: '2026-01-02T00:00:00Z',
        current: true,
      },
      {
        id: 's2',
        user_agent: 'Mozilla/5.0 (Windows)',
        created_at: '2026-01-01T00:00:00Z',
        last_seen_at: '2026-01-02T00:00:00Z',
        current: false,
      },
    ]);
    mockRevokeSession.mockResolvedValue(undefined);

    renderTab();
    const user = userEvent.setup();

    expect(await screen.findByText(/Hermes Desktop\/1\.0/)).toBeInTheDocument();
    const signOutButtons = await screen.findAllByRole('button', { name: /^sign out$/i });
    expect(signOutButtons).toHaveLength(1);

    await user.click(signOutButtons[0]!);
    await waitFor(() => expect(mockRevokeSession).toHaveBeenCalledWith('s2'));
  });

  it('renders an empty device list without failing', async () => {
    mockListSessions.mockResolvedValue([]);
    renderTab();
    expect(await screen.findByText(/no other devices are signed in/i)).toBeInTheDocument();
  });
});
