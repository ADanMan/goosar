// @vitest-environment jsdom

import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ruLayout from '../locales/ru/layout.json';

const { navigation } = vi.hoisted(() => ({
  navigation: { current: { pathname: '/acme/inbox' } },
}));

vi.mock('../navigation', () => ({
  AppLink: ({ children, href }: { children: React.ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
  useNavigation: () => ({ pathname: navigation.current.pathname }),
}));
vi.mock('@goosar/core/auth', () => ({ useAuthStore: () => undefined }));
vi.mock('@goosar/core/paths', () => ({
  useCurrentWorkspace: () => ({ id: 'ws-1' }),
  useWorkspacePaths: () => ({
    inbox: () => '/acme/inbox',
    chat: () => '/acme/chat',
    myIssues: () => '/acme/my-issues',
    projects: () => '/acme/projects',
    agents: () => '/acme/agents',
    squads: () => '/acme/squads',
    runtimes: () => '/acme/runtimes',
    skills: () => '/acme/skills',
    usage: () => '/acme/usage',
    settings: () => '/acme/settings',
    issueDetail: (id: string) => `/acme/issues/${id}`,
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
}));
vi.mock('@goosar/core/pins/queries', () => ({ pinListOptions: () => ({ queryKey: ['pins'] }) }));
vi.mock('@goosar/core/pins/mutations', () => ({ useDeletePin: () => vi.fn() }));
vi.mock('@goosar/core/issues/queries', () => ({ issueDetailOptions: () => ({ queryKey: ['issue'] }) }));
vi.mock('@goosar/core/projects/queries', () => ({ projectDetailOptions: () => ({ queryKey: ['project'] }) }));
vi.mock('@tanstack/react-query', () => ({ useQuery: () => ({ data: undefined }) }));
vi.mock('../issues/components/status-icon', () => ({ StatusIcon: () => <span /> }));
vi.mock('../projects/components/project-icon', () => ({ ProjectIcon: () => <span /> }));
vi.mock('@goosar/ui/components/ui/sidebar', () => ({
  Sidebar: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroup: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroupContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroupLabel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarMenuButton: ({
    children,
    isActive,
    render,
  }: {
    children: React.ReactNode;
    isActive?: boolean;
    render?: React.ReactElement<{ href?: string }>;
  }) => (
    <a href={render?.props.href} data-active={isActive ? 'true' : undefined}>
      {children}
    </a>
  ),
}));
vi.mock('../i18n', () => ({
  useT: () => ({
    t: (sel: (r: typeof ruLayout) => string) => sel(ruLayout),
  }),
}));

const STORAGE_KEY = 'goosar.contextPanel.collapsed';

afterEach(() => {
  cleanup();
  window.localStorage.clear();
  vi.resetModules();
});

beforeEach(() => {
  window.localStorage.clear();
  navigation.current.pathname = '/acme/inbox';
});

describe('ContextPanel collapse toggle (T-020)', () => {
  it('starts expanded by default and collapses on button click', async () => {
    const { ContextPanel } = await import('./context-panel');
    const user = userEvent.setup();
    render(<ContextPanel activeSection="feed" />);

    expect(screen.getByText(ruLayout.nav.inbox)).toBeInTheDocument();
    await user.click(screen.getByLabelText(ruLayout.sidebar.panel_collapse));

    expect(screen.queryByText(ruLayout.nav.inbox)).not.toBeInTheDocument();
    expect(screen.getByLabelText(ruLayout.sidebar.panel_expand)).toBeInTheDocument();
  });

  it('toggles with the `[` key', async () => {
    const { ContextPanel } = await import('./context-panel');
    const user = userEvent.setup();
    render(<ContextPanel activeSection="feed" />);

    await user.keyboard('{[}');
    expect(screen.getByLabelText(ruLayout.sidebar.panel_expand)).toBeInTheDocument();

    await user.keyboard('{[}');
    expect(screen.getByLabelText(ruLayout.sidebar.panel_collapse)).toBeInTheDocument();
  });

  it('persists collapsed state across remounts via localStorage', async () => {
    const { ContextPanel } = await import('./context-panel');
    const user = userEvent.setup();
    const { unmount } = render(<ContextPanel activeSection="feed" />);

    await user.click(screen.getByLabelText(ruLayout.sidebar.panel_collapse));
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('true');
    unmount();

    vi.resetModules();
    const { ContextPanel: ContextPanel2 } = await import('./context-panel');
    render(<ContextPanel2 activeSection="feed" />);
    expect(screen.getByLabelText(ruLayout.sidebar.panel_expand)).toBeInTheDocument();
  });

  it('renders the chat section without its own sub-items once chat has its own rail item', async () => {
    const { ContextPanel } = await import('./context-panel');
    render(<ContextPanel activeSection="chat" />);
    expect(screen.getByText(ruLayout.nav.chat)).toBeInTheDocument();
    expect(screen.queryByText(ruLayout.nav.inbox)).not.toBeInTheDocument();
  });
});

describe('ContextPanel settings section billing item (T-032 §3.2)', () => {
  it('hides the billing item when the test-page flag is off', async () => {
    const { ContextPanel } = await import('./context-panel');
    render(<ContextPanel activeSection="settings" />);
    expect(screen.getByText(ruLayout.nav.runtimes)).toBeInTheDocument();
    expect(screen.queryByText(ruLayout.nav.billing)).not.toBeInTheDocument();
  });

  it('shows the billing item, linked under the workspace, when the flag is on', async () => {
    const { ContextPanel } = await import('./context-panel');
    render(<ContextPanel activeSection="settings" billingEnabled />);
    const billingLink = screen.getByText(ruLayout.nav.billing).closest('a');
    expect(billingLink).toHaveAttribute('href', '/acme/billing');
  });
});
