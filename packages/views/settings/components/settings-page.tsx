'use client';

import React from 'react';
import {
  User,
  SlidersHorizontal,
  Key,
  Settings,
  ShieldCheck,
  Users,
  FolderGit2,
  FlaskConical,
  Bell,
  Plug,
  MessageCircle,
  Tags,
  Keyboard,
  ListTodo,
  ServerCog,
  Server,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@goosar/ui/components/ui/tabs';
import { Skeleton } from '@goosar/ui/components/ui/skeleton';
import { useIsMobile } from '@goosar/ui/hooks/use-mobile';
import { cn } from '@goosar/ui/lib/utils';
import { useAuthStore } from '@goosar/core/auth';
import { useCurrentWorkspace } from '@goosar/core/paths';
import { memberListOptions } from '@goosar/core/workspace/queries';
import { deploymentAdminsOptions } from '@goosar/core/deployment/admin';
import { useNavigation } from '../../navigation';
import { AccountTab } from './account-tab';
import { AdminTab } from './admin-tab';
import { PreferencesTab } from './preferences-tab';
import { ChatTab } from './chat-tab';
import { IssueTab } from './issue-tab';
import { SecurityTab } from './security-tab';
import { TokensTab } from './tokens-tab';
import { WorkspaceTab } from './workspace-tab';
import { MembersTab } from './members-tab';
import { RepositoriesTab } from './repositories-tab';
import { IntegrationsTab } from './integrations-tab';
import { LabsTab } from './labs-tab';
import { McpTab } from './mcp-tab';
import { NotificationsTab } from './notifications-tab';
import { LabelsTab } from './labels-tab';
import { PropertiesTab } from './properties-tab';
import { KeyboardShortcutsTab } from './keyboard-shortcuts-tab';
import { DeploymentTab } from './deployment-tab';
import { SettingsTab } from './settings-layout';
import { useT } from '../../i18n';

const ACCOUNT_TAB_KEYS = [
  'profile',
  'preferences',
  'shortcuts',
  'issue',
  'chat',
  'notifications',
  'security',
  'tokens',
] as const;
const ACCOUNT_TAB_ICONS = {
  profile: User,
  preferences: SlidersHorizontal,
  shortcuts: Keyboard,
  issue: ListTodo,
  chat: MessageCircle,
  notifications: Bell,
  security: ShieldCheck,
  tokens: Key,
} as const;

const WORKSPACE_TAB_KEYS = [
  'general',
  'repositories',
  'integrations',
  'labs',
  'members',
  'labels',
  'properties',
  'mcp',
] as const;
const WORKSPACE_TAB_VALUES = {
  general: 'workspace',
  repositories: 'repositories',
  integrations: 'integrations',
  labs: 'labs',
  members: 'members',
  labels: 'labels',
  properties: 'properties',
  mcp: 'mcp',
  admin: 'admin',
} as const;
const WORKSPACE_TAB_ICONS = {
  general: Settings,
  repositories: FolderGit2,
  integrations: Plug,
  labs: FlaskConical,
  members: Users,
  labels: Tags,
  properties: SlidersHorizontal,
  mcp: Server,
  admin: ShieldCheck,
} as const;

const DEFAULT_TAB = 'profile';
const TAB_QUERY_KEY = 'tab';

const LEGACY_WORKSPACE_TAB_REDIRECTS: Record<string, string> = {
  github: 'integrations',
};

const SETTINGS_TAB_TRIGGER_CLASS =
  'h-8 shrink-0 px-2.5 hover:bg-surface-hover data-active:!bg-surface-selected data-active:!text-surface-selected-foreground data-active:hover:!bg-surface-selected md:!w-full md:px-2 md:after:hidden';

// §3 L91: on the narrow layout the rail stays a flat row instead of the
// grouped column it is at md+, but the group label is kept as a compact
// divider (uppercase, no padding to spare) rather than `hidden` outright —
// it is still the only way to tell personal/workspace/deployment tabs apart
// short of reading each icon. Callers add their own md:pt-* (the desktop
// layout still wants more space above the 2nd/3rd group than the 1st).
const SETTINGS_TAB_GROUP_LABEL_CLASS =
  'flex shrink-0 items-center whitespace-nowrap px-2 text-[10px] font-medium uppercase tracking-wide text-muted-foreground md:block md:pb-1 md:text-xs md:font-medium md:normal-case md:tracking-normal';

export interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
}

interface SettingsPageProps {
  extraAccountTabs?: ExtraSettingsTab[];
  accountKerberosSlot?: React.ReactNode;
  accountHasKerberos?: boolean;
}

export function SettingsPage({
  extraAccountTabs,
  accountKerberosSlot,
  accountHasKerberos,
}: SettingsPageProps = {}) {
  const { t } = useT('settings');
  const workspace = useCurrentWorkspace();
  const workspaceName = workspace?.name;
  const navigation = useNavigation();
  const isMobile = useIsMobile();

  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery({
    ...memberListOptions(workspace?.id ?? ''),
    enabled: !!workspace?.id,
  });
  const viewerRole = members.find((m) => m.user_id === user?.id)?.role;
  const canManageWorkspace = viewerRole === 'owner' || viewerRole === 'admin';

  const { data: deploymentAdmins, isLoading: isDeploymentAdminsLoading } =
    useQuery(deploymentAdminsOptions());
  const isDeploymentAdmin = Array.isArray(deploymentAdmins);

  const workspaceTabKeys = React.useMemo(
    () => (canManageWorkspace ? ([...WORKSPACE_TAB_KEYS, 'admin'] as const) : WORKSPACE_TAB_KEYS),
    [canManageWorkspace],
  );

  // 'deployment' stays a known destination even before the admin check has
  // resolved or when it comes back negative: the content pane below then
  // shows a loading state or an explicit "no access" message instead of the
  // tab silently vanishing into a fallback to Profile (T-032).
  const validTabs = React.useMemo(
    () =>
      new Set<string>([
        ...ACCOUNT_TAB_KEYS,
        ...workspaceTabKeys.map((key) => WORKSPACE_TAB_VALUES[key]),
        'deployment',
        ...(extraAccountTabs?.map((tab) => tab.value) ?? []),
      ]),
    [extraAccountTabs, workspaceTabKeys],
  );

  const tabFromUrl = navigation.searchParams.get(TAB_QUERY_KEY);
  const candidateTab = tabFromUrl
    ? (LEGACY_WORKSPACE_TAB_REDIRECTS[tabFromUrl] ?? tabFromUrl)
    : null;
  const activeTab = candidateTab && validTabs.has(candidateTab) ? candidateTab : DEFAULT_TAB;

  const handleTabChange = (next: string) => {
    const params = new URLSearchParams(navigation.searchParams);
    params.set(TAB_QUERY_KEY, next);
    navigation.replace(`${navigation.pathname}?${params.toString()}`);
  };

  return (
    <Tabs
      value={activeTab}
      onValueChange={handleTabChange}
      orientation={isMobile ? 'horizontal' : 'vertical'}
      className="flex flex-1 min-h-0 flex-col gap-0 overflow-y-auto md:flex-row md:overflow-hidden"
    >
      {/* Structural navigation; bounded setting groups remain in the content surface.
          Stays on the content surface color (no shell tint): the desktop's active
          tab merges into the card top, and a tinted panel under the first tabs
          breaks that seam (MUL-4439). Zoning comes from the divider instead.

          §3 L98: this rail is intentionally NOT independently collapsible,
          unlike the app's primary sidebar — it is a settings-only nested
          panel (HIG "Sidebars"), and the asymmetry is deliberate, not a gap
          to close on the next pass through this file. */}
      <div className="shrink-0 overflow-x-auto border-b border-surface-border p-2 md:w-56 md:overflow-y-auto md:border-b-0 md:border-r md:p-4">
        <h1 className="sr-only text-sm font-semibold md:not-sr-only md:mb-4 md:px-2">
          {t(($) => $.page.title)}
        </h1>
        <TabsList
          variant="line"
          className="flex w-max min-w-full flex-row items-center gap-1 p-0 md:w-full md:flex-col md:items-stretch"
        >
          {/* My Account group */}
          <span className={cn(SETTINGS_TAB_GROUP_LABEL_CLASS, 'md:pt-2')}>
            {t(($) => $.page.my_account)}
          </span>
          {ACCOUNT_TAB_KEYS.map((key) => {
            const Icon = ACCOUNT_TAB_ICONS[key];
            return (
              <TabsTrigger key={key} value={key} className={SETTINGS_TAB_TRIGGER_CLASS}>
                <Icon className="h-4 w-4" />
                {t(($) => $.page.tabs[key])}
              </TabsTrigger>
            );
          })}
          {extraAccountTabs?.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value} className={SETTINGS_TAB_TRIGGER_CLASS}>
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </TabsTrigger>
          ))}

          {/* Workspace group */}
          <span
            className={cn(
              SETTINGS_TAB_GROUP_LABEL_CLASS,
              'max-w-[8rem] truncate md:max-w-none md:pt-4',
            )}
          >
            {workspaceName ?? t(($) => $.page.workspace_fallback)}
          </span>
          {workspaceTabKeys.map((key) => {
            const Icon = WORKSPACE_TAB_ICONS[key];
            return (
              <TabsTrigger
                key={key}
                value={WORKSPACE_TAB_VALUES[key]}
                className={SETTINGS_TAB_TRIGGER_CLASS}
              >
                <Icon className="h-4 w-4" />
                {t(($) => $.page.tabs[key])}
              </TabsTrigger>
            );
          })}

          {/* Deployment group — only for holders of the deployment_admin
              role (see the gate above). */}
          {isDeploymentAdmin && (
            <>
              <span className={cn(SETTINGS_TAB_GROUP_LABEL_CLASS, 'md:pt-4')}>
                {t(($) => $.page.deployment_group)}
              </span>
              <TabsTrigger value="deployment" className={SETTINGS_TAB_TRIGGER_CLASS}>
                <ServerCog className="h-4 w-4" />
                {t(($) => $.page.tabs.deployment)}
              </TabsTrigger>
            </>
          )}
        </TabsList>
      </div>

      {/* Right content */}
      <div className="min-w-0 flex-1 md:overflow-y-auto">
        <div
          className={`mx-auto w-full p-4 sm:p-6 md:p-8 ${activeTab === 'labels' || activeTab === 'properties' ? 'max-w-5xl' : 'max-w-3xl'}`}
        >
          <TabsContent value="profile">
            <AccountTab kerberosSlot={accountKerberosSlot} hasKerberos={accountHasKerberos} />
          </TabsContent>
          <TabsContent value="preferences">
            <PreferencesTab />
          </TabsContent>
          <TabsContent value="shortcuts">
            <KeyboardShortcutsTab />
          </TabsContent>
          <TabsContent value="issue">
            <IssueTab />
          </TabsContent>
          <TabsContent value="chat">
            <ChatTab />
          </TabsContent>
          <TabsContent value="notifications">
            <NotificationsTab />
          </TabsContent>
          {/* Second factor, recovery codes and the person's own sessions
              (#391). Account-scoped, not workspace-scoped: a factor protects
              the ACCOUNT, and it would be wrong to reach it through whichever
              workspace happens to be open. */}
          <TabsContent value="security">
            <SecurityTab />
          </TabsContent>
          <TabsContent value="tokens">
            <TokensTab />
          </TabsContent>
          <TabsContent value="workspace">
            <WorkspaceTab />
          </TabsContent>
          <TabsContent value="repositories">
            <RepositoriesTab />
          </TabsContent>
          <TabsContent value="integrations">
            <IntegrationsTab />
          </TabsContent>
          <TabsContent value="labs">
            <LabsTab />
          </TabsContent>
          <TabsContent value="members">
            <MembersTab />
          </TabsContent>
          <TabsContent value="labels">
            <LabelsTab />
          </TabsContent>
          <TabsContent value="properties">
            <PropertiesTab />
          </TabsContent>
          {/* The workspace MCP library. Every member may see the inventory —
              it carries no credential material and an agent owner needs to
              know what is available to assign — so this tab is not gated on
              the admin role; the write affordances inside it are. */}
          <TabsContent value="mcp">
            <McpTab wsId={workspace?.id ?? ''} />
          </TabsContent>
          {canManageWorkspace && (
            <TabsContent value="admin">
              <AdminTab />
            </TabsContent>
          )}
          {/* Mounted whenever 'deployment' is the active tab (see validTabs
              above), not only for a confirmed admin: it is what shows the
              loading and no-access states instead of a silent fallback. */}
          {activeTab === 'deployment' && (
            <TabsContent value="deployment">
              {isDeploymentAdminsLoading ? (
                <DeploymentTabSkeleton />
              ) : isDeploymentAdmin ? (
                <DeploymentTab />
              ) : (
                <DeploymentNoAccess />
              )}
            </TabsContent>
          )}
          {extraAccountTabs?.map((tab) => (
            <TabsContent key={tab.value} value={tab.value}>
              {tab.content}
            </TabsContent>
          ))}
        </div>
      </div>
    </Tabs>
  );
}

function DeploymentTabSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton className="h-7 w-48" />
      <Skeleton className="h-4 w-full max-w-xl" />
      <Skeleton className="h-40 w-full rounded-xl" />
      <Skeleton className="h-40 w-full rounded-xl" />
    </div>
  );
}

function DeploymentNoAccess() {
  const { t } = useT('settings');
  return (
    <SettingsTab title={t(($) => $.deployment.no_access_title)}>
      <p className="text-sm text-muted-foreground">
        {t(($) => $.deployment.no_access_description)}
      </p>
    </SettingsTab>
  );
}
