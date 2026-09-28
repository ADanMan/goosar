'use client';

import { useEffect } from 'react';
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
import { workspaceBillingPath, type NavSection } from './nav-sections';
import { useContextPanelState } from './use-context-panel-state';

const EMPTY_PINS: PinnedItem[] = [];

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

function SettingsSection({ billingEnabled }: { billingEnabled?: boolean }) {
  const { t } = useT('layout');
  const p = useWorkspacePaths();
  const { pathname } = useNavigation();
  const billingHref = workspaceBillingPath(p);
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
          {/* Тестовая страница биллинга (T-032 §3.2) — пункт виден только
              когда включён NEXT_PUBLIC_ENABLE_BILLING_TEST_PAGE, иначе
              /billing отдаёт notFound(). */}
          {billingEnabled && (
            <PanelLink href={billingHref} isActive={pathname === billingHref}>
              <span>{t(($) => $.nav.billing)}</span>
            </PanelLink>
          )}
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
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}

// Чат теперь собственный пункт рельсы (T-019), а не подпункт «Ленты»; сам
// пункт уже открывает `/chat` одним кликом из NavRail, так что панели для
// него достаточно заголовка раздела — без списка (в отличие от «Задач» и
// «Исполнителей», у чата нет вложенных подстраниц).
function ChatSection() {
  const { t } = useT('layout');
  return (
    <SidebarGroup>
      <SidebarGroupLabel>{t(($) => $.nav.chat)}</SidebarGroupLabel>
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
  billingEnabled?: boolean;
}

// Контекстная панель (ADR-0002): содержимое зависит от активного раздела
// рельсы. Сворачивается кнопкой (всегда видна на границе панели) и клавишей
// `[`, состояние переживает перезагрузку через localStorage и общий хук
// `useContextPanelState` (T-005, T-020) — тот же хук использует десктопный
// `WindowToolbar`, чтобы его `SidebarTrigger` переключал именно эту панель.
export function ContextPanel({
  activeSection,
  onCollapsedChange,
  billingEnabled,
}: ContextPanelProps) {
  const { t } = useT('layout');
  const { collapsed, setCollapsed, toggle } = useContextPanelState();

  useEffect(() => {
    onCollapsedChange?.(collapsed);
  }, [collapsed, onCollapsedChange]);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== '[') return;
      const target = e.target as HTMLElement | null;
      if (target && ['INPUT', 'TEXTAREA'].includes(target.tagName)) return;
      toggle();
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [toggle]);

  if (collapsed) {
    return (
      <div className="flex w-8 shrink-0 flex-col items-center border-r border-sidebar-border bg-sidebar pt-3">
        <button
          type="button"
          onClick={() => setCollapsed(false)}
          aria-label={t(($) => $.sidebar.panel_expand)}
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
              aria-label={t(($) => $.sidebar.panel_collapse)}
              className="flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
            >
              <ChevronsLeft className="size-3.5" />
            </button>
          </div>
        </SidebarGroup>
        {activeSection === 'feed' && <FeedSection />}
        {activeSection === 'chat' && <ChatSection />}
        {activeSection === 'tasks' && <TasksSection />}
        {activeSection === 'crew' && <CrewSection />}
        {activeSection === 'settings' && <SettingsSection billingEnabled={billingEnabled} />}
        {activeSection === 'projects' && <EmptySection titleKey="projects" />}
        {activeSection === 'autopilot' && <EmptySection titleKey="autopilots" />}
      </SidebarContent>
    </Sidebar>
  );
}
