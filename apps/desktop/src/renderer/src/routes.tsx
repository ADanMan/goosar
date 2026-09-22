import { useEffect } from 'react';
import { createMemoryRouter, Outlet, useMatches } from 'react-router-dom';
import type { RouteObject } from 'react-router-dom';
import { IssueDetailPage } from './pages/issue-detail-page';
import { ProjectDetailPage } from './pages/project-detail-page';
import { AutopilotDetailPage } from './pages/autopilot-detail-page';
import { SkillDetailPage } from './pages/skill-detail-page';
import { AgentDetailPage } from './pages/agent-detail-page';
import { MemberDetailPage } from './pages/member-detail-page';
import { RuntimeDetailPage, RuntimeSettingsPage } from './pages/runtime-detail-page';
import { AttachmentPreviewRoute } from './pages/attachment-preview-page';
import { IssuesPage } from '@goosar/views/issues/components';
import { ProjectsPage } from '@goosar/views/projects/components';
import { DashboardPage } from '@goosar/views/dashboard';
import { AutopilotsPage } from '@goosar/views/autopilots/components';
import { MyIssuesPage } from '@goosar/views/my-issues';
import { SkillsPage } from '@goosar/views/skills';
import { DesktopCapabilitiesPage } from './components/desktop-capabilities-page';
import { DesktopRuntimesPage } from './components/desktop-runtimes-page';
import { DesktopAgentsPage } from './components/desktop-agents-page';
import { AgentCreationStudio } from '@goosar/views/agents';
import {
  SquadsPage,
  SquadDetailPage as SquadDetailPageView,
} from '@goosar/views/squads/components';
import { InboxPage } from '@goosar/views/inbox';
import { ChatPage } from '@goosar/views/chat';
import { SettingsPage } from '@goosar/views/settings';
import { useT } from '@goosar/views/i18n';
import { Download, Server } from 'lucide-react';
import { DaemonSettingsTab } from './components/daemon-settings-tab';
import { UpdatesSettingsTab } from './components/updates-settings-tab';
import { WorkspaceRouteLayout } from './components/workspace-route-layout';
import { DesktopRouteErrorPage } from './components/route-error-page';
import { KerberosTicketStatus, kerberosTicketSummary } from './components/kerberos-ticket-status';
import { useNetworkStatus } from './components/network-status';

function DesktopSettingsRoute() {
  const { t } = useT('settings');
  const { state: perimeter } = useNetworkStatus();
  const kerberosVisible = kerberosTicketSummary({ state: perimeter, nowMs: Date.now() }) !== null;
  return (
    <SettingsPage
      extraAccountTabs={[
        {
          value: 'daemon',
          label: 'Daemon',
          icon: Server,
          content: <DaemonSettingsTab />,
        },
        {
          value: 'updates',
          label: t(($) => $.desktop.tabs.updates),
          icon: Download,
          content: <UpdatesSettingsTab />,
        },
      ]}
      accountKerberosSlot={<KerberosTicketStatus />}
      accountHasKerberos={kerberosVisible}
    />
  );
}

function TitleSync() {
  const matches = useMatches();
  const title = [...matches].reverse().find((m) => (m.handle as { title?: string })?.title)
    ?.handle as { title?: string } | undefined;

  useEffect(() => {
    if (title?.title) document.title = title.title;
  }, [title?.title]);

  return null;
}

function PageShell() {
  return (
    <>
      <TitleSync />
      <Outlet />
    </>
  );
}

export const appRoutes: RouteObject[] = [
  {
    element: <PageShell />,
    errorElement: <DesktopRouteErrorPage />,
    children: [
      { index: true, element: null },
      {
        path: ':workspaceSlug',
        element: <WorkspaceRouteLayout />,
        children: [
          { index: true, element: null },
          {
            path: 'issues',
            element: <IssuesPage />,
            handle: { title: 'Issues' },
          },
          {
            path: 'issues/:id',
            element: <IssueDetailPage />,
            handle: { title: 'Issue' },
          },
          {
            path: 'projects',
            element: <ProjectsPage />,
            handle: { title: 'Projects' },
          },
          {
            path: 'projects/:id',
            element: <ProjectDetailPage />,
            handle: { title: 'Project' },
          },
          {
            path: 'autopilots',
            element: <AutopilotsPage />,
            handle: { title: 'Autopilot' },
          },
          {
            path: 'autopilots/:id',
            element: <AutopilotDetailPage />,
            handle: { title: 'Autopilot' },
          },
          {
            path: 'my-issues',
            element: <MyIssuesPage />,
            handle: { title: 'My Issues' },
          },
          {
            path: 'runtimes',
            element: <DesktopRuntimesPage />,
            handle: { title: 'Runtimes' },
          },
          {
            path: 'runtimes/:id',
            element: <RuntimeDetailPage />,
            handle: { title: 'Machine' },
          },
          {
            path: 'runtimes/:id/runtime/:runtimeId',
            element: <RuntimeSettingsPage />,
            handle: { title: 'Runtime' },
          },
          { path: 'skills', element: <SkillsPage />, handle: { title: 'Skills' } },
          {
            path: 'skills/:id',
            element: <SkillDetailPage />,
            handle: { title: 'Skill' },
          },
          { path: 'agents', element: <DesktopAgentsPage />, handle: { title: 'Agents' } },
          {
            path: 'agents/new',
            element: <AgentCreationStudio />,
            handle: { title: 'Create Agent' },
          },
          {
            path: 'agents/:id',
            element: <AgentDetailPage />,
            handle: { title: 'Agent' },
          },
          {
            path: 'members/:id',
            element: <MemberDetailPage />,
            handle: { title: 'Member' },
          },
          { path: 'squads', element: <SquadsPage />, handle: { title: 'Squads' } },
          {
            path: 'squads/:id',
            element: <SquadDetailPageView />,
            handle: { title: 'Squad' },
          },
          { path: 'inbox', element: <InboxPage />, handle: { title: 'Inbox' } },
          { path: 'chat', element: <ChatPage />, handle: { title: 'Chat' } },
          {
            path: 'attachments/:id/preview',
            element: <AttachmentPreviewRoute />,
            handle: { title: 'Attachment' },
          },
          {
            path: 'usage',
            element: <DashboardPage />,
            handle: { title: 'Usage' },
          },
          {
            path: 'settings',
            element: <DesktopSettingsRoute />,
            handle: { title: 'Settings' },
          },
          {
            path: 'capabilities',
            element: <DesktopCapabilitiesPage />,
            handle: { title: 'Capabilities' },
          },
        ],
      },
    ],
  },
];

export function createAppRouter() {
  return createMemoryRouter(appRoutes, {
    initialEntries: ['/'],
  });
}
