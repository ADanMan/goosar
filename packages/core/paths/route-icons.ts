// Реестр страниц навигации воркспейса и их иконок; иконка страницы — статичный
// выбор по сегменту маршрута.

export type RouteIconName =
  | 'Inbox'
  | 'MessageSquare'
  | 'CircleUser'
  | 'ListTodo'
  | 'FolderKanban'
  | 'Zap'
  | 'Bot'
  | 'Users'
  | 'BarChart3'
  | 'Monitor'
  | 'Server'
  | 'BookOpenText'
  | 'Settings'
  | 'Sparkles'
  | 'File'
  | 'FileText'
  | 'FileImage'
  | 'FileCode'
  | 'FileArchive'
  | 'FileAudio'
  | 'FileVideo'
  | 'FileQuestion';

export type NavLabelKey =
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
  | 'settings'
  | 'capabilities';

export type WorkspacePageKey =
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
  | 'settings'
  | 'capabilities';

export interface WorkspacePage {
  segment: string;
  icon: RouteIconName;
  navKey: NavLabelKey;
}

export const WORKSPACE_PAGES: Record<WorkspacePageKey, WorkspacePage> = {
  inbox: { segment: 'inbox', icon: 'Inbox', navKey: 'inbox' },
  chat: { segment: 'chat', icon: 'MessageSquare', navKey: 'chat' },
  myIssues: { segment: 'my-issues', icon: 'CircleUser', navKey: 'my_issues' },
  issues: { segment: 'issues', icon: 'ListTodo', navKey: 'issues' },
  projects: { segment: 'projects', icon: 'FolderKanban', navKey: 'projects' },
  autopilots: { segment: 'autopilots', icon: 'Zap', navKey: 'autopilots' },
  agents: { segment: 'agents', icon: 'Bot', navKey: 'agents' },
  squads: { segment: 'squads', icon: 'Users', navKey: 'squads' },
  usage: { segment: 'usage', icon: 'BarChart3', navKey: 'usage' },
  runtimes: { segment: 'runtimes', icon: 'Monitor', navKey: 'runtimes' },
  skills: { segment: 'skills', icon: 'BookOpenText', navKey: 'skills' },
  settings: { segment: 'settings', icon: 'Settings', navKey: 'settings' },
  capabilities: {
    segment: 'capabilities',
    icon: 'Sparkles',
    navKey: 'capabilities',
  },
};

const PAGE_BY_SEGMENT: Record<string, WorkspacePageKey> = Object.fromEntries(
  (Object.keys(WORKSPACE_PAGES) as WorkspacePageKey[]).map((key) => [
    WORKSPACE_PAGES[key].segment,
    key,
  ]),
);

export function pageForSegment(segment: string): WorkspacePageKey | null {
  return PAGE_BY_SEGMENT[segment] ?? null;
}

export const DEFAULT_ROUTE_ICON_NAME: RouteIconName = 'ListTodo';

export function resolveRouteIconName(path: string): RouteIconName {
  const pathname = path.split(/[?#]/)[0] ?? '';
  const segment = pathname.split('/').filter(Boolean)[1] ?? '';
  const page = pageForSegment(segment);
  return page ? WORKSPACE_PAGES[page].icon : DEFAULT_ROUTE_ICON_NAME;
}
