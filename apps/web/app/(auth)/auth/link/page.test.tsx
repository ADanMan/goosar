import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '@goosar/views/locales/en/common.json';
import enAuth from '@goosar/views/locales/en/auth.json';
import type { ReactNode } from 'react';

const TEST_RESOURCES = {
  en: { common: enCommon, auth: enAuth },
};

function createWrapper() {
  return ({ children }: { children: ReactNode }) => (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const { mockLoginWithLinkToken, mockReplace, mockSetLoggedInCookie } = vi.hoisted(() => ({
  mockLoginWithLinkToken: vi.fn(),
  mockReplace: vi.fn(),
  mockSetLoggedInCookie: vi.fn(),
}));

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), replace: mockReplace }),
  usePathname: () => '/auth/link',
}));

vi.mock('@goosar/core/auth', async () => {
  const actual = await vi.importActual<typeof import('@goosar/core/auth')>('@goosar/core/auth');
  const state = { loginWithLinkToken: mockLoginWithLinkToken };
  const useAuthStore = Object.assign((selector: (s: typeof state) => unknown) => selector(state), {
    getState: () => state,
  });
  return { ...actual, useAuthStore };
});

vi.mock('@/features/auth/auth-cookie', () => ({
  setLoggedInCookie: mockSetLoggedInCookie,
}));

import { MFARequiredError } from '@goosar/core/auth';
import AuthLinkPage from './page';

describe('AuthLinkPage', () => {
  const originalLocation = window.location;
  let hrefSetter: (value: string) => void;

  function stubLocation(hash: string) {
    hrefSetter = vi.fn();
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: {
        ...originalLocation,
        hash,
        set href(value: string) {
          hrefSetter(value);
        },
      },
    });
  }

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: originalLocation,
    });
  });

  it('scrubs the credential from the address bar on mount (#226 review)', async () => {
    stubLocation('#lt=tok-scrub-me');
    const replaceState = vi.spyOn(window.history, 'replaceState').mockImplementation(() => {});

    render(<AuthLinkPage />, { wrapper: createWrapper() });

    await waitFor(() => {
      expect(replaceState).toHaveBeenCalledWith(null, '', expect.any(String));
    });
    replaceState.mockRestore();
  });

  it('fires the goosar:// deep link once and offers the web fallback', async () => {
    stubLocation('#lt=one-time-tok');

    render(<AuthLinkPage />, { wrapper: createWrapper() });

    await waitFor(() => {
      expect(hrefSetter).toHaveBeenCalledWith('goosar://auth/callback?link_token=one-time-tok');
    });
    expect(hrefSetter).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('button', { name: 'Continue in browser' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Go to sign-in' })).toHaveAttribute('href', '/login');
  });

  it("exchanges the token on 'Continue in browser' and hands off to /login", async () => {
    stubLocation('#lt=one-time-tok');
    mockLoginWithLinkToken.mockResolvedValue({ id: 'u1' });

    render(<AuthLinkPage />, { wrapper: createWrapper() });

    await userEvent.click(await screen.findByRole('button', { name: 'Continue in browser' }));

    await waitFor(() => {
      expect(mockLoginWithLinkToken).toHaveBeenCalledWith('one-time-tok');
    });
    expect(mockSetLoggedInCookie).toHaveBeenCalledTimes(1);
    expect(mockReplace).toHaveBeenCalledWith('/login');
  });

  it("carries a second-factor demand to the sign-in screen's code step", async () => {
    stubLocation('#lt=one-time-tok');
    mockLoginWithLinkToken.mockRejectedValue(new MFARequiredError('ticket-1'));

    render(<AuthLinkPage />, { wrapper: createWrapper() });

    await userEvent.click(await screen.findByRole('button', { name: 'Continue in browser' }));

    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith('/login#mfa_token=ticket-1');
    });
    expect(mockSetLoggedInCookie).not.toHaveBeenCalled();
  });

  it('shows the spent-link state when the exchange is refused (one-time link)', async () => {
    stubLocation('#lt=already-used');
    mockLoginWithLinkToken.mockRejectedValue(new Error('invalid or expired link'));

    render(<AuthLinkPage />, { wrapper: createWrapper() });

    await userEvent.click(await screen.findByRole('button', { name: 'Continue in browser' }));

    expect(await screen.findByText('This link no longer works')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Go to sign-in' })).toHaveAttribute('href', '/login');
    expect(mockReplace).not.toHaveBeenCalled();
  });

  it('treats a link without a token as broken and routes to web sign-in', async () => {
    stubLocation('');

    render(<AuthLinkPage />, { wrapper: createWrapper() });

    expect(await screen.findByText('This link no longer works')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Go to sign-in' })).toHaveAttribute('href', '/login');
    expect(hrefSetter).not.toHaveBeenCalled();
  });
});
