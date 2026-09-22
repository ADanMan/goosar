import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

function createWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
}

const { mockListWorkspaces, mockPush, mockReplace, searchParamsState, authStateRef } = vi.hoisted(
  () => ({
    mockListWorkspaces: vi.fn(),
    mockPush: vi.fn(),
    mockReplace: vi.fn(),
    searchParamsState: { params: new URLSearchParams() },
    authStateRef: {
      state: {
        user: null as null | {
          id: string;
          email: string;
          onboarded_at?: string | null;
        },
        isLoading: false,
      },
    },
  }),
);

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: mockPush, replace: mockReplace }),
  usePathname: () => '/onboarding',
  useSearchParams: () => searchParamsState.params,
}));

vi.mock('@goosar/core/auth', async () => {
  const actual = await vi.importActual<typeof import('@goosar/core/auth')>('@goosar/core/auth');
  const useAuthStore = Object.assign(
    (selector: (s: typeof authStateRef.state) => unknown) => selector(authStateRef.state),
    { getState: () => authStateRef.state },
  );
  return { ...actual, useAuthStore };
});

vi.mock('@goosar/core/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/api')>()),
  api: {
    listWorkspaces: mockListWorkspaces,
  },
}));

vi.mock('@goosar/views/onboarding', () => ({
  OnboardingFlow: () => <div data-testid="onboarding-flow" />,
  CliInstallInstructions: () => null,
}));

import OnboardingPage from './page';

const onboardedUser = {
  id: 'u1',
  email: 'test@goosar.ru',
  onboarded_at: '2026-01-01T00:00:00Z',
};

describe('OnboardingPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    searchParamsState.params = new URLSearchParams();
    authStateRef.state.user = null;
    authStateRef.state.isLoading = false;
    mockListWorkspaces.mockResolvedValue([]);
  });

  it('redirects a logged-out visitor to /login', async () => {
    render(<OnboardingPage />, { wrapper: createWrapper() });

    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith('/login');
    });
    expect(screen.queryByTestId('onboarding-flow')).not.toBeInTheDocument();
  });

  it('bounces an onboarded user without replay to their workspace', async () => {
    authStateRef.state.user = onboardedUser;
    mockListWorkspaces.mockResolvedValue([{ id: 'ws-1', slug: 'acme' }]);

    render(<OnboardingPage />, { wrapper: createWrapper() });

    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith('/acme/issues');
    });
    expect(screen.queryByTestId('onboarding-flow')).not.toBeInTheDocument();
  });

  it('renders the flow for an onboarded user when replay=1', async () => {
    searchParamsState.params = new URLSearchParams({ replay: '1' });
    authStateRef.state.user = onboardedUser;
    mockListWorkspaces.mockResolvedValue([{ id: 'ws-1', slug: 'acme' }]);

    render(<OnboardingPage />, { wrapper: createWrapper() });

    expect(await screen.findByTestId('onboarding-flow')).toBeInTheDocument();
    await act(async () => {});
    expect(mockReplace).not.toHaveBeenCalled();
    expect(mockPush).not.toHaveBeenCalled();
  });

  it('keeps the flow mounted when onboarding completes mid-flow', async () => {
    authStateRef.state.user = {
      id: 'u1',
      email: 'test@goosar.ru',
      onboarded_at: null,
    };
    mockListWorkspaces.mockResolvedValue([]);

    const { rerender } = render(<OnboardingPage />, {
      wrapper: createWrapper(),
    });
    expect(await screen.findByTestId('onboarding-flow')).toBeInTheDocument();
    await act(async () => {});

    authStateRef.state.user = onboardedUser;
    rerender(<OnboardingPage />);
    await act(async () => {});

    expect(screen.getByTestId('onboarding-flow')).toBeInTheDocument();
    expect(mockReplace).not.toHaveBeenCalled();
    expect(mockPush).not.toHaveBeenCalled();
  });

  it("still bounces when replay carries a non-'1' value", async () => {
    searchParamsState.params = new URLSearchParams({ replay: 'yes' });
    authStateRef.state.user = onboardedUser;
    mockListWorkspaces.mockResolvedValue([{ id: 'ws-1', slug: 'acme' }]);

    render(<OnboardingPage />, { wrapper: createWrapper() });

    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith('/acme/issues');
    });
    expect(screen.queryByTestId('onboarding-flow')).not.toBeInTheDocument();
  });
});
