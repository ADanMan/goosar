import { render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@goosar/core/api';
import { AppSidebar } from './app-sidebar';

const {
  appForeground,
  chatSessions,
  chatStore,
  detail,
  deletePin,
  inboxItems,
  navigation,
  pins,
  realtime,
  summary,
  workspaces,
} = vi.hoisted(() => ({
  appForeground: { current: true },
  realtime: {
    current: {
      state: 'connected' as string,
      captured: {
        inboxList: undefined as unknown,
        unreadSummary: undefined as unknown,
        chatSessions: undefined as unknown,
      },
    },
  },
  chatSessions: { current: [] as { id?: string; unread_count?: number }[] },
  chatStore: {
    current: {
      activeSessionId: null as string | null,
      isOpen: false,
      floatingChatEnabled: true,
    },
  },
  detail: {
    current: { isPending: false, isError: false, data: null as unknown, error: null as unknown },
  },
  deletePin: vi.fn(),
  inboxItems: { current: [] as { id: string; read: boolean }[] },
  navigation: { current: { pathname: '/acme/issues' } },
  summary: { current: [] as { workspace_id: string; count: number }[] },
  workspaces: {
    current: [] as { id: string; name: string; slug: string; avatar_url: string | null }[],
  },
  pins: {
    current: [
      {
        id: 'pin-1',
        workspace_id: 'ws-1',
        user_id: 'user-1',
        item_type: 'issue' as const,
        item_id: 'issue-1',
        position: 0,
        created_at: '2026-05-06T00:00:00Z',
      },
    ],
  },
}));

vi.mock('@dnd-kit/core', () => ({
  DndContext: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PointerSensor: vi.fn(),
  closestCenter: vi.fn(),
  useSensor: vi.fn(),
  useSensors: vi.fn(),
}));
vi.mock('@dnd-kit/sortable', () => ({
  SortableContext: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useSortable: () => ({ attributes: {}, listeners: {}, setNodeRef: vi.fn() }),
  verticalListSortingStrategy: vi.fn(),
}));
vi.mock('@dnd-kit/utilities', () => ({ CSS: { Transform: { toString: () => undefined } } }));
vi.mock('@goosar/ui/components/ui/sidebar', () => ({
  Sidebar: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarFooter: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarGroup: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarGroupContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarGroupLabel: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarHeader: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarMenuButton: ({
    children,
    isActive,
    render,
  }: {
    children: React.ReactNode;
    isActive?: boolean;
    render?: React.ReactElement<{ href?: string }>;
  }) => (
    <button
      type="button"
      data-active={isActive ? 'true' : undefined}
      data-href={render?.props.href}
    >
      {children}
    </button>
  ),
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarRail: () => null,
}));
vi.mock('@goosar/ui/components/ui/dropdown-menu', () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuGroup: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuLabel: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuSeparator: () => null,
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
}));
vi.mock('@goosar/ui/components/ui/collapsible', () => ({
  Collapsible: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  CollapsibleContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  CollapsibleTrigger: () => <button type="button" />,
}));
vi.mock('@goosar/ui/components/ui/tooltip', () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => (
    <button type="button">{children}</button>
  ),
}));
vi.mock('../common/use-app-foreground', () => ({
  useAppForeground: () => appForeground.current,
}));
vi.mock('./help-launcher', () => ({ HelpLauncher: () => null }));
vi.mock('../auth', () => ({ useLogout: () => vi.fn() }));
vi.mock('../issues/components/status-icon', () => ({ StatusIcon: () => <span /> }));
vi.mock('../navigation', () => ({
  AppLink: ({ children, href }: { children: React.ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
  useNavigation: () => ({ pathname: navigation.current.pathname, push: vi.fn() }),
}));
vi.mock('../projects/components/project-icon', () => ({ ProjectIcon: () => <span /> }));
vi.mock('../workspace/workspace-avatar', () => ({ WorkspaceAvatar: () => <span /> }));
vi.mock('@goosar/ui/components/common/actor-avatar', () => ({ ActorAvatar: () => <span /> }));

vi.mock('@goosar/core/auth', () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: 'user-1' } }),
}));
vi.mock('@goosar/core/chat', () => ({
  useChatStore: Object.assign(
    (selector: (state: { activeSessionId: string | null; isOpen: boolean }) => unknown) =>
      selector(chatStore.current),
    { getState: () => chatStore.current },
  ),
}));
vi.mock('@goosar/core/paths', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/paths')>()),
  paths: { workspace: (slug: string) => ({ issues: () => `/${slug}/issues` }) },
  useCurrentWorkspace: () => ({ id: 'ws-1', name: 'Acme', slug: 'acme' }),
  useWorkspacePaths: () => ({
    inbox: () => '/acme/inbox',
    chat: () => '/acme/chat',
    myIssues: () => '/acme/my-issues',
    issues: () => '/acme/issues',
    projects: () => '/acme/projects',
    autopilots: () => '/acme/autopilots',
    agents: () => '/acme/agents',
    squads: () => '/acme/squads',
    usage: () => '/acme/usage',
    runtimes: () => '/acme/runtimes',
    skills: () => '/acme/skills',
    settings: () => '/acme/settings',
    issueDetail: (id: string) => `/acme/issues/${id}`,
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
}));
vi.mock('@goosar/core/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/api')>();
  return {
    ...actual,
    api: {
      ...actual.api,
      getBaseUrl: () => 'http://127.0.0.1:8080',
    },
  };
});
vi.mock('@goosar/core/inbox/queries', () => ({
  deduplicateInboxItems: (items: unknown[]) => items,
  inboxKeys: { list: () => ['inbox'], unreadSummary: () => ['inbox', 'unread-summary'] },
  inboxUnreadSummaryOptions: () => ({ queryKey: ['inbox', 'unread-summary'] }),
  hasOtherWorkspaceUnread: (
    entries: { workspace_id: string; count: number }[],
    currentWsId: string | null,
  ) => entries.some((s) => s.workspace_id !== currentWsId && s.count > 0),
  unreadWorkspaceIds: (entries: { workspace_id: string; count: number }[]) =>
    new Set(entries.filter((s) => s.count > 0).map((s) => s.workspace_id)),
}));
vi.mock('@goosar/core/issues/queries', () => ({
  issueDetailOptions: () => ({ queryKey: ['issue'] }),
}));
vi.mock('@goosar/core/issues/stores/create-mode-store', () => ({
  useCreateModeStore: { getState: () => ({ lastMode: 'agent' }) },
  openCreateIssueWithPreference: vi.fn(),
}));
vi.mock('@goosar/core/issues/stores/draft-store', () => ({ useIssueDraftStore: () => false }));
vi.mock('@goosar/core/modals', () => ({
  useModalStore: { getState: () => ({ modal: null, open: vi.fn() }) },
}));
vi.mock('@goosar/core/pins/mutations', () => ({
  useDeletePin: () => ({ mutate: deletePin }),
  useReorderPins: () => ({ mutate: vi.fn() }),
}));
vi.mock('@goosar/core/pins/queries', () => ({ pinListOptions: () => ({ queryKey: ['pins'] }) }));
vi.mock('@goosar/core/projects/queries', () => ({
  projectDetailOptions: () => ({ queryKey: ['project'] }),
}));
vi.mock('@goosar/core/workspace/queries', () => ({
  myInvitationListOptions: () => ({ queryKey: ['invitations'] }),
  workspaceKeys: { myInvitations: () => ['invitations'] },
  workspaceListOptions: () => ({ queryKey: ['workspaces'] }),
}));
vi.mock('@goosar/core/realtime', () => ({
  useRealtimeConnectionState: () => realtime.current.state,
  useRealtimePollingInterval: (ms: number) => (realtime.current.state === 'degraded' ? ms : false),
  BACKGROUND_DEGRADED_POLL_INTERVAL_MS: 60_000,
}));
vi.mock('@tanstack/react-query', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-query')>()),
  useMutation: () => ({ isPending: false, mutate: vi.fn() }),
  useQuery: ({
    queryKey,
    refetchInterval,
  }: {
    queryKey: readonly unknown[];
    refetchInterval?: unknown;
  }) => {
    if (queryKey[0] === 'pins') return { data: pins.current };
    if (queryKey[0] === 'issue') return detail.current;
    if (queryKey[0] === 'inbox' && queryKey[1] === 'unread-summary') {
      realtime.current.captured.unreadSummary = refetchInterval;
      return { data: summary.current };
    }
    if (queryKey[0] === 'inbox') {
      realtime.current.captured.inboxList = refetchInterval;
      return { data: inboxItems.current };
    }
    if (queryKey[0] === 'workspaces') return { data: workspaces.current };
    if (queryKey[0] === 'chat' && queryKey[2] === 'sessions') {
      realtime.current.captured.chatSessions = refetchInterval;
      return { data: chatSessions.current };
    }
    return { data: [] };
  },
  useQueryClient: () => ({ fetchQuery: vi.fn(), invalidateQueries: vi.fn() }),
}));

describe('PinRow', () => {
  beforeEach(() => {
    deletePin.mockReset();
    navigation.current.pathname = '/acme/issues';
    detail.current = { isPending: false, isError: false, data: null, error: null };
    summary.current = [];
    workspaces.current = [];
  });

  it('unpins missing details', async () => {
    detail.current = {
      isPending: false,
      isError: true,
      data: null,
      error: new ApiError('missing', 404, 'Not Found'),
    };
    render(<AppSidebar />);
    await waitFor(() => expect(deletePin).toHaveBeenCalledTimes(1));
  });

  it('ignores non-404 errors', async () => {
    detail.current = {
      isPending: false,
      isError: true,
      data: null,
      error: new ApiError('error', 500, 'Server Error'),
    };
    render(<AppSidebar />);
    await waitFor(() => expect(deletePin).not.toHaveBeenCalled());
  });

  it('renders loaded details', async () => {
    detail.current = {
      isPending: false,
      isError: false,
      data: { identifier: 'MUL-123', title: 'Keep this pin', status: 'todo' },
      error: null,
    };
    render(<AppSidebar />);
    expect(await screen.findByText('Keep this pin')).toBeInTheDocument();
    expect(screen.queryByText('MUL-123 Keep this pin')).not.toBeInTheDocument();
  });

  it('does not also highlight the parent workspace nav for an active pin', async () => {
    navigation.current.pathname = '/acme/issues/issue-1';
    detail.current = {
      isPending: false,
      isError: false,
      data: { identifier: 'MUL-123', title: 'Keep this pin', status: 'todo' },
      error: null,
    };

    const { container } = render(<AppSidebar />);

    expect((await screen.findByText('Keep this pin')).closest('button')).toHaveAttribute(
      'data-active',
      'true',
    );
    expect(container.querySelector('button[data-href="/acme/issues"]')).not.toHaveAttribute(
      'data-active',
    );
  });
});

describe('workspace-switcher unread dot', () => {
  beforeEach(() => {
    summary.current = [];
    workspaces.current = [];
  });

  const dot = (container: HTMLElement) => container.querySelector('span.bg-brand.ring-sidebar');

  it('shows a dot when another workspace has unread inbox items', () => {
    summary.current = [{ workspace_id: 'ws-2', count: 3 }];
    const { container } = render(<AppSidebar />);
    expect(dot(container)).not.toBeNull();
  });

  it('does not show a dot when only the active workspace has unread', () => {
    summary.current = [{ workspace_id: 'ws-1', count: 3 }];
    const { container } = render(<AppSidebar />);
    expect(dot(container)).toBeNull();
  });

  it('does not show a dot when no workspace has unread', () => {
    summary.current = [];
    const { container } = render(<AppSidebar />);
    expect(dot(container)).toBeNull();
  });
});

describe('workspace-switcher dropdown per-workspace dot', () => {
  beforeEach(() => {
    summary.current = [];
    workspaces.current = [
      { id: 'ws-1', name: 'Active WS', slug: 'active', avatar_url: null },
      { id: 'ws-2', name: 'Other WS', slug: 'other', avatar_url: null },
    ];
  });

  const rowDots = (container: HTMLElement) =>
    container.querySelectorAll('span.bg-brand:not(.ring-sidebar)');

  it('dots the specific other workspace that has unread', () => {
    summary.current = [{ workspace_id: 'ws-2', count: 3 }];
    const { container } = render(<AppSidebar />);
    expect(rowDots(container)).toHaveLength(1);
    expect(screen.getByText('Other WS').nextElementSibling?.className).toContain('bg-brand');
    expect(screen.getByText('Active WS').nextElementSibling?.className ?? '').not.toContain(
      'bg-brand',
    );
  });

  it('does not dot a workspace whose unread count is zero', () => {
    summary.current = [{ workspace_id: 'ws-2', count: 0 }];
    const { container } = render(<AppSidebar />);
    expect(rowDots(container)).toHaveLength(0);
  });

  it('never dots the active workspace even when it has unread', () => {
    summary.current = [{ workspace_id: 'ws-1', count: 5 }];
    const { container } = render(<AppSidebar />);
    expect(rowDots(container)).toHaveLength(0);
  });
});

describe('personal nav — Chat', () => {
  beforeEach(() => {
    chatSessions.current = [];
    inboxItems.current = [];
    navigation.current = { pathname: '/acme/issues' };
    chatStore.current = { activeSessionId: null, isOpen: false, floatingChatEnabled: true };
    appForeground.current = true;
  });

  const chatNav = (container: HTMLElement) =>
    container.querySelector<HTMLElement>('button[data-href="/acme/chat"]');
  const chatBadge = (container: HTMLElement) =>
    chatNav(container)?.querySelector('number-flow-react') ?? null;

  it('keeps persistent Inbox and Chat counters static', () => {
    inboxItems.current = [{ id: 'inbox-1', read: false }];
    chatSessions.current = [{ id: 'chat-1', unread_count: 2 }];
    const { container } = render(<AppSidebar />);
    const inboxBadge = container
      .querySelector<HTMLElement>('button[data-href="/acme/inbox"]')
      ?.querySelector('number-flow-react') as (HTMLElement & { animated?: boolean }) | null;
    const currentChatBadge = chatBadge(container) as (HTMLElement & { animated?: boolean }) | null;

    expect(inboxBadge?.animated).toBe(false);
    expect(currentChatBadge?.animated).toBe(false);
  });

  it('renders a Chat nav link to the workspace chat route', () => {
    const { container } = render(<AppSidebar />);
    expect(chatNav(container)).not.toBeNull();
  });

  it('badges the Chat nav with the summed unread_count of chat sessions', () => {
    chatSessions.current = [
      { id: 'a', unread_count: 3 },
      { id: 'b', unread_count: 2 },
      { id: 'c', unread_count: 0 },
    ];
    const { container } = render(<AppSidebar />);
    expect(chatBadge(container)).toHaveAttribute('aria-label', '5');
  });

  it('shows no Chat unread badge when every session is read', () => {
    chatSessions.current = [{ id: 'a', unread_count: 0 }, { id: 'b' }];
    const { container } = render(<AppSidebar />);
    expect(chatBadge(container)).toBeNull();
  });

  it('excludes the session being viewed on the chat page from the badge', () => {
    chatSessions.current = [
      { id: 'a', unread_count: 2 },
      { id: 'b', unread_count: 3 },
    ];
    navigation.current = { pathname: '/acme/chat' };
    chatStore.current = { activeSessionId: 'a', isOpen: false, floatingChatEnabled: true };
    const { container } = render(<AppSidebar />);
    expect(chatBadge(container)).toHaveAttribute('aria-label', '3');
  });

  it('excludes the viewed session when the floating chat window is open off-route', () => {
    chatSessions.current = [
      { id: 'a', unread_count: 2 },
      { id: 'b', unread_count: 3 },
    ];
    navigation.current = { pathname: '/acme/issues' };
    chatStore.current = { activeSessionId: 'a', isOpen: true, floatingChatEnabled: true };
    const { container } = render(<AppSidebar />);
    expect(chatBadge(container)).toHaveAttribute('aria-label', '3');
  });

  it('still counts a remembered selection when no chat surface is showing it', () => {
    chatSessions.current = [
      { id: 'a', unread_count: 2 },
      { id: 'b', unread_count: 3 },
    ];
    navigation.current = { pathname: '/acme/issues' };
    chatStore.current = { activeSessionId: 'a', isOpen: false, floatingChatEnabled: true };
    const { container } = render(<AppSidebar />);
    expect(chatBadge(container)).toHaveAttribute('aria-label', '5');
  });

  it('counts the active session while the floating window is open but the app is backgrounded', () => {
    chatSessions.current = [
      { id: 'a', unread_count: 2 },
      { id: 'b', unread_count: 3 },
    ];
    navigation.current = { pathname: '/acme/issues' };
    chatStore.current = { activeSessionId: 'a', isOpen: true, floatingChatEnabled: true };
    appForeground.current = false;
    const { container } = render(<AppSidebar />);
    expect(chatBadge(container)).toHaveAttribute('aria-label', '5');
  });

  it('counts the active session on the chat route while the app is backgrounded', () => {
    chatSessions.current = [
      { id: 'a', unread_count: 2 },
      { id: 'b', unread_count: 3 },
    ];
    navigation.current = { pathname: '/acme/chat' };
    chatStore.current = { activeSessionId: 'a', isOpen: false, floatingChatEnabled: true };
    appForeground.current = false;
    const { container } = render(<AppSidebar />);
    expect(chatBadge(container)).toHaveAttribute('aria-label', '5');
  });
});

describe('AppSidebar inbox badges — degraded-mode REST polling (#257)', () => {
  beforeEach(() => {
    navigation.current = { pathname: '/acme/issues' };
    detail.current = { isPending: false, isError: false, data: null, error: null };
    summary.current = [];
    workspaces.current = [];
    chatSessions.current = [];
    chatStore.current = { activeSessionId: null, isOpen: false, floatingChatEnabled: true };
    appForeground.current = true;
    realtime.current.state = 'connected';
    realtime.current.captured = {
      inboxList: undefined,
      unreadSummary: undefined,
      chatSessions: undefined,
    };
  });

  it('does not poll while the realtime connection is healthy', () => {
    render(<AppSidebar />);
    expect(realtime.current.captured.inboxList).toBe(false);
    expect(realtime.current.captured.unreadSummary).toBe(false);
  });

  it('polls both inbox caches on the background cadence while degraded', () => {
    realtime.current.state = 'degraded';
    render(<AppSidebar />);
    expect(realtime.current.captured.inboxList).toBe(60_000);
    expect(realtime.current.captured.unreadSummary).toBe(60_000);
  });

  it('yields the inbox list timer to the Inbox page on the inbox route', () => {
    realtime.current.state = 'degraded';
    navigation.current = { pathname: '/acme/inbox' };
    render(<AppSidebar />);
    expect(realtime.current.captured.inboxList).toBe(false);
    expect(realtime.current.captured.unreadSummary).toBe(60_000);
  });

  it('polls the chat sessions cache while degraded when no chat surface owns it', () => {
    realtime.current.state = 'degraded';
    chatStore.current = {
      activeSessionId: null,
      isOpen: false,
      floatingChatEnabled: false,
    };
    render(<AppSidebar />);
    expect(realtime.current.captured.chatSessions).toBe(60_000);
  });

  it('does not poll the chat sessions cache on a healthy connection', () => {
    chatStore.current = {
      activeSessionId: null,
      isOpen: false,
      floatingChatEnabled: false,
    };
    render(<AppSidebar />);
    expect(realtime.current.captured.chatSessions).toBe(false);
  });

  it('yields the chat sessions timer to the floating overlay when it is mounted', () => {
    realtime.current.state = 'degraded';
    chatStore.current = {
      activeSessionId: null,
      isOpen: false,
      floatingChatEnabled: true,
    };
    render(<AppSidebar />);
    expect(realtime.current.captured.chatSessions).toBe(false);
  });

  it('yields the chat sessions timer to the chat page on the chat route', () => {
    realtime.current.state = 'degraded';
    navigation.current = { pathname: '/acme/chat' };
    chatStore.current = {
      activeSessionId: null,
      isOpen: false,
      floatingChatEnabled: false,
    };
    render(<AppSidebar />);
    expect(realtime.current.captured.chatSessions).toBe(false);
  });

  it('does not render the realtime indicator itself, even while degraded', () => {
    realtime.current.state = 'degraded';
    const { container } = render(<AppSidebar />);
    expect(container.querySelector('[data-testid="realtime-status-indicator"]')).toBeNull();
  });
});
