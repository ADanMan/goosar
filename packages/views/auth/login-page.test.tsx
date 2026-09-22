import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactElement, ReactNode } from 'react';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../locales/en/common.json';
import enAuth from '../locales/en/auth.json';
import enSettings from '../locales/en/settings.json';

const TEST_RESOURCES = {
  en: { common: enCommon, auth: enAuth, settings: enSettings },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderWithI18n(ui: ReactElement) {
  return render(ui, { wrapper: I18nWrapper });
}

const mockSendCode = vi.hoisted(() => vi.fn());
const mockVerifyCode = vi.hoisted(() => vi.fn());
const mockLoginWithToken = vi.hoisted(() => vi.fn());
const mockApiListWorkspaces = vi.hoisted(() => vi.fn());
const mockApiVerifyCode = vi.hoisted(() => vi.fn());
const mockApiSetToken = vi.hoisted(() => vi.fn());
const mockApiGetMe = vi.hoisted(() => vi.fn());
const mockApiIssueCliToken = vi.hoisted(() => vi.fn());
const mockApiGetAuthMethods = vi.hoisted(() =>
  vi.fn(async () => ({
    methods: ['email'],
    oidc_display_name: '',
    ldap_display_name: '',
  })),
);
const mockApiOidcStartURL = vi.hoisted(() =>
  vi.fn(() => 'https://goosar.example.test/api/auth/oidc/start'),
);
const mockApiLdapLogin = vi.hoisted(() => vi.fn());
const mockApiVerifyMFA = vi.hoisted(() => vi.fn());
const mockSetQueryData = vi.hoisted(() => vi.fn());

vi.mock('@tanstack/react-query', async () => {
  const actual =
    await vi.importActual<typeof import('@tanstack/react-query')>('@tanstack/react-query');
  return { ...actual, useQueryClient: () => ({ setQueryData: mockSetQueryData }) };
});

vi.mock('@goosar/core/auth', () => ({
  useAuthStore: Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = {
        sendCode: mockSendCode,
        verifyCode: mockVerifyCode,
        loginWithToken: mockLoginWithToken,
      };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({
        sendCode: mockSendCode,
        verifyCode: mockVerifyCode,
        loginWithToken: mockLoginWithToken,
      }),
    },
  ),
}));

vi.mock('@goosar/core/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/api')>();
  return {
    ...actual,
    api: {
      listWorkspaces: mockApiListWorkspaces,
      verifyCode: mockApiVerifyCode,
      setToken: mockApiSetToken,
      getMe: mockApiGetMe,
      issueCliToken: mockApiIssueCliToken,
      getAuthMethods: mockApiGetAuthMethods,
      oidcStartURL: mockApiOidcStartURL,
      ldapLogin: mockApiLdapLogin,
      verifyMFA: mockApiVerifyMFA,
    },
  };
});

vi.mock('@goosar/core/types', () => ({}));

import { ApiError } from '@goosar/core/api';
import { configStore } from '@goosar/core/config';
import { LoginPage, validateCliCallback } from './login-page';

function serverRejection(message: string, status = 400) {
  return new ApiError(message, status, 'Bad Request', { error: message }, message);
}

function getOTPInput() {
  return screen.getByRole('textbox', { hidden: true });
}

describe('LoginPage', () => {
  const onSuccess = vi.fn();

  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.clearAllMocks();
    mockApiGetMe.mockRejectedValue(new Error('unauthorized'));
    localStorage.clear();
    Object.defineProperty(window, 'location', {
      writable: true,
      value: { href: 'http://localhost:3000' },
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders email form with 'Sign in to Goosar' title", () => {
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);
    expect(screen.getByText(/sign in to goosar/i)).toBeInTheDocument();
    expect(screen.getByText(/enter your email to get a login code/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /continue/i })).toBeInTheDocument();
  });

  it('shows error when submitting with empty email', async () => {
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const emailInput = screen.getByLabelText(/email/i);
    const button = screen.getByRole('button', { name: /continue/i });
    expect(button).toBeDisabled();

    const user = userEvent.setup();
    await user.type(emailInput, 'a');
    expect(button).not.toBeDisabled();
    await user.clear(emailInput);
    expect(button).toBeDisabled();
  });

  it('calls sendCode on form submit with email', async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    expect(mockSendCode).toHaveBeenCalledWith('test@example.com');
  });

  it("shows 'Sending code...' while submitting", async () => {
    mockSendCode.mockReturnValueOnce(new Promise(() => {}));
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    expect(screen.getByText(/sending code/i)).toBeInTheDocument();
  });

  it('transitions to code step after successful sendCode', async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/test@example.com/)).toBeInTheDocument();
  });

  it('autofocuses the OTP input when the code step opens', async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });

    expect(getOTPInput()).toHaveFocus();
  });

  it('shows error when sendCode fails', async () => {
    mockSendCode.mockRejectedValueOnce(serverRejection('Rate limited'));
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText('Rate limited')).toBeInTheDocument();
    });
  });

  it('does not headline the synthesized HTTP status line', async () => {
    mockSendCode.mockRejectedValueOnce(
      new ApiError('API error: 500 Internal Server Error', 500, 'Internal Server Error'),
    );
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/failed to send code/i)).toBeInTheDocument();
    });
    expect(screen.queryByText('API error: 500 Internal Server Error')).not.toBeInTheDocument();
  });

  it('keeps the protocol text reachable behind the details disclosure', async () => {
    mockSendCode.mockRejectedValueOnce(
      new ApiError('API error: 500 Internal Server Error', 500, 'Internal Server Error'),
    );
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    const details = await screen.findByText(enAuth.errors.details);
    await user.click(details);

    expect(await screen.findByText('API error: 500 Internal Server Error')).toBeInTheDocument();
  });

  it("names an unreachable server instead of echoing 'Failed to fetch'", async () => {
    mockSendCode.mockRejectedValueOnce(new TypeError('Failed to fetch'));
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(enAuth.errors.unreachable)).toBeInTheDocument();
    });
    expect(screen.queryByText('Failed to fetch')).not.toBeInTheDocument();
  });

  it('shows generic error when sendCode throws non-Error', async () => {
    mockSendCode.mockRejectedValueOnce('boom');
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/failed to send code/i)).toBeInTheDocument();
    });
  });

  it('lets describeError replace the sendCode failure message', async () => {
    mockSendCode.mockRejectedValueOnce(new TypeError('Failed to fetch'));
    renderWithI18n(
      <LoginPage
        onSuccess={onSuccess}
        describeError={() => 'Cannot reach the server at https://goosar.acme.test'}
      />,
    );

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(
        screen.getByText('Cannot reach the server at https://goosar.acme.test'),
      ).toBeInTheDocument();
    });
    expect(screen.queryByText('Failed to fetch')).not.toBeInTheDocument();
  });

  it('keeps the default message when describeError returns undefined', async () => {
    const describeError = vi.fn().mockReturnValue(undefined);
    mockSendCode.mockRejectedValueOnce(serverRejection('Rate limited'));
    renderWithI18n(<LoginPage onSuccess={onSuccess} describeError={describeError} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText('Rate limited')).toBeInTheDocument();
    });
    expect(describeError).toHaveBeenCalledWith(expect.any(Error));
  });

  it('re-reads the platform description when the platform learns more', async () => {
    mockSendCode.mockRejectedValueOnce(new TypeError('Failed to fetch'));
    const { rerender } = renderWithI18n(
      <LoginPage onSuccess={onSuccess} describeError={() => 'Route unknown'} />,
    );

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));
    await screen.findByText('Route unknown');

    rerender(
      <LoginPage onSuccess={onSuccess} describeError={() => 'Routed through proxy.acme.test'} />,
    );

    expect(await screen.findByText('Routed through proxy.acme.test')).toBeInTheDocument();
    expect(screen.queryByText('Route unknown')).not.toBeInTheDocument();
  });

  it('tells the platform that a sign-in attempt failed', async () => {
    const failure = new TypeError('Failed to fetch');
    const onError = vi.fn();
    mockSendCode.mockRejectedValueOnce(failure);
    renderWithI18n(<LoginPage onSuccess={onSuccess} onError={onError} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => expect(onError).toHaveBeenCalledWith(failure));
  });

  it('does not report a failure when sign-in succeeds', async () => {
    const onError = vi.fn();
    mockSendCode.mockResolvedValueOnce(undefined);
    renderWithI18n(<LoginPage onSuccess={onSuccess} onError={onError} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await screen.findByText(/check your email/i);
    expect(onError).not.toHaveBeenCalled();
  });

  it('calls verifyCode, seeds workspace list cache, then onSuccess', async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    mockApiVerifyCode.mockResolvedValueOnce({ token: 'jwt', mfa_required: false });
    mockApiListWorkspaces.mockResolvedValueOnce([{ id: 'ws-1' }]);

    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });

    const otpInput = getOTPInput();
    await user.type(otpInput, '123456');

    await waitFor(() => {
      expect(mockApiVerifyCode).toHaveBeenCalledWith('test@example.com', '123456');
      expect(mockApiListWorkspaces).toHaveBeenCalled();
      expect(mockSetQueryData).toHaveBeenCalledWith(
        expect.arrayContaining(['workspaces', 'list']),
        [{ id: 'ws-1' }],
      );
      expect(onSuccess).toHaveBeenCalled();
    });
  });

  it('shows error on invalid code', async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    mockApiVerifyCode.mockRejectedValueOnce(serverRejection('Invalid code'));

    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });

    const otpInput = getOTPInput();
    await user.type(otpInput, '000000');

    await waitFor(() => {
      expect(screen.getByText('Invalid code')).toBeInTheDocument();
    });
    expect(onSuccess).not.toHaveBeenCalled();
  });

  it('disables resend button during cooldown', async () => {
    mockSendCode.mockResolvedValue(undefined);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });

    const resendBtn = screen.getByRole('button', { name: /resend in/i });
    expect(resendBtn).toBeDisabled();
  });

  it('shows resend button with cooldown text after sending code', async () => {
    mockSendCode.mockResolvedValue(undefined);
    const user = userEvent.setup();
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });

    expect(screen.getByText(/resend in/i)).toBeInTheDocument();
  });

  it('calls sendCode again when resend is clicked after cooldown', async () => {
    mockSendCode.mockResolvedValue(undefined);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });

    expect(mockSendCode).toHaveBeenCalledTimes(1);

    for (let i = 0; i < 61; i++) {
      await act(async () => {
        vi.advanceTimersByTime(1_000);
      });
    }

    await waitFor(() => {
      expect(screen.getByText(/resend code/i)).toBeInTheDocument();
    });

    const resendBtn = screen.getByRole('button', { name: /resend code/i });
    expect(resendBtn).not.toBeDisabled();

    await user.click(resendBtn);
    expect(mockSendCode).toHaveBeenCalledTimes(2);
  });

  it('shows cli_confirm step when existing session + cliCallback', async () => {
    localStorage.setItem('goosar_token', 'existing-jwt');
    mockApiGetMe.mockRejectedValueOnce(new Error('no cookie')).mockResolvedValueOnce({
      id: 'u-1',
      email: 'user@example.com',
      name: 'Test User',
    });

    render(
      <LoginPage
        onSuccess={onSuccess}
        cliCallback={{ url: 'http://localhost:9876/callback', state: 'abc' }}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText(/authorize cli/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/user@example.com/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /authorize/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /use a different account/i })).toBeInTheDocument();
  });

  it('CLI authorize button redirects to callback URL', async () => {
    localStorage.setItem('goosar_token', 'existing-jwt');
    mockApiGetMe.mockRejectedValueOnce(new Error('no cookie')).mockResolvedValueOnce({
      id: 'u-1',
      email: 'user@example.com',
      name: 'Test User',
    });
    const onTokenObtained = vi.fn();

    render(
      <LoginPage
        onSuccess={onSuccess}
        onTokenObtained={onTokenObtained}
        cliCallback={{ url: 'http://localhost:9876/callback', state: 'abc' }}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText(/authorize cli/i)).toBeInTheDocument();
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /^authorize$/i }));

    expect(onTokenObtained).toHaveBeenCalled();
    expect(window.location.href).toContain(
      'http://localhost:9876/callback?token=existing-jwt&state=abc',
    );
  });

  it("'Use a different account' returns to email step", async () => {
    localStorage.setItem('goosar_token', 'existing-jwt');
    mockApiGetMe.mockRejectedValueOnce(new Error('no cookie')).mockResolvedValueOnce({
      id: 'u-1',
      email: 'user@example.com',
      name: 'Test User',
    });

    render(
      <LoginPage
        onSuccess={onSuccess}
        cliCallback={{ url: 'http://localhost:9876/callback', state: 'abc' }}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText(/authorize cli/i)).toBeInTheDocument();
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /use a different account/i }));

    expect(screen.getByText(/sign in to goosar/i)).toBeInTheDocument();
  });

  it('detects cookie-based session and shows cli_confirm when no localStorage token', async () => {
    mockApiGetMe.mockResolvedValueOnce({
      id: 'u-1',
      email: 'cookie@example.com',
      name: 'Cookie User',
    });

    render(
      <LoginPage
        onSuccess={onSuccess}
        cliCallback={{ url: 'http://localhost:9876/callback', state: 'abc' }}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText(/authorize cli/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/cookie@example.com/)).toBeInTheDocument();
  });

  it('CLI authorize with cookie session calls issueCliToken and redirects', async () => {
    mockApiGetMe.mockResolvedValueOnce({
      id: 'u-1',
      email: 'cookie@example.com',
      name: 'Cookie User',
    });
    mockApiIssueCliToken.mockResolvedValueOnce({ token: 'fresh-jwt' });
    const onTokenObtained = vi.fn();

    render(
      <LoginPage
        onSuccess={onSuccess}
        onTokenObtained={onTokenObtained}
        cliCallback={{ url: 'http://localhost:9876/callback', state: 'abc' }}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText(/authorize cli/i)).toBeInTheDocument();
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /^authorize$/i }));

    await waitFor(() => {
      expect(mockApiIssueCliToken).toHaveBeenCalled();
      expect(onTokenObtained).toHaveBeenCalled();
      expect(window.location.href).toContain(
        'http://localhost:9876/callback?token=fresh-jwt&state=abc',
      );
    });
  });

  it('CLI code verification redirects to callback URL', async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    mockApiVerifyCode.mockResolvedValueOnce({ token: 'new-jwt-token' });
    const onTokenObtained = vi.fn();

    render(
      <LoginPage
        onSuccess={onSuccess}
        onTokenObtained={onTokenObtained}
        cliCallback={{ url: 'http://localhost:9876/callback', state: 'xyz' }}
      />,
    );

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'cli@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });

    const otpInput = getOTPInput();
    await user.type(otpInput, '654321');

    await waitFor(() => {
      expect(mockApiVerifyCode).toHaveBeenCalledWith('cli@example.com', '654321');
      expect(onTokenObtained).toHaveBeenCalled();
      expect(window.location.href).toContain(
        'http://localhost:9876/callback?token=new-jwt-token&state=xyz',
      );
    });

    expect(mockVerifyCode).not.toHaveBeenCalled();
    expect(onSuccess).not.toHaveBeenCalled();
  });

  it('renders logo when provided', () => {
    render(<LoginPage onSuccess={onSuccess} logo={<div data-testid="custom-logo">Logo</div>} />);
    expect(screen.getByTestId('custom-logo')).toBeInTheDocument();
  });

  it('does not render logo placeholder when omitted', () => {
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);
    expect(screen.queryByTestId('custom-logo')).not.toBeInTheDocument();
  });

  it('calls onTokenObtained after successful verification', async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    mockApiVerifyCode.mockResolvedValueOnce({ token: 'jwt', mfa_required: false });
    mockApiListWorkspaces.mockResolvedValueOnce([{ id: 'ws-1' }]);
    const onTokenObtained = vi.fn();

    render(<LoginPage onSuccess={onSuccess} onTokenObtained={onTokenObtained} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });

    const otpInput = getOTPInput();
    await user.type(otpInput, '123456');

    await waitFor(() => {
      expect(onTokenObtained).toHaveBeenCalled();
      expect(onSuccess).toHaveBeenCalled();
    });
  });

  it('back button returns to email step', async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), 'test@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /back/i }));

    expect(screen.getByText(/sign in to goosar/i)).toBeInTheDocument();
  });
});

describe('validateCliCallback', () => {
  it('accepts http://localhost', () => {
    expect(validateCliCallback('http://localhost:9876/callback')).toBe(true);
  });

  it('accepts http://127.0.0.1', () => {
    expect(validateCliCallback('http://127.0.0.1:8080/cb')).toBe(true);
  });

  it('accepts 10.x.x.x private IPs', () => {
    expect(validateCliCallback('http://10.0.0.5:9876/callback')).toBe(true);
    expect(validateCliCallback('http://10.255.255.255:1234/cb')).toBe(true);
  });

  it('accepts 172.16-31.x.x private IPs', () => {
    expect(validateCliCallback('http://172.16.0.1:9876/callback')).toBe(true);
    expect(validateCliCallback('http://172.31.255.255:1234/cb')).toBe(true);
  });

  it('rejects 172.x outside 16-31 range', () => {
    expect(validateCliCallback('http://172.15.0.1:9876/callback')).toBe(false);
    expect(validateCliCallback('http://172.32.0.1:9876/callback')).toBe(false);
  });

  it('accepts 192.168.x.x private IPs', () => {
    expect(validateCliCallback('http://192.168.1.131:41117/callback')).toBe(true);
    expect(validateCliCallback('http://192.168.0.1:8080/cb')).toBe(true);
  });

  it('rejects https:// URLs', () => {
    expect(validateCliCallback('https://localhost:9876/callback')).toBe(false);
  });

  it('rejects public IPs and domains', () => {
    expect(validateCliCallback('http://evil.com:9876/callback')).toBe(false);
    expect(validateCliCallback('http://8.8.8.8:9876/callback')).toBe(false);
    expect(validateCliCallback('http://192.169.1.1:9876/callback')).toBe(false);
  });

  it('rejects invalid URLs', () => {
    expect(validateCliCallback('not-a-url')).toBe(false);
  });
});

describe('LoginPage mail transport notice', () => {
  const onSuccess = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    mockApiGetMe.mockRejectedValue(new Error('unauthorized'));
    configStore.getState().setEmailTransport('');
  });

  afterEach(() => {
    configStore.getState().setEmailTransport('');
  });

  it('says nothing when the server delivers mail', () => {
    configStore.getState().setEmailTransport('smtp');
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);
    expect(screen.queryByText(/not configured/i)).not.toBeInTheDocument();
  });

  it('says nothing on a server too old to report the transport', () => {
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);
    expect(screen.queryByText(/not configured/i)).not.toBeInTheDocument();
  });

  it('warns that codes go to the server log on the dev transport', () => {
    configStore.getState().setEmailTransport('dev');
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);
    expect(screen.getByText(/printed to the server log/i)).toBeInTheDocument();
  });

  it('points at the administrator when mail is unavailable', () => {
    configStore.getState().setEmailTransport('none');
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);
    expect(screen.getByText(/no email delivery configured/i)).toBeInTheDocument();
  });

  it('localizes an email_not_configured refusal instead of echoing the server sentence', async () => {
    const user = userEvent.setup();
    mockSendCode.mockRejectedValue(
      new ApiError(
        'email delivery is not configured on this server',
        503,
        'Service Unavailable',
        {
          error: 'email delivery is not configured on this server',
          code: 'email_not_configured',
        },
        'email delivery is not configured on this server',
      ),
    );

    renderWithI18n(<LoginPage onSuccess={onSuccess} />);
    await user.type(screen.getByLabelText(/email/i), 'user@example.com');
    await user.click(screen.getByRole('button', { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/no email delivery configured/i)).toBeInTheDocument();
    });
    expect(screen.getByRole('button', { name: /continue/i })).toBeInTheDocument();
  });
});

describe('LoginPage — corporate sign-in (#394)', () => {
  const onSuccess = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    mockApiGetAuthMethods.mockResolvedValue({
      methods: ['email'],
      oidc_display_name: '',
      ldap_display_name: '',
    });
    mockApiOidcStartURL.mockReturnValue('https://goosar.example.test/api/auth/oidc/start');
    mockApiListWorkspaces.mockResolvedValue([]);
  });

  function offers(
    methods: string[],
    extra: Partial<{ oidc_display_name: string; ldap_display_name: string }> = {},
  ) {
    mockApiGetAuthMethods.mockResolvedValue({
      methods,
      oidc_display_name: '',
      ldap_display_name: '',
      ...extra,
    });
  }

  it('offers only the email form when the server offers only email', async () => {
    offers(['email']);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    await waitFor(() => expect(mockApiGetAuthMethods).toHaveBeenCalled());
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /corporate account/i })).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/password/i)).not.toBeInTheDocument();
  });

  it('offers the corporate button when the server offers oidc', async () => {
    offers(['email', 'oidc']);
    const onCorporateLogin = vi.fn();
    renderWithI18n(<LoginPage onSuccess={onSuccess} onCorporateLogin={onCorporateLogin} />);

    const button = await screen.findByRole('button', {
      name: /corporate account/i,
    });
    await userEvent.click(button);
    expect(onCorporateLogin).toHaveBeenCalledWith(
      'https://goosar.example.test/api/auth/oidc/start',
    );
  });

  it('asks the server to deliver the session to the desktop app when it is one', async () => {
    offers(['email', 'oidc']);
    mockApiOidcStartURL.mockReturnValue(
      'https://goosar.example.test/api/auth/oidc/start?client=desktop',
    );
    const onCorporateLogin = vi.fn();
    renderWithI18n(
      <LoginPage
        onSuccess={onSuccess}
        onCorporateLogin={onCorporateLogin}
        corporateClient="desktop"
      />,
    );

    await userEvent.click(await screen.findByRole('button', { name: /corporate account/i }));
    expect(mockApiOidcStartURL).toHaveBeenCalledWith('desktop');
    expect(onCorporateLogin).toHaveBeenCalledWith(
      'https://goosar.example.test/api/auth/oidc/start?client=desktop',
    );
  });

  it("uses the operator's own label for the corporate button", async () => {
    offers(['email', 'oidc'], { oidc_display_name: 'Вход через Keycloak' });
    renderWithI18n(<LoginPage onSuccess={onSuccess} onCorporateLogin={vi.fn()} />);

    expect(await screen.findByRole('button', { name: 'Вход через Keycloak' })).toBeInTheDocument();
  });

  it('hides the corporate button when the platform cannot service it', async () => {
    offers(['email', 'oidc']);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    await waitFor(() => expect(mockApiGetAuthMethods).toHaveBeenCalled());
    expect(screen.queryByRole('button', { name: /corporate account/i })).not.toBeInTheDocument();
  });

  it('leads with the directory form when email is not offered', async () => {
    offers(['ldap']);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    expect(await screen.findByLabelText(/password/i)).toBeInTheDocument();
    expect(screen.queryByLabelText(/^email$/i)).not.toBeInTheDocument();
  });

  it('signs in through the directory and hands the session to the caller', async () => {
    offers(['ldap']);
    mockApiLdapLogin.mockResolvedValue({
      token: 'ldap-session-token',
      user: { id: 'user-1', email: 'jdoe@example.test' },
    });
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    await userEvent.type(await screen.findByLabelText(/username/i), 'jdoe');
    await userEvent.type(screen.getByLabelText(/password/i), 'correct-horse');
    await userEvent.click(screen.getByRole('button', { name: /^sign in$/i }));

    await waitFor(() => expect(mockApiLdapLogin).toHaveBeenCalledWith('jdoe', 'correct-horse'));
    await waitFor(() => expect(mockLoginWithToken).toHaveBeenCalledWith('ldap-session-token'));
    await waitFor(() => expect(onSuccess).toHaveBeenCalled());
  });

  it('shows the localized refusal for a bad directory password', async () => {
    offers(['ldap']);
    mockApiLdapLogin.mockRejectedValue(
      new ApiError(
        'invalid username or password',
        401,
        'Unauthorized',
        { error: 'invalid username or password', code: 'ldap_invalid_credentials' },
        'invalid username or password',
      ),
    );
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    await userEvent.type(await screen.findByLabelText(/username/i), 'jdoe');
    await userEvent.type(screen.getByLabelText(/password/i), 'wrong');
    await userEvent.click(screen.getByRole('button', { name: /^sign in$/i }));

    await waitFor(() =>
      expect(screen.getByText(/wrong username or password/i)).toBeInTheDocument(),
    );
    expect(onSuccess).not.toHaveBeenCalled();
  });

  it('states the cause when the directory is down', async () => {
    offers(['email', 'ldap']);
    mockApiLdapLogin.mockRejectedValue(
      new ApiError(
        'the directory did not answer',
        502,
        'Bad Gateway',
        { error: 'the directory did not answer', code: 'ldap_unavailable' },
        'the directory did not answer',
      ),
    );
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    await userEvent.click(await screen.findByRole('button', { name: /corporate sign-in/i }));
    await userEvent.type(screen.getByLabelText(/username/i), 'jdoe');
    await userEvent.type(screen.getByLabelText(/password/i), 'whatever');
    await userEvent.click(screen.getByRole('button', { name: /^sign in$/i }));

    await waitFor(() =>
      expect(screen.getByText(/corporate directory did not answer/i)).toBeInTheDocument(),
    );
    expect(screen.getByRole('button', { name: /sign in with email/i })).toBeInTheDocument();
  });

  it('refuses a malformed directory response instead of half-signing-in', async () => {
    offers(['ldap']);
    mockApiLdapLogin.mockResolvedValue({ token: '', user: null });
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    await userEvent.type(await screen.findByLabelText(/username/i), 'jdoe');
    await userEvent.type(screen.getByLabelText(/password/i), 'correct-horse');
    await userEvent.click(screen.getByRole('button', { name: /^sign in$/i }));

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument());
    expect(mockLoginWithToken).not.toHaveBeenCalled();
    expect(onSuccess).not.toHaveBeenCalled();
  });

  it('falls back to the email form when the methods call fails', async () => {
    mockApiGetAuthMethods.mockRejectedValue(new Error('Failed to fetch'));
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    await waitFor(() => expect(mockApiGetAuthMethods).toHaveBeenCalled());
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /continue/i })).toBeInTheDocument();
  });

  it('localizes a corporate refusal code handed in by the platform', async () => {
    offers(['email', 'oidc']);
    renderWithI18n(
      <LoginPage
        onSuccess={onSuccess}
        onCorporateLogin={vi.fn()}
        corporateErrorCode="oidc_provider_unavailable"
      />,
    );

    expect(await screen.findByText(/identity provider did not answer/i)).toBeInTheDocument();
  });

  describe('second factor', () => {
    async function reachTheMFAStep() {
      mockSendCode.mockResolvedValueOnce(undefined);
      mockApiVerifyCode.mockResolvedValueOnce({
        token: '',
        mfa_required: true,
        mfa_token: 'ticket-1',
      });
      renderWithI18n(<LoginPage onSuccess={onSuccess} />);
      const user = userEvent.setup();
      await user.type(screen.getByLabelText(/email/i), 'test@example.com');
      await user.click(screen.getByRole('button', { name: /continue/i }));
      await waitFor(() => {
        expect(screen.getByText(/check your email/i)).toBeInTheDocument();
      });
      await user.type(getOTPInput(), '123456');
      expect(await screen.findByText(/two-step verification/i)).toBeInTheDocument();
      return user;
    }

    it('asks for a code instead of signing in when the server demands one', async () => {
      await reachTheMFAStep();
      expect(mockLoginWithToken).not.toHaveBeenCalled();
      expect(onSuccess).not.toHaveBeenCalled();
    });

    it('exchanges the ticket for a session', async () => {
      const user = await reachTheMFAStep();
      mockApiVerifyMFA.mockResolvedValueOnce({ token: 'jwt', mfa_required: false });
      mockApiListWorkspaces.mockResolvedValueOnce([{ id: 'ws-1' }]);

      await user.type(getOTPInput(), '654321');

      await waitFor(() => {
        expect(mockApiVerifyMFA).toHaveBeenCalledWith({
          mfa_token: 'ticket-1',
          code: '654321',
        });
        expect(mockLoginWithToken).toHaveBeenCalledWith('jwt');
        expect(onSuccess).toHaveBeenCalled();
      });
    });

    it('sends a recovery code on the recovery-code field, not as a TOTP code', async () => {
      const user = await reachTheMFAStep();
      await user.click(screen.getByRole('button', { name: /recovery code/i }));

      mockApiVerifyMFA.mockResolvedValueOnce({ token: 'jwt', mfa_required: false });
      mockApiListWorkspaces.mockResolvedValueOnce([{ id: 'ws-1' }]);
      await user.type(screen.getByLabelText(/recovery code/i), 'ABCDEFGHIJKLMNOP');
      await user.click(screen.getByRole('button', { name: /^continue$/i }));

      await waitFor(() => {
        expect(mockApiVerifyMFA).toHaveBeenCalledWith({
          mfa_token: 'ticket-1',
          recovery_code: 'ABCDEFGHIJKLMNOP',
        });
      });
    });

    it('keeps the person on the step when the code is refused', async () => {
      const user = await reachTheMFAStep();
      mockApiVerifyMFA.mockRejectedValueOnce(serverRejection('that code is not valid'));

      await user.type(getOTPInput(), '000000');

      expect(await screen.findByRole('alert')).toBeInTheDocument();
      expect(screen.getByText(/two-step verification/i)).toBeInTheDocument();
      expect(onSuccess).not.toHaveBeenCalled();
    });

    it('treats a malformed exchange as a failure rather than a login', async () => {
      const user = await reachTheMFAStep();
      mockApiVerifyMFA.mockResolvedValueOnce({ token: '', mfa_required: false });

      await user.type(getOTPInput(), '111111');

      await waitFor(() => {
        expect(screen.getByRole('alert')).toBeInTheDocument();
      });
      expect(mockLoginWithToken).not.toHaveBeenCalled();
      expect(onSuccess).not.toHaveBeenCalled();
    });

    it('opens the step for a ticket handed back by a corporate sign-in', async () => {
      renderWithI18n(
        <LoginPage
          onSuccess={onSuccess}
          onCorporateLogin={vi.fn()}
          corporateMfaToken="ticket-oidc"
        />,
      );
      expect(await screen.findByText(/two-step verification/i)).toBeInTheDocument();
    });
  });
});
