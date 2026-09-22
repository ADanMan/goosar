import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '@goosar/views/locales/en/common.json';
import enAuth from '@goosar/views/locales/en/auth.json';
import enSettings from '@goosar/views/locales/en/settings.json';
import type { ReactNode } from 'react';

const TEST_RESOURCES = {
  en: { common: enCommon, auth: enAuth, settings: enSettings },
};

function createWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    </I18nProvider>
  );
}

const {
  mockSendCode,
  mockVerifyCode,
  mockIssueCliToken,
  mockListWorkspaces,
  mockListMyInvitations,
  mockPush,
  mockReplace,
  searchParamsState,
  authStateRef,
} = vi.hoisted(() => ({
  mockSendCode: vi.fn(),
  mockVerifyCode: vi.fn(),
  mockIssueCliToken: vi.fn(),
  mockListWorkspaces: vi.fn(),
  mockListMyInvitations: vi.fn(),
  mockPush: vi.fn(),
  mockReplace: vi.fn(),
  searchParamsState: { params: new URLSearchParams() },
  authStateRef: {
    state: {
      sendCode: vi.fn(),
      verifyCode: vi.fn(),
      user: null as null | { id: string; email: string; onboarded_at?: string | null },
      isLoading: false,
    },
  },
}));

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: mockPush, replace: mockReplace }),
  usePathname: () => '/login',
  useSearchParams: () => searchParamsState.params,
}));

vi.mock('@goosar/core/auth', async () => {
  const actual = await vi.importActual<typeof import('@goosar/core/auth')>('@goosar/core/auth');
  authStateRef.state.sendCode = mockSendCode;
  authStateRef.state.verifyCode = mockVerifyCode;
  const useAuthStore = Object.assign(
    (selector: (s: typeof authStateRef.state) => unknown) => selector(authStateRef.state),
    { getState: () => authStateRef.state },
  );
  return { ...actual, useAuthStore };
});

vi.mock('@/features/auth/auth-cookie', () => ({
  setLoggedInCookie: vi.fn(),
}));

vi.mock('@goosar/core/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/api')>()),
  api: {
    listWorkspaces: mockListWorkspaces,
    listMyInvitations: mockListMyInvitations,
    verifyCode: vi.fn(),
    setToken: vi.fn(),
    getMe: vi.fn(),
    getAuthMethods: vi.fn(async () => ({
      methods: ['email'],
      oidc_display_name: '',
      ldap_display_name: '',
    })),
    oidcStartURL: vi.fn(() => 'https://goosar.example.test/api/auth/oidc/start'),
    ldapLogin: vi.fn(),
    issueCliToken: mockIssueCliToken,
  },
}));

import LoginPage from './page';

describe('LoginPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    searchParamsState.params = new URLSearchParams();
    authStateRef.state.user = null;
    authStateRef.state.isLoading = false;
    mockListWorkspaces.mockResolvedValue([]);
    mockListMyInvitations.mockResolvedValue([]);
  });

  it('renders login form with email input and continue button', () => {
    render(<LoginPage />, { wrapper: createWrapper() });

    expect(screen.getByText('Sign in to Goosar')).toBeInTheDocument();
    expect(screen.getByText('Enter your email to get a login code')).toBeInTheDocument();
    expect(screen.getByLabelText('Email')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Continue' })).toBeInTheDocument();
  });

  it('does not call sendCode when email is empty', async () => {
    const user = userEvent.setup();
    render(<LoginPage />, { wrapper: createWrapper() });

    await user.click(screen.getByRole('button', { name: 'Continue' }));
    expect(mockSendCode).not.toHaveBeenCalled();
  });

  it('calls sendCode with email on submit', async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    const user = userEvent.setup();
    render(<LoginPage />, { wrapper: createWrapper() });

    await user.type(screen.getByLabelText('Email'), 'test@goosar.ru');
    await user.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => {
      expect(mockSendCode).toHaveBeenCalledWith('test@goosar.ru');
    });
  });

  it("shows 'Sending code...' while submitting", async () => {
    mockSendCode.mockReturnValueOnce(new Promise(() => {}));
    const user = userEvent.setup();
    render(<LoginPage />, { wrapper: createWrapper() });

    await user.type(screen.getByLabelText('Email'), 'test@goosar.ru');
    await user.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => {
      expect(screen.getByText('Sending code...')).toBeInTheDocument();
    });
  });

  it('shows verification code step after sending code', async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    const user = userEvent.setup();
    render(<LoginPage />, { wrapper: createWrapper() });

    await user.type(screen.getByLabelText('Email'), 'test@goosar.ru');
    await user.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => {
      expect(screen.getByText('Check your email')).toBeInTheDocument();
    });
  });

  it('shows error when sendCode fails', async () => {
    mockSendCode.mockRejectedValueOnce(new Error('Network error'));
    const user = userEvent.setup();
    render(<LoginPage />, { wrapper: createWrapper() });

    await user.type(screen.getByLabelText('Email'), 'test@goosar.ru');
    await user.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument();
    });
  });

  it('mints a token and deep-links to Desktop when already logged in with platform=desktop', async () => {
    searchParamsState.params = new URLSearchParams({ platform: 'desktop' });
    authStateRef.state.user = { id: 'u1', email: 'test@goosar.ru' };
    mockIssueCliToken.mockImplementation(() => Promise.resolve({ token: 'handoff-jwt' }));

    const hrefSetter = vi.fn();
    const originalLocation = window.location;
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: {
        ...originalLocation,
        set href(value: string) {
          hrefSetter(value);
        },
      },
    });

    try {
      render(<LoginPage />, { wrapper: createWrapper() });

      await waitFor(() => {
        expect(mockIssueCliToken).toHaveBeenCalledTimes(1);
      });
      await waitFor(() => {
        expect(hrefSetter).toHaveBeenCalledWith('goosar://auth/callback?token=handoff-jwt');
      });
      expect(
        await screen.findByRole('button', { name: 'Open Goosar Desktop' }),
      ).toBeInTheDocument();
    } finally {
      Object.defineProperty(window, 'location', {
        configurable: true,
        value: originalLocation,
      });
    }
  });

  describe('post-login redirect ownership (#5009)', () => {
    const onboardedUser = {
      id: 'u1',
      email: 'test@goosar.ru',
      onboarded_at: '2026-01-01T00:00:00Z',
    };

    it('does not redirect from the arrival effect when the user logs in via the form', async () => {
      const wrapper = createWrapper();
      const { rerender } = render(<LoginPage />, { wrapper });
      authStateRef.state.user = onboardedUser;
      rerender(<LoginPage />);

      await act(async () => {});
      expect(mockReplace).not.toHaveBeenCalled();
      expect(mockPush).not.toHaveBeenCalled();
      expect(mockListWorkspaces).not.toHaveBeenCalled();
    });

    it('fetches the workspace list before redirecting a visitor who arrived authenticated', async () => {
      authStateRef.state.user = onboardedUser;
      mockListWorkspaces.mockResolvedValue([{ id: 'ws-1', slug: 'acme' }]);

      render(<LoginPage />, { wrapper: createWrapper() });

      await waitFor(() => {
        expect(mockReplace).toHaveBeenCalledWith('/acme/issues');
      });
      expect(mockListWorkspaces).toHaveBeenCalledTimes(1);
    });

    it('still honors ?next= for a visitor who arrived authenticated', async () => {
      searchParamsState.params = new URLSearchParams({
        next: '/invite/abc',
      });
      authStateRef.state.user = onboardedUser;

      render(<LoginPage />, { wrapper: createWrapper() });

      await waitFor(() => {
        expect(mockReplace).toHaveBeenCalledWith('/invite/abc');
      });
      expect(mockListWorkspaces).not.toHaveBeenCalled();
    });
  });
});

describe('LoginPage — corporate refusal in the URL fragment (#394)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    searchParamsState.params = new URLSearchParams();
    authStateRef.state.user = null;
    authStateRef.state.isLoading = false;
    mockListWorkspaces.mockResolvedValue([]);
    mockListMyInvitations.mockResolvedValue([]);
    window.history.replaceState(null, '', '/login');
  });

  it('shows the localized reason a corporate sign-in failed', async () => {
    window.history.replaceState(null, '', '/login#auth_error=oidc_provider_unavailable');
    render(<LoginPage />, { wrapper: createWrapper() });

    expect(await screen.findByText(/identity provider did not answer/i)).toBeInTheDocument();
  });

  it('strips the fragment once it has been read', async () => {
    window.history.replaceState(null, '', '/login#auth_error=oidc_state_invalid');
    render(<LoginPage />, { wrapper: createWrapper() });

    await screen.findByText(/took too long or was already completed/i);
    await waitFor(() => expect(window.location.hash).toBe(''));
    expect(window.location.pathname).toBe('/login');
  });

  it('falls back to a general sentence for a code it does not know', async () => {
    window.history.replaceState(null, '', '/login#auth_error=something_new');
    render(<LoginPage />, { wrapper: createWrapper() });

    expect(await screen.findByText(/corporate sign-in failed/i)).toBeInTheDocument();
  });

  it('says nothing when the URL carries no failure', async () => {
    render(<LoginPage />, { wrapper: createWrapper() });

    await screen.findByRole('button', { name: 'Continue' });
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});
