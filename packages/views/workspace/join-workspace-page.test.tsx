import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '@goosar/core/i18n/react';
import { configStore } from '@goosar/core/config';
import enCommon from '../locales/en/common.json';
import enWorkspace from '../locales/en/workspace.json';
import { JoinWorkspacePage } from './join-workspace-page';

const mockListJoinTargets = vi.fn<() => Promise<unknown[]>>(async () => []);
const mockJoinTarget = vi.fn<(id: string) => Promise<unknown>>();
vi.mock('@goosar/core/api', () => ({
  api: {
    listJoinTargets: () => mockListJoinTargets(),
    joinTarget: (id: string) => mockJoinTarget(id),
  },
}));

const mockReplace = vi.fn();
vi.mock('../navigation', () => ({
  useNavigation: () => ({ push: vi.fn(), replace: mockReplace, back: vi.fn() }),
}));

vi.mock('../auth', () => ({ useLogout: () => vi.fn() }));
vi.mock('../platform', () => ({ DragStrip: () => null }));

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={{ en: { common: enCommon, workspace: enWorkspace } }}>
      <QueryClientProvider
        client={
          new QueryClient({
            defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
          })
        }
      >
        {children}
      </QueryClientProvider>
    </I18nProvider>
  );
}

function renderPage(onCreateInstead?: () => void) {
  return render(<JoinWorkspacePage onCreateInstead={onCreateInstead} />, {
    wrapper: Wrapper,
  });
}

const HR_ROLE = {
  id: 'ws-hr',
  slug: 'hr',
  name: 'HR',
  description: 'Hiring and onboarding',
  template_key: 'hr',
  member_count: 3,
};

describe('JoinWorkspacePage', () => {
  beforeEach(() => {
    mockListJoinTargets.mockReset();
    mockJoinTarget.mockReset();
    mockReplace.mockReset();
    mockListJoinTargets.mockResolvedValue([]);
    configStore.setState({ workspaceCreationDisabled: false });
  });

  it('hides workspace creation when the deployment disabled it', async () => {
    configStore.setState({ workspaceCreationDisabled: true });
    renderPage(vi.fn());

    expect(
      await screen.findByText(/offers no roles to join yet/, {}, { timeout: 5000 }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: 'Create my own workspace instead' }),
    ).not.toBeInTheDocument();
  });

  it('renders one card per role with its description and member count', async () => {
    mockListJoinTargets.mockResolvedValue([HR_ROLE]);
    renderPage();

    expect(await screen.findByText('HR', {}, { timeout: 5000 })).toBeInTheDocument();
    expect(screen.getByText('Hiring and onboarding')).toBeInTheDocument();
    expect(screen.getByText('3 members')).toBeInTheDocument();
  });

  it('joins and navigates to the workspace', async () => {
    mockListJoinTargets.mockResolvedValue([HR_ROLE]);
    mockJoinTarget.mockResolvedValue({
      id: 'ws-hr',
      slug: 'hr',
      name: 'HR',
      already_member: false,
    });
    renderPage();

    fireEvent.click(await screen.findByRole('button', { name: 'Join' }, { timeout: 5000 }));

    await waitFor(() => expect(mockJoinTarget).toHaveBeenCalledWith('ws-hr'));
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/hr/issues'));
  });

  it('navigates to root rather than a broken path when the response has no slug', async () => {
    mockListJoinTargets.mockResolvedValue([HR_ROLE]);
    mockJoinTarget.mockResolvedValue({
      id: '',
      slug: '',
      name: '',
      already_member: false,
    });
    renderPage();

    fireEvent.click(await screen.findByRole('button', { name: 'Join' }, { timeout: 5000 }));

    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/'));
  });

  it('reports a failed join and stays on the page', async () => {
    mockListJoinTargets.mockResolvedValue([HR_ROLE]);
    mockJoinTarget.mockRejectedValue(new Error('nope'));
    renderPage();

    fireEvent.click(await screen.findByRole('button', { name: 'Join' }, { timeout: 5000 }));

    expect(await screen.findByText('Could not join. Try again.')).toBeInTheDocument();
    expect(mockReplace).not.toHaveBeenCalled();
  });

  it('explains an empty catalog instead of showing a blank page', async () => {
    renderPage();

    expect(
      await screen.findByText(/offers no roles to join yet/, {}, { timeout: 5000 }),
    ).toBeInTheDocument();
  });

  it('offers workspace creation only when the platform supplies it', async () => {
    const onCreateInstead = vi.fn();
    const { rerender } = renderPage(onCreateInstead);

    fireEvent.click(
      await screen.findByRole(
        'button',
        { name: 'Create my own workspace instead' },
        { timeout: 5000 },
      ),
    );
    expect(onCreateInstead).toHaveBeenCalled();

    rerender(<JoinWorkspacePage />);
    expect(
      screen.queryByRole('button', { name: 'Create my own workspace instead' }),
    ).not.toBeInTheDocument();
  });
});
