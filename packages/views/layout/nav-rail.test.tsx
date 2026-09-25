import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { NavRail } from './nav-rail';

const { navigation, push } = vi.hoisted(() => ({
  navigation: { current: { pathname: '/acme/issues' } },
  push: vi.fn(),
}));

vi.mock('@goosar/ui/components/ui/sidebar', () => ({
  Sidebar: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroup: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroupContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarMenuButton: ({
    children,
    isActive,
    render,
    onClick,
    ...rest
  }: {
    children: React.ReactNode;
    isActive?: boolean;
    render?: React.ReactElement<{ href?: string; onClick?: () => void }>;
    onClick?: () => void;
    [key: string]: unknown;
  }) => (
    <button
      type="button"
      data-active={isActive ? 'true' : undefined}
      data-href={render?.props.href}
      onClick={() => {
        onClick?.();
        if (render?.props.href) push(render.props.href);
      }}
      {...rest}
    >
      {children}
    </button>
  ),
}));
vi.mock('@goosar/ui/components/ui/dropdown-menu', () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuGroup: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuLabel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuSeparator: () => null,
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
}));
vi.mock('../navigation', () => ({
  AppLink: ({ children, href, onClick }: { children: React.ReactNode; href: string; onClick?: () => void }) => (
    <a
      href={href}
      onClick={(e) => {
        e.preventDefault();
        onClick?.();
        push(href);
      }}
    >
      {children}
    </a>
  ),
  useNavigation: () => ({ pathname: navigation.current.pathname, push }),
}));
vi.mock('@goosar/ui/components/common/actor-avatar', () => ({ ActorAvatar: () => <span /> }));
vi.mock('../workspace/workspace-avatar', () => ({ WorkspaceAvatar: () => <span /> }));
vi.mock('@goosar/core/auth', () => ({ useAuthStore: () => ({ id: 'u1', name: 'Ada', email: 'a@x.com' }) }));
vi.mock('../auth', () => ({ useLogout: () => vi.fn() }));
vi.mock('@goosar/core/config', () => ({ useConfigStore: () => false }));
vi.mock('@goosar/core/modals', () => ({ useModalStore: { getState: () => ({ open: vi.fn() }) } }));
vi.mock('@goosar/core/api', () => ({ api: { listWorkspaces: async () => [] } }));
vi.mock('@goosar/core/workspace/queries', () => ({ workspaceListOptions: () => ({ queryKey: ['workspaces'] }) }));
vi.mock('@goosar/core/workspace/avatar-url', () => ({ resolvePublicFileUrl: () => undefined }));
vi.mock('@tanstack/react-query', () => ({ useQuery: () => ({ data: [] }) }));
vi.mock('@goosar/core/paths', () => ({
  useCurrentWorkspace: () => ({ id: 'ws-1', name: 'Acme', avatar_url: null }),
  useWorkspacePaths: () => ({
    inbox: () => '/acme/inbox',
    issues: () => '/acme/issues',
    projects: () => '/acme/projects',
    agents: () => '/acme/agents',
    autopilots: () => '/acme/autopilots',
    settings: () => '/acme/settings',
  }),
  paths: { workspace: (slug: string) => ({ issues: () => `/${slug}/issues` }) },
}));
vi.mock('../i18n', () => ({
  useT: () => ({
    t: (sel: (r: Record<string, unknown>) => string) =>
      sel({
        nav: {
          inbox: 'Лента',
          issues: 'Задачи',
          projects: 'Проекты',
          agents: 'Исполнители',
          autopilots: 'Автопилот',
          settings: 'Настройки',
        },
        sidebar: { workspaces_label: 'Рабочие пространства', log_out: 'Выйти', create_workspace: 'Создать' },
      }),
  }),
}));

describe('NavRail', () => {
  beforeEach(() => {
    push.mockReset();
    navigation.current.pathname = '/acme/issues';
  });

  it('marks the section matching the current path as active', () => {
    render(<NavRail activeSection="tasks" />);
    expect(screen.getByLabelText('Задачи')).toHaveAttribute('data-active', 'true');
    expect(screen.getByLabelText('Лента')).not.toHaveAttribute('data-active', 'true');
  });

  it('marks nothing active when the path is outside all six sections', () => {
    render(<NavRail activeSection={null} />);
    for (const label of ['Лента', 'Задачи', 'Проекты', 'Исполнители', 'Автопилот', 'Настройки']) {
      expect(screen.getByLabelText(label)).not.toHaveAttribute('data-active', 'true');
    }
  });

  it('reaches every rail item with Tab and activates it with Enter', async () => {
    const user = userEvent.setup();
    render(<NavRail activeSection="tasks" />);

    const crew = screen.getByLabelText('Исполнители');
    crew.focus();
    expect(crew).toHaveFocus();

    await user.keyboard('{Enter}');
    expect(push).toHaveBeenCalledWith('/acme/agents');
  });

  it('exposes an aria-label on every rail item', () => {
    render(<NavRail activeSection={null} />);
    for (const label of ['Лента', 'Задачи', 'Проекты', 'Исполнители', 'Автопилот', 'Настройки']) {
      expect(screen.getByLabelText(label)).toBeInTheDocument();
    }
  });
});
