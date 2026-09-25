'use client';

import { useEffect, useState } from 'react';
import { ChevronsLeft, ChevronsRight } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { AppLink, useNavigation } from '../navigation';
import { useAuthStore } from '@goosar/core/auth';
import { useCurrentWorkspace, useWorkspacePaths } from '@goosar/core/paths';
import { pinListOptions } from '@goosar/core/pins/queries';
import { useDeletePin } from '@goosar/core/pins/mutations';
import { issueDetailOptions } from '@goosar/core/issues/queries';
import { projectDetailOptions } from '@goosar/core/projects/queries';
import type { PinnedItem } from '@goosar/core/types';
import { StatusIcon } from '../issues/components/status-icon';
import { ProjectIcon } from '../projects/components/project-icon';
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from '@goosar/ui/components/ui/sidebar';
import { useT } from '../i18n';
import type { NavSection } from './nav-sections';

const COLLAPSE_STORAGE_KEY = 'goosar.contextPanel.collapsed';
const EMPTY_PINS: PinnedItem[] = [];

// ponytail: locales/** is locked to another agent this session (T-005 scope
// note), so these two labels are hardcoded ru strings instead of new i18n
// keys. Fold into layout.json's `sidebar.*` once that lock lifts.
const EXPAND_PANEL_LABEL = 'Развернуть панель';
const COLLAPSE_PANEL_LABEL = 'Свернуть панель';

function readStoredCollapsed(): boolean {
  if (typeof window === 'undefined') return false;
  try {
    return window.localStorage.getItem(COLLAPSE_STORAGE_KEY) === 'true';
  } catch {
    return false;
  }
}

function PanelLink({ href, isActive, children }: { href: string; isActive: boolean; children: React.ReactNode }) {
  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        isActive={isActive}
        render={<AppLink href={href} />}
        className="text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground"
      >
        {children}
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}

function PinRow({ pin, wsId }: { pin: PinnedItem; wsId: string }) {
  const p = useWorkspacePaths();
  const { pathname } = useNavigation();
  const deletePin = useDeletePin();
  void deletePin; // ponytail: unpin action stayed with the old sidebar's drag list; add back if needed
  const isIssue = pin.item_type === 'issue';
  const issueQuery = useQuery({ ...issueDetailOptions(wsId, pin.item_id), enabled: isIssue });
  const projectQuery = useQuery({ ...projectDetailOptions(wsId, pin.item_id), enabled: !isIssue });

  if (isIssue) {
    if (!issueQuery.data) return null;
    const href = p.issueDetail(pin.item_id);
    return (
      <PanelLink href={href} isActive={pathname === href}>
        <StatusIcon status={issueQuery.data.status} className="!size-3.5 shrink-0" />
        <span className="truncate">{issueQuery.data.title}</span>
      </PanelLink>
    );
  }
  if (!projectQuery.data) return null;
  const href = p.projectDetail(pin.item_id);
  return (
    <PanelLink href={href} isActive={pathname === href}>
      <ProjectIcon project={projectQuery.data} size="sm" />
      <span className="truncate">{projectQuery.data.title}</span>
    </PanelLink>
  );
}

function TasksSection() {
  const { t } = useT('layout');
  const p = useWorkspacePaths();
  const { pathname } = useNavigation();
  const workspace = useCurrentWorkspace();
  const userId = useAuthStore((s) => s.user?.id);
  const wsId = workspace?.id;
  const { data: pins = EMPTY_PINS } = useQuery({
    ...pinListOptions(wsId ?? '', userId ?? ''),
    enabled: !!wsId && !!userId,
  });

  return (
    <>
      <SidebarGroup>
        <SidebarGroupContent>
          <SidebarMenu className="gap-0.5">
            <PanelLink href={p.myIssues()} isActive={pathname === p.myIssues()}>
              <span>{t(($) => $.nav.my_issues)}</span>
            </PanelLink>
            <PanelLink href={p.projects()} isActive={pathname === p.projects()}>
              <span>{t(($) => $.nav.projects)}</span>
            </PanelLink>
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>
      {pins.length > 0 && (
        <SidebarGroup>
          <SidebarGroupLabel>{t(($) => $.sidebar.pinned_label)}</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu className="gap-0.5">
              {pins.map((pin) => (
                <PinRow key={pin.id} pin={pin} wsId={wsId ?? ''} />
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      )}
    </>
  );
}

function CrewSection() {
  const { t } = useT('layout');
  const p = useWorkspacePaths();
  const { pathname } = useNavigation();
  return (
    <SidebarGroup>
      <SidebarGroupContent>
        <SidebarMenu className="gap-0.5">
          <PanelLink href={p.agents()} isActive={pathname === p.agents()}>
            <span>{t(($) => $.nav.agents)}</span>
          </PanelLink>
          <PanelLink href={p.squads()} isActive={pathname === p.squads()}>
            <span>{t(($) => $.nav.squads)}</span>
          </PanelLink>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}

function SettingsSection() {
  const { t } = useT('layout');
  const p = useWorkspacePaths();
  const { pathname } = useNavigation();
  return (
    <SidebarGroup>
      <SidebarGroupContent>
        <SidebarMenu className="gap-0.5">
          <PanelLink href={p.runtimes()} isActive={pathname === p.runtimes()}>
            <span>{t(($) => $.nav.runtimes)}</span>
          </PanelLink>
          <PanelLink href={p.skills()} isActive={pathname === p.skills()}>
            <span>{t(($) => $.nav.skills)}</span>
          </PanelLink>
          <PanelLink href={p.usage()} isActive={pathname === p.usage()}>
            <span>{t(($) => $.nav.usage)}</span>
          </PanelLink>
          <PanelLink href={p.settings()} isActive={pathname === p.settings()}>
            <span>{t(($) => $.nav.settings)}</span>
          </PanelLink>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}

function FeedSection() {
  const { t } = useT('layout');
  const p = useWorkspacePaths();
  const { pathname } = useNavigation();
  return (
    <SidebarGroup>
      <SidebarGroupContent>
        <SidebarMenu className="gap-0.5">
          <PanelLink href={p.inbox()} isActive={pathname === p.inbox()}>
            <span>{t(($) => $.nav.inbox)}</span>
          </PanelLink>
          <PanelLink href={p.chat()} isActive={pathname === p.chat()}>
            <span>{t(($) => $.nav.chat)}</span>
          </PanelLink>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}

function EmptySection({ titleKey }: { titleKey: 'projects' | 'autopilots' }) {
  const { t } = useT('layout');
  return (
    <SidebarGroup>
      <SidebarGroupLabel>{t(($) => $.nav[titleKey])}</SidebarGroupLabel>
    </SidebarGroup>
  );
}

interface ContextPanelProps {
  activeSection: NavSection | null;
  onCollapsedChange?: (collapsed: boolean) => void;
}

// Контекстная панель (ADR-0002): содержимое зависит от активного раздела
// рельсы. Сворачивается кнопкой и клавишей `[`, состояние переживает
// перезагрузку через localStorage (T-005).
export function ContextPanel({ activeSection, onCollapsedChange }: ContextPanelProps) {
  const [collapsed, setCollapsed] = useState(readStoredCollapsed);

  useEffect(() => {
    try {
      window.localStorage.setItem(COLLAPSE_STORAGE_KEY, String(collapsed));
    } catch {
      // ponytail: localStorage can throw in private mode; collapse state
      // just won't persist, no need to surface an error for that.
    }
    onCollapsedChange?.(collapsed);
  }, [collapsed, onCollapsedChange]);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== '[') return;
      const target = e.target as HTMLElement | null;
      if (target && ['INPUT', 'TEXTAREA'].includes(target.tagName)) return;
      setCollapsed((c) => !c);
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);

  if (collapsed) {
    return (
      <div className="flex w-8 shrink-0 flex-col items-center border-r border-sidebar-border bg-sidebar pt-3">
        <button
          type="button"
          onClick={() => setCollapsed(false)}
          aria-label={EXPAND_PANEL_LABEL}
          className="flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
        >
          <ChevronsRight className="size-3.5" />
        </button>
      </div>
    );
  }

  return (
    <Sidebar collapsible="none" className="w-[260px]! border-r">
      <SidebarContent>
        <SidebarGroup className="pb-0">
          <div className="flex items-center justify-end px-1">
            <button
              type="button"
              onClick={() => setCollapsed(true)}
              aria-label={COLLAPSE_PANEL_LABEL}
              className="flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
            >
              <ChevronsLeft className="size-3.5" />
            </button>
          </div>
        </SidebarGroup>
        {activeSection === 'feed' && <FeedSection />}
        {activeSection === 'tasks' && <TasksSection />}
        {activeSection === 'crew' && <CrewSection />}
        {activeSection === 'settings' && <SettingsSection />}
        {activeSection === 'projects' && <EmptySection titleKey="projects" />}
        {activeSection === 'autopilot' && <EmptySection titleKey="autopilots" />}
      </SidebarContent>
    </Sidebar>
  );
}
