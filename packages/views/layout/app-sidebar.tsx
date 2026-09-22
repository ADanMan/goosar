'use client';

import React, { useCallback, useEffect, useRef, useState } from 'react';
import { cn } from '@goosar/ui/lib/utils';
import { useScrollFade } from '@goosar/ui/hooks/use-scroll-fade';
import { AppLink, useNavigation } from '../navigation';
import { HelpLauncher } from './help-launcher';
import {
  DndContext,
  PointerSensor,
  useSensor,
  useSensors,
  closestCenter,
  type DragEndEvent,
} from '@dnd-kit/core';
import {
  SortableContext,
  verticalListSortingStrategy,
  useSortable,
  arrayMove,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { ChevronDown, ChevronRight, LogOut, Plus, Check, SquarePen, X } from 'lucide-react';
import { WorkspaceAvatar } from '../workspace/workspace-avatar';
import { ActorAvatar } from '@goosar/ui/components/common/actor-avatar';
import { Tooltip, TooltipTrigger, TooltipContent } from '@goosar/ui/components/ui/tooltip';
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from '@goosar/ui/components/ui/collapsible';
import { CappedNumberFlow } from '@goosar/ui/components/ui/number-flow';
import { StatusIcon } from '../issues/components/status-icon';
import { useIssueDraftStore } from '@goosar/core/issues/stores/draft-store';
import { openCreateIssueWithPreference } from '@goosar/core/issues/stores/create-mode-store';
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from '@goosar/ui/components/ui/sidebar';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@goosar/ui/components/ui/dropdown-menu';
import { useAuthStore } from '@goosar/core/auth';
import { useCurrentWorkspace, useWorkspacePaths, paths } from '@goosar/core/paths';
import {
  workspaceListOptions,
  myInvitationListOptions,
  workspaceKeys,
} from '@goosar/core/workspace/queries';
import { resolvePublicFileUrl } from '@goosar/core/workspace/avatar-url';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  inboxKeys,
  deduplicateInboxItems,
  inboxUnreadSummaryOptions,
  hasOtherWorkspaceUnread,
  unreadWorkspaceIds,
} from '@goosar/core/inbox/queries';
import { chatSessionsOptions } from '@goosar/core/chat/queries';
import { countUnreadChatMessages } from '@goosar/core/chat/unread';
import { useChatStore } from '@goosar/core/chat';
import { api, ApiError } from '@goosar/core/api';
import { useModalStore } from '@goosar/core/modals';
import { useConfigStore } from '@goosar/core/config';
import { pinListOptions } from '@goosar/core/pins/queries';
import { useDeletePin, useReorderPins } from '@goosar/core/pins/mutations';
import { issueDetailOptions } from '@goosar/core/issues/queries';
import { projectDetailOptions } from '@goosar/core/projects/queries';
import type { PinnedItem } from '@goosar/core/types';
import { useLogout } from '../auth';
import { ProjectIcon } from '../projects/components/project-icon';
import { routeIconForPath } from './route-icon-components';
import { useT } from '../i18n';
import { useShortcut } from '@goosar/core/shortcuts';
import { ShortcutKeycaps } from '../common/shortcut-keycaps';
import { useAppForeground } from '../common/use-app-foreground';
import {
  useRealtimePollingInterval,
  BACKGROUND_DEGRADED_POLL_INTERVAL_MS,
} from '@goosar/core/realtime';

function isNavActive(pathname: string, href: string): boolean {
  return pathname === href || pathname.startsWith(href + '/');
}

const EMPTY_PINS: PinnedItem[] = [];
const EMPTY_WORKSPACES: Awaited<ReturnType<typeof api.listWorkspaces>> = [];
const EMPTY_INVITATIONS: Awaited<ReturnType<typeof api.listMyInvitations>> = [];
const EMPTY_INBOX: Awaited<ReturnType<typeof api.listInbox>> = [];
const EMPTY_INBOX_SUMMARY: Awaited<ReturnType<typeof api.getInboxUnreadSummary>> = [];
const EMPTY_CHAT_SESSIONS: Awaited<ReturnType<typeof api.listChatSessions>> = [];

type NavKey =
  | 'inbox'
  | 'chat'
  | 'myIssues'
  | 'issues'
  | 'projects'
  | 'autopilots'
  | 'agents'
  | 'squads'
  | 'usage'
  | 'runtimes'
  | 'skills'
  | 'settings';

type NavLabelKey =
  | 'inbox'
  | 'chat'
  | 'my_issues'
  | 'issues'
  | 'projects'
  | 'autopilots'
  | 'agents'
  | 'squads'
  | 'usage'
  | 'runtimes'
  | 'skills'
  | 'settings';

const personalNav: { key: NavKey; labelKey: NavLabelKey }[] = [
  { key: 'inbox', labelKey: 'inbox' },
  { key: 'chat', labelKey: 'chat' },
  { key: 'myIssues', labelKey: 'my_issues' },
];

const workspaceNav: { key: NavKey; labelKey: NavLabelKey }[] = [
  { key: 'issues', labelKey: 'issues' },
  { key: 'projects', labelKey: 'projects' },
  { key: 'autopilots', labelKey: 'autopilots' },
  { key: 'agents', labelKey: 'agents' },
  { key: 'squads', labelKey: 'squads' },
  { key: 'usage', labelKey: 'usage' },
];

const configureNav: { key: NavKey; labelKey: NavLabelKey }[] = [
  { key: 'runtimes', labelKey: 'runtimes' },
  { key: 'skills', labelKey: 'skills' },
  { key: 'settings', labelKey: 'settings' },
];

function DraftDot() {
  const hasDraft = useIssueDraftStore((s) => s.hasDraft());
  if (!hasDraft) return null;
  return <span className="absolute top-0 right-0 size-1.5 rounded-full bg-brand" />;
}

function SortablePinItem({
  pin,
  href,
  pathname,
  onUnpin,
  label,
  iconNode,
}: {
  pin: PinnedItem;
  href: string;
  pathname: string;
  onUnpin: () => void;
  label: string;
  iconNode: React.ReactNode;
}) {
  const { t } = useT('layout');
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: pin.id,
  });
  const wasDragged = useRef(false);

  useEffect(() => {
    if (isDragging) wasDragged.current = true;
  }, [isDragging]);

  const style = { transform: CSS.Transform.toString(transform), transition };
  const isActive = pathname === href;

  return (
    <SidebarMenuItem
      ref={setNodeRef}
      style={style}
      className={cn('group/pin', isDragging && 'opacity-30')}
      {...attributes}
      {...listeners}
    >
      <SidebarMenuButton
        size="sm"
        isActive={isActive}
        render={<AppLink href={href} draggable={false} />}
        onClick={(event) => {
          if (wasDragged.current) {
            wasDragged.current = false;
            event.preventDefault();
            return;
          }
        }}
        className={cn(
          'text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground',
          isDragging && 'pointer-events-none',
        )}
      >
        {iconNode}
        <span
          className="min-w-0 flex-1 overflow-hidden whitespace-nowrap"
          style={{
            maskImage: 'linear-gradient(to right, black calc(100% - 12px), transparent)',
            WebkitMaskImage: 'linear-gradient(to right, black calc(100% - 12px), transparent)',
          }}
        >
          {label}
        </span>
        <Tooltip>
          <TooltipTrigger
            render={<span role="button" />}
            className="hidden size-2.5 shrink-0 items-center justify-center rounded-sm text-muted-foreground group-hover/pin:flex hover:text-foreground"
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              onUnpin();
            }}
          >
            <X className="size-1" />
          </TooltipTrigger>
          <TooltipContent side="top" sideOffset={4}>
            {t(($) => $.sidebar.unpin_tooltip)}
          </TooltipContent>
        </Tooltip>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}

function PinRow({
  pin,
  href,
  pathname,
  onUnpin,
  wsId,
}: {
  pin: PinnedItem;
  href: string;
  pathname: string;
  onUnpin: () => void;
  wsId: string;
}) {
  const isIssue = pin.item_type === 'issue';
  const issueQuery = useQuery({
    ...issueDetailOptions(wsId, pin.item_id),
    enabled: isIssue,
  });
  const projectQuery = useQuery({
    ...projectDetailOptions(wsId, pin.item_id),
    enabled: !isIssue,
  });

  const triggeredRef = useRef(false);
  useEffect(() => {
    const err = isIssue ? issueQuery.error : projectQuery.error;
    if (err instanceof ApiError && err.status === 404 && !triggeredRef.current) {
      triggeredRef.current = true;
      onUnpin();
    }
  }, [isIssue, issueQuery.error, onUnpin, projectQuery.error]);

  if (isIssue) {
    if (issueQuery.isPending) return <PinSkeleton />;
    if (issueQuery.isError || !issueQuery.data) return null;
    const issue = issueQuery.data;
    const label = issue.title;
    const iconNode = (
      <StatusIcon status={issue.status} className="!size-3.5 shrink-0" />
    );
    return (
      <SortablePinItem
        pin={pin}
        href={href}
        pathname={pathname}
        onUnpin={onUnpin}
        label={label}
        iconNode={iconNode}
      />
    );
  }

  if (projectQuery.isPending) return <PinSkeleton />;
  if (projectQuery.isError || !projectQuery.data) return null;
  const project = projectQuery.data;
  const iconNode = <ProjectIcon project={project} size="sm" />;
  return (
    <SortablePinItem
      pin={pin}
      href={href}
      pathname={pathname}
      onUnpin={onUnpin}
      label={project.title}
      iconNode={iconNode}
    />
  );
}

function PinSkeleton() {
  return (
    <SidebarMenuItem>
      <div className="flex h-7 w-full items-center gap-2 px-2">
        <div className="size-3.5 shrink-0 rounded-sm bg-sidebar-accent/40" />
        <div className="h-3 w-24 rounded bg-sidebar-accent/40" />
      </div>
    </SidebarMenuItem>
  );
}

interface AppSidebarProps {
  topSlot?: React.ReactNode;
  searchSlot?: React.ReactNode;
  footerSlot?: React.ReactNode;
  headerClassName?: string;
  headerStyle?: React.CSSProperties;
}

export function AppSidebar({
  topSlot,
  searchSlot,
  footerSlot,
  headerClassName,
  headerStyle,
}: AppSidebarProps = {}) {
  const { t } = useT('layout');
  const { pathname, push } = useNavigation();
  const user = useAuthStore((s) => s.user);
  const userId = useAuthStore((s) => s.user?.id);
  const logout = useLogout();
  const workspace = useCurrentWorkspace();
  const p = useWorkspacePaths();
  const { data: workspaces = EMPTY_WORKSPACES } = useQuery(workspaceListOptions());
  const { data: myInvitations = EMPTY_INVITATIONS } = useQuery(myInvitationListOptions());
  const workspaceCreationDisabled = useConfigStore((s) => s.workspaceCreationDisabled);

  const wsId = workspace?.id;
  const badgePollingInterval = useRealtimePollingInterval(BACKGROUND_DEGRADED_POLL_INTERVAL_MS);
  const inboxRouteActive = isNavActive(pathname, p.inbox());
  const { data: inboxItems = EMPTY_INBOX } = useQuery({
    queryKey: wsId ? inboxKeys.list(wsId) : ['inbox', 'disabled'],
    queryFn: () => api.listInbox(),
    enabled: !!wsId,
    refetchInterval: inboxRouteActive ? false : badgePollingInterval,
  });
  const unreadCount = React.useMemo(
    () => deduplicateInboxItems(inboxItems).filter((i) => !i.read).length,
    [inboxItems],
  );
  const floatingChatEnabled = useChatStore((s) => s.floatingChatEnabled);
  const chatHref = p.chat();
  const chatRouteActive = isNavActive(pathname, chatHref);
  const chatSurfaceOwnsSessions = chatRouteActive || floatingChatEnabled;
  const { data: chatSessions = EMPTY_CHAT_SESSIONS } = useQuery({
    ...chatSessionsOptions(wsId ?? ''),
    enabled: !!wsId,
    refetchInterval: chatSurfaceOwnsSessions ? false : badgePollingInterval,
  });
  const activeChatSessionId = useChatStore((s) => s.activeSessionId);
  const floatingChatOpen = useChatStore((s) => s.isOpen);
  const appForeground = useAppForeground();
  const viewedChatSessionId =
    appForeground && (floatingChatOpen || chatRouteActive) ? activeChatSessionId : null;
  const chatUnreadCount = React.useMemo(
    () => countUnreadChatMessages(chatSessions, viewedChatSessionId),
    [chatSessions, viewedChatSessionId],
  );
  const { data: unreadSummary = EMPTY_INBOX_SUMMARY } = useQuery({
    ...inboxUnreadSummaryOptions(),
    enabled: !!wsId,
    refetchInterval: badgePollingInterval,
  });
  const otherWorkspaceUnread = React.useMemo(
    () => hasOtherWorkspaceUnread(unreadSummary, wsId),
    [unreadSummary, wsId],
  );
  const unreadWsIds = React.useMemo(() => unreadWorkspaceIds(unreadSummary), [unreadSummary]);
  const { data: pinnedItems = EMPTY_PINS } = useQuery({
    ...pinListOptions(wsId ?? '', userId ?? ''),
    enabled: !!wsId && !!userId,
  });
  const deletePin = useDeletePin();
  const reorderPins = useReorderPins();
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const sidebarScrollRef = useRef<HTMLDivElement>(null);
  const sidebarFadeStyle = useScrollFade(sidebarScrollRef, 24);
  const getPinHref = useCallback(
    (pin: PinnedItem) =>
      pin.item_type === 'issue' ? p.issueDetail(pin.item_id) : p.projectDetail(pin.item_id),
    [p],
  );

  const [localPinned, setLocalPinned] = useState<PinnedItem[]>(pinnedItems);
  const [localPinnedWsId, setLocalPinnedWsId] = useState<string | null>(wsId ?? null);
  const isDraggingRef = useRef(false);
  useEffect(() => {
    if (!isDraggingRef.current) {
      setLocalPinned(pinnedItems);
    }
  }, [pinnedItems]);
  useEffect(() => {
    setLocalPinnedWsId(wsId ?? null);
  }, [wsId]);
  const visiblePinned = localPinnedWsId === (wsId ?? null) ? localPinned : EMPTY_PINS;
  const isActivePinnedRoute = visiblePinned.some((pin) => pathname === getPinHref(pin));

  const handleDragStart = useCallback(() => {
    isDraggingRef.current = true;
  }, []);
  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      isDraggingRef.current = false;
      const { active, over } = event;
      if (!over || active.id === over.id) return;
      const oldIndex = localPinned.findIndex((p) => p.id === active.id);
      const newIndex = localPinned.findIndex((p) => p.id === over.id);
      if (oldIndex === -1 || newIndex === -1) return;
      const reordered = arrayMove(localPinned, oldIndex, newIndex);
      setLocalPinned(reordered);
      reorderPins.mutate(reordered);
    },
    [localPinned, reorderPins],
  );

  const queryClient = useQueryClient();
  const acceptInvitationMut = useMutation({
    mutationFn: (id: string) => api.acceptInvitation(id),
    onSuccess: async (_, invitationId) => {
      const invitation = myInvitations.find((i) => i.id === invitationId);
      queryClient.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
      const list = await queryClient.fetchQuery({
        ...workspaceListOptions(),
        staleTime: 0,
      });
      const joined = invitation ? list.find((w) => w.id === invitation.workspace_id) : null;
      if (joined) {
        push(paths.workspace(joined.slug).issues());
      }
    },
  });
  const declineInvitationMut = useMutation({
    mutationFn: (id: string) => api.declineInvitation(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
    },
  });

  const createIssueShortcut = useShortcut('createIssue');

  return (
    <Sidebar variant="inset">
      {topSlot}
      {/* Workspace Switcher */}
      <SidebarHeader className={cn('py-3', headerClassName)} style={headerStyle}>
        <SidebarMenu>
          <SidebarMenuItem>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <SidebarMenuButton>
                    <span className="relative">
                      <WorkspaceAvatar
                        name={workspace?.name ?? 'M'}
                        avatarUrl={workspace?.avatar_url}
                        size="sm"
                      />
                      {/* Shared brand dot: a pending invitation OR another
                            workspace with unread inbox items. The active
                            workspace's own unread stays on the Inbox nav count
                            (below), so it is deliberately excluded here. */}
                      {(myInvitations.length > 0 || otherWorkspaceUnread) && (
                        <span className="absolute -top-0.5 -right-0.5 size-2 rounded-full bg-brand ring-1 ring-sidebar" />
                      )}
                    </span>
                    <span className="flex-1 truncate font-medium">
                      {workspace?.name ?? 'Goosar'}
                    </span>
                    <ChevronDown className="size-3 text-muted-foreground" />
                  </SidebarMenuButton>
                }
              />
              <DropdownMenuContent
                className="w-auto min-w-56"
                align="start"
                side="bottom"
                sideOffset={4}
              >
                <div className="flex items-center gap-2.5 px-2 py-1.5">
                  <ActorAvatar
                    name={user?.name ?? ''}
                    initials={(user?.name ?? 'U').charAt(0).toUpperCase()}
                    avatarUrl={resolvePublicFileUrl(user?.avatar_url)}
                    size="lg"
                  />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium leading-tight">{user?.name}</p>
                    <p className="truncate text-xs text-muted-foreground leading-tight">
                      {user?.email}
                    </p>
                  </div>
                </div>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  <DropdownMenuLabel className="text-xs text-muted-foreground">
                    {t(($) => $.sidebar.workspaces_label)}
                  </DropdownMenuLabel>
                  {workspaces.map((ws) => (
                    <DropdownMenuItem
                      key={ws.id}
                      render={<AppLink href={paths.workspace(ws.slug).issues()} />}
                    >
                      <WorkspaceAvatar name={ws.name} avatarUrl={ws.avatar_url} size="sm" />
                      <span className="flex-1 truncate">{ws.name}</span>
                      {/* Points at the specific workspace holding unread
                            inbox items. Sits in the same right-edge slot as the
                            active-workspace check; the active workspace is
                            excluded (its unread is the Inbox nav count), so dot
                            and check never collide on one row. */}
                      {ws.id !== workspace?.id && unreadWsIds.has(ws.id) && (
                        <span className="size-2 rounded-full bg-brand" />
                      )}
                      {ws.id === workspace?.id && <Check className="h-3.5 w-3.5 text-primary" />}
                    </DropdownMenuItem>
                  ))}
                  {!workspaceCreationDisabled && (
                    <DropdownMenuItem
                      onClick={() => useModalStore.getState().open('create-workspace')}
                    >
                      <Plus className="h-3.5 w-3.5" />
                      {t(($) => $.sidebar.create_workspace)}
                    </DropdownMenuItem>
                  )}
                </DropdownMenuGroup>
                {myInvitations.length > 0 && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuGroup>
                      <DropdownMenuLabel className="text-xs text-muted-foreground">
                        {t(($) => $.sidebar.pending_invitations_label)}
                      </DropdownMenuLabel>
                      {myInvitations.map((inv) => (
                        <div key={inv.id} className="flex items-center gap-2 px-2 py-1.5">
                          <WorkspaceAvatar name={inv.workspace_name ?? 'W'} size="sm" />
                          <span className="flex-1 truncate text-sm">
                            {inv.workspace_name ??
                              t(($) => $.sidebar.invitation_workspace_fallback)}
                          </span>
                          <button
                            type="button"
                            className="text-xs px-2 py-0.5 rounded bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
                            disabled={acceptInvitationMut.isPending}
                            onClick={(e) => {
                              e.stopPropagation();
                              acceptInvitationMut.mutate(inv.id);
                            }}
                          >
                            {t(($) => $.sidebar.invitation_join)}
                          </button>
                          <button
                            type="button"
                            className="text-xs px-2 py-0.5 rounded bg-muted text-muted-foreground hover:bg-muted/80 disabled:opacity-50"
                            disabled={declineInvitationMut.isPending}
                            onClick={(e) => {
                              e.stopPropagation();
                              declineInvitationMut.mutate(inv.id);
                            }}
                          >
                            {t(($) => $.sidebar.invitation_decline)}
                          </button>
                        </div>
                      ))}
                    </DropdownMenuGroup>
                  </>
                )}
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  <DropdownMenuItem variant="destructive" onClick={logout}>
                    <LogOut className="h-3.5 w-3.5" />
                    {t(($) => $.sidebar.log_out)}
                  </DropdownMenuItem>
                </DropdownMenuGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          </SidebarMenuItem>
        </SidebarMenu>
        <SidebarMenu>
          {searchSlot && <SidebarMenuItem>{searchSlot}</SidebarMenuItem>}
          <SidebarMenuItem>
            <SidebarMenuButton
              className="text-muted-foreground"
              onClick={() => openCreateIssueWithPreference()}
            >
              <span className="relative">
                <SquarePen />
                <DraftDot />
              </span>
              <span>{t(($) => $.sidebar.new_issue)}</span>
              {createIssueShortcut ? (
                <ShortcutKeycaps
                  shortcut={createIssueShortcut}
                  decorative
                  className="pointer-events-none ml-auto"
                />
              ) : null}
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      {/* Navigation */}
      <SidebarContent ref={sidebarScrollRef} style={sidebarFadeStyle}>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu className="gap-0.5">
              {personalNav.map((item) => {
                const href = p[item.key]();
                const Icon = routeIconForPath(href);
                const isActive = isNavActive(pathname, href);
                return (
                  <SidebarMenuItem key={item.key}>
                    <SidebarMenuButton
                      isActive={isActive}
                      render={<AppLink href={href} />}
                      className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                    >
                      <Icon />
                      <span>{t(($) => $.nav[item.labelKey])}</span>
                      {item.key === 'inbox' && unreadCount > 0 && (
                        <CappedNumberFlow
                          value={unreadCount}
                          animated={false}
                          className="ml-auto text-xs"
                        />
                      )}
                      {item.key === 'chat' && chatUnreadCount > 0 && (
                        <CappedNumberFlow
                          value={chatUnreadCount}
                          animated={false}
                          className="ml-auto text-xs"
                        />
                      )}
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        {visiblePinned.length > 0 && (
          <Collapsible defaultOpen>
            <SidebarGroup className="group/pinned">
              <SidebarGroupLabel
                render={<CollapsibleTrigger />}
                className="group/trigger cursor-pointer hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground"
              >
                <span>{t(($) => $.sidebar.pinned_label)}</span>
                <ChevronRight className="!size-3 ml-1 stroke-[2.5] transition-transform duration-200 group-data-[panel-open]/trigger:rotate-90" />
                <span className="ml-auto text-[10px] text-muted-foreground opacity-0 transition-opacity group-hover/pinned:opacity-100">
                  {visiblePinned.length}
                </span>
              </SidebarGroupLabel>
              <CollapsibleContent>
                <SidebarGroupContent>
                  <DndContext
                    sensors={sensors}
                    collisionDetection={closestCenter}
                    onDragStart={handleDragStart}
                    onDragEnd={handleDragEnd}
                  >
                    <SortableContext
                      items={visiblePinned.map((p) => p.id)}
                      strategy={verticalListSortingStrategy}
                    >
                      <SidebarMenu className="gap-0.5">
                        {visiblePinned.map((pin: PinnedItem) => (
                          <PinRow
                            key={pin.id}
                            pin={pin}
                            href={getPinHref(pin)}
                            pathname={pathname}
                            onUnpin={() =>
                              deletePin.mutate({ itemType: pin.item_type, itemId: pin.item_id })
                            }
                            wsId={wsId ?? ''}
                          />
                        ))}
                      </SidebarMenu>
                    </SortableContext>
                  </DndContext>
                </SidebarGroupContent>
              </CollapsibleContent>
            </SidebarGroup>
          </Collapsible>
        )}

        <SidebarGroup>
          <SidebarGroupLabel>{t(($) => $.sidebar.workspace_group)}</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu className="gap-0.5">
              {workspaceNav.map((item) => {
                const href = p[item.key]();
                const Icon = routeIconForPath(href);
                const isActive = !isActivePinnedRoute && isNavActive(pathname, href);
                return (
                  <SidebarMenuItem key={item.key}>
                    <SidebarMenuButton
                      isActive={isActive}
                      render={<AppLink href={href} />}
                      className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                    >
                      <Icon />
                      <span>{t(($) => $.nav[item.labelKey])}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup>
          <SidebarGroupLabel>{t(($) => $.sidebar.configure_group)}</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu className="gap-0.5">
              {configureNav.map((item) => {
                const href = p[item.key]();
                const Icon = routeIconForPath(href);
                const isActive = isNavActive(pathname, href);
                return (
                  <SidebarMenuItem key={item.key}>
                    <SidebarMenuButton
                      isActive={isActive}
                      render={<AppLink href={href} />}
                      className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
                    >
                      <Icon />
                      <span>{t(($) => $.nav[item.labelKey])}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter className="p-2">
        {/* No realtime indicator here (#257): this sidebar is
              collapsible="offcanvas" and a closed Sheet below the mobile
              breakpoint, so a badge in this footer vanishes precisely when the
              user has hidden the sidebar. The layout shells own that mount —
              DashboardLayout on web, DesktopShell on desktop. */}
        <div className="flex items-center justify-end gap-1">
          {footerSlot}
          <HelpLauncher />
        </div>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
