import type { WorkspacePaths } from '@goosar/core/paths';

// Шесть разделов рельсы (ADR-0002, docs/20-architecture.md §4.2). Порядок
// здесь = порядок кнопок в NavRail и порядок вкладок в ContextPanel.
export type NavSection = 'feed' | 'tasks' | 'projects' | 'crew' | 'autopilot' | 'settings';

export const NAV_SECTIONS: readonly NavSection[] = [
  'feed',
  'tasks',
  'projects',
  'crew',
  'autopilot',
  'settings',
];

// Раздел рельсы -> путь, на который ведёт его кнопка (первый пункт раздела).
export function navSectionHref(p: WorkspacePaths, section: NavSection): string {
  switch (section) {
    case 'feed':
      return p.inbox();
    case 'tasks':
      return p.issues();
    case 'projects':
      return p.projects();
    case 'crew':
      return p.agents();
    case 'autopilot':
      return p.autopilots();
    case 'settings':
      return p.settings();
  }
}

// Путь -> раздел рельсы, которому он принадлежит (для подсветки активного
// пункта и для контекстной панели/хлебных крошек). Порядок веток учитывает
// вложенные пути: `/agents/new` должен попасть в `crew` так же, как `/agents`.
export function navSectionForPath(p: WorkspacePaths, pathname: string): NavSection | null {
  const isUnder = (href: string) => pathname === href || pathname.startsWith(href + '/');

  if (isUnder(p.inbox()) || isUnder(p.chat())) return 'feed';
  if (isUnder(p.issues()) || isUnder(p.myIssues())) return 'tasks';
  if (isUnder(p.projects())) return 'projects';
  if (isUnder(p.agents()) || isUnder(p.squads())) return 'crew';
  if (isUnder(p.autopilots())) return 'autopilot';
  if (isUnder(p.settings()) || isUnder(p.runtimes()) || isUnder(p.skills()) || isUnder(p.usage()))
    return 'settings';
  return null;
}

// Раздел -> ключ nav.* для подписи в рельсе, вкладке ContextPanel и крошке
// CommandBar. Общий словарь для всех трёх потребителей.
export const NAV_LABEL_KEYS_FOR_BREADCRUMB: Record<
  NavSection,
  'inbox' | 'issues' | 'projects' | 'agents' | 'autopilots' | 'settings'
> = {
  feed: 'inbox',
  tasks: 'issues',
  projects: 'projects',
  crew: 'agents',
  autopilot: 'autopilots',
  settings: 'settings',
};
