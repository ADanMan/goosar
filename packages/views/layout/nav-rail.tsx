'use client';

import { Inbox, ListTodo, FolderKanban, Bot, Zap, Settings, LogOut, Plus, Check } from 'lucide-react';
import { AppLink } from '../navigation';
import { ActorAvatar } from '@goosar/ui/components/common/actor-avatar';
import { WorkspaceAvatar } from '../workspace/workspace-avatar';
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
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
import { workspaceListOptions } from '@goosar/core/workspace/queries';
import { resolvePublicFileUrl } from '@goosar/core/workspace/avatar-url';
import { api } from '@goosar/core/api';
import { useQuery } from '@tanstack/react-query';
import { useConfigStore } from '@goosar/core/config';
import { useModalStore } from '@goosar/core/modals';
import { useLogout } from '../auth';
import { useT } from '../i18n';
import {
  NAV_LABEL_KEYS_FOR_BREADCRUMB,
  NAV_SECTIONS,
  navSectionForPath,
  navSectionHref,
  type NavSection,
} from './nav-sections';

const NAV_ICONS: Record<NavSection, typeof Inbox> = {
  feed: Inbox,
  tasks: ListTodo,
  projects: FolderKanban,
  crew: Bot,
  autopilot: Zap,
  settings: Settings,
};

const EMPTY_WORKSPACES: Awaited<ReturnType<typeof api.listWorkspaces>> = [];

interface NavRailProps {
  activeSection: NavSection | null;
  /** Extra small control rendered above the profile avatar in the footer
   * (e.g. desktop's network status indicator). */
  footerExtra?: React.ReactNode;
}

// Рельса навигации (ADR-0002): шесть разделов приложения. 56px, иконки, тёмный
// фон `--rail`. Переключатель воркспейса сверху и профиль снизу переносят
// логику прежнего `app-sidebar.tsx` без списков (избранное/приглашения теперь
// в ContextPanel).
export function NavRail({ activeSection, footerExtra }: NavRailProps) {
  const { t } = useT('layout');
  const user = useAuthStore((s) => s.user);
  const logout = useLogout();
  const workspace = useCurrentWorkspace();
  const p = useWorkspacePaths();
  const { data: workspaces = EMPTY_WORKSPACES } = useQuery(workspaceListOptions());
  const workspaceCreationDisabled = useConfigStore((s) => s.workspaceCreationDisabled);

  return (
    <Sidebar collapsible="none" className="w-14! border-r bg-rail text-sidebar-foreground">
      <SidebarHeader className="items-center py-3">
        <SidebarMenu>
          <SidebarMenuItem>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <SidebarMenuButton
                    tooltip={{ children: workspace?.name ?? 'Goosar', hidden: false }}
                    className="mx-auto size-8 justify-center p-0"
                    aria-label={t(($) => $.sidebar.workspaces_label)}
                  >
                    <WorkspaceAvatar
                      name={workspace?.name ?? 'M'}
                      avatarUrl={workspace?.avatar_url}
                      size="sm"
                    />
                  </SidebarMenuButton>
                }
              />
              <DropdownMenuContent className="w-auto min-w-56" align="start" side="right" sideOffset={8}>
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
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup className="px-0">
          <SidebarGroupContent className="px-1.5">
            <SidebarMenu className="gap-1">
              {NAV_SECTIONS.map((section) => {
                const href = navSectionHref(p, section);
                const Icon = NAV_ICONS[section];
                const label = t(($) => $.nav[NAV_LABEL_KEYS_FOR_BREADCRUMB[section]]);
                const isActive = activeSection === section;
                return (
                  <SidebarMenuItem key={section}>
                    <SidebarMenuButton
                      isActive={isActive}
                      render={<AppLink href={href} />}
                      tooltip={{ children: label, hidden: false }}
                      aria-label={label}
                      className="mx-auto size-10 justify-center p-0 text-muted-foreground data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground hover:not-data-active:bg-sidebar-accent/70"
                    >
                      <Icon />
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter className="items-center gap-2 pb-3">
        {footerExtra}
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              render={<AppLink href={p.settings()} />}
              tooltip={{ children: user?.name ?? '', hidden: false }}
              aria-label={user?.name ?? ''}
              className="mx-auto size-8 justify-center p-0"
            >
              <ActorAvatar
                name={user?.name ?? ''}
                initials={(user?.name ?? 'U').charAt(0).toUpperCase()}
                avatarUrl={resolvePublicFileUrl(user?.avatar_url)}
                size="sm"
              />
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
    </Sidebar>
  );
}

export { navSectionForPath };
export type { NavSection };
