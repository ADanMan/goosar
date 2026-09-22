// Чистый резолвер представления вкладки десктопа: по цели вкладки и данным
// из кэша выдаёт визуальный элемент и заголовок.
import type { IssueStatus } from '../types';
import { WORKSPACE_PAGES, type NavLabelKey, type RouteIconName } from './route-icons';
import type { TabActorType, TabSubject } from './tab-subject';

export type TabVisual =
  | { kind: 'icon'; icon: RouteIconName }
  /** An issue's live status glyph. `null` while the issue is loading. */
  | { kind: 'issue-status'; status: IssueStatus | null }
  /** A project's own icon. `null` falls back to the default project glyph. */
  | { kind: 'project-icon'; icon: string | null }
  /** An actor's avatar, resolved by the view layer from `actorType`+`id`. */
  | { kind: 'actor'; actorType: TabActorType; id: string };

export type TabLabelKey =
  | 'issue'
  | 'project'
  | 'autopilot'
  | 'agent'
  | 'member'
  | 'squad'
  | 'skill'
  | 'machine'
  | 'runtime'
  | 'attachment'
  | 'create_agent'
  | 'unknown';

export type TabTitleSpec =
  | { kind: 'text'; text: string }
  /** A page name — localize via `layout.nav.<navKey>`. */
  | { kind: 'nav'; navKey: NavLabelKey }
  /** A type label (loading / flow / unknown) — localize via `layout.tab.<tabKey>`. */
  | { kind: 'tab'; tabKey: TabLabelKey };

export interface TabPresentation {
  visual: TabVisual;
  title: TabTitleSpec;
}

export type InboxSelectionData =
  { kind: 'issue'; identifier: string; title: string } | { kind: 'item'; title: string };

export interface TabEntityData {
  issue?: { identifier: string; title: string; status: IssueStatus };
  project?: { icon: string | null; title: string };
  autopilot?: { title: string };
  actorName?: string;
  skill?: { name: string };
  machine?: { name: string };
  runtime?: { name: string };
  chatSessionTitle?: string;
  inboxSelection?: InboxSelectionData;
}

export const DEFAULT_TAB_VISUAL: TabVisual = { kind: 'icon', icon: 'FileQuestion' };

function textOr(text: string | undefined | null, tabKey: TabLabelKey): TabTitleSpec {
  const trimmed = text?.trim();
  return trimmed ? { kind: 'text', text: trimmed } : { kind: 'tab', tabKey };
}

const ACTOR_LABEL: Record<TabActorType, TabLabelKey> = {
  agent: 'agent',
  member: 'member',
  squad: 'squad',
};

const EXTENSION_ICON: Record<string, RouteIconName> = {};
const registerExtensions = (icon: RouteIconName, exts: string[]) => {
  for (const ext of exts) EXTENSION_ICON[ext] = icon;
};
registerExtensions('FileImage', [
  'png',
  'jpg',
  'jpeg',
  'gif',
  'webp',
  'svg',
  'bmp',
  'ico',
  'avif',
  'heic',
]);
registerExtensions('FileVideo', ['mp4', 'mov', 'webm', 'mkv', 'avi', 'm4v']);
registerExtensions('FileAudio', ['mp3', 'wav', 'ogg', 'flac', 'm4a', 'aac']);
registerExtensions('FileArchive', ['zip', 'tar', 'gz', 'tgz', 'rar', '7z', 'bz2']);
registerExtensions('FileCode', [
  'js',
  'jsx',
  'ts',
  'tsx',
  'json',
  'yaml',
  'yml',
  'py',
  'go',
  'rs',
  'java',
  'c',
  'h',
  'cpp',
  'cc',
  'rb',
  'php',
  'swift',
  'kt',
  'sh',
  'css',
  'scss',
]);
registerExtensions('FileText', [
  'txt',
  'md',
  'markdown',
  'pdf',
  'doc',
  'docx',
  'csv',
  'log',
  'html',
  'htm',
  'xml',
  'rtf',
  'odt',
]);

export function iconForAttachment(filename: string | null): RouteIconName {
  if (!filename) return 'File';
  const dot = filename.lastIndexOf('.');
  if (dot < 0 || dot === filename.length - 1) return 'File';
  const ext = filename.slice(dot + 1).toLowerCase();
  return EXTENSION_ICON[ext] ?? 'File';
}

export function resolveTabPresentation(
  subject: TabSubject,
  data: TabEntityData = {},
): TabPresentation {
  switch (subject.kind) {
    case 'page': {
      const page = WORKSPACE_PAGES[subject.page];
      return {
        visual: { kind: 'icon', icon: page.icon },
        title: { kind: 'nav', navKey: page.navKey },
      };
    }
    case 'issue':
      return {
        visual: { kind: 'issue-status', status: data.issue?.status ?? null },
        title: data.issue
          ? { kind: 'text', text: `${data.issue.identifier}: ${data.issue.title}` }
          : { kind: 'tab', tabKey: 'issue' },
      };
    case 'project':
      return {
        visual: { kind: 'project-icon', icon: data.project?.icon ?? null },
        title: textOr(data.project?.title, 'project'),
      };
    case 'autopilot':
      return {
        visual: { kind: 'icon', icon: 'Zap' },
        title: textOr(data.autopilot?.title, 'autopilot'),
      };
    case 'actor':
      return {
        visual: { kind: 'actor', actorType: subject.actorType, id: subject.id },
        title: textOr(data.actorName, ACTOR_LABEL[subject.actorType]),
      };
    case 'skill':
      return {
        visual: { kind: 'icon', icon: 'BookOpenText' },
        title: textOr(data.skill?.name, 'skill'),
      };
    case 'machine':
      return {
        visual: { kind: 'icon', icon: 'Monitor' },
        title: textOr(data.machine?.name, 'machine'),
      };
    case 'runtime':
      return {
        visual: { kind: 'icon', icon: 'Server' },
        title: textOr(data.runtime?.name, 'runtime'),
      };
    case 'attachment':
      return {
        visual: { kind: 'icon', icon: iconForAttachment(subject.filename) },
        title: subject.filename
          ? { kind: 'text', text: subject.filename }
          : { kind: 'tab', tabKey: 'attachment' },
      };
    case 'inbox': {
      const sel = subject.selectedKey ? data.inboxSelection : undefined;
      let title: TabTitleSpec;
      if (!sel) {
        title = { kind: 'nav', navKey: 'inbox' };
      } else if (sel.kind === 'issue') {
        title = { kind: 'text', text: `${sel.identifier}: ${sel.title}` };
      } else {
        const text = sel.title.trim();
        title = text ? { kind: 'text', text } : { kind: 'nav', navKey: 'inbox' };
      }
      return { visual: { kind: 'icon', icon: 'Inbox' }, title };
    }
    case 'chat': {
      const title: TabTitleSpec =
        subject.sessionId && data.chatSessionTitle?.trim()
          ? { kind: 'text', text: data.chatSessionTitle.trim() }
          : { kind: 'nav', navKey: 'chat' };
      return { visual: { kind: 'icon', icon: 'MessageSquare' }, title };
    }
    case 'flow':
      return {
        visual: { kind: 'icon', icon: 'Bot' },
        title: { kind: 'tab', tabKey: 'create_agent' },
      };
    case 'unknown':
      return {
        visual: DEFAULT_TAB_VISUAL,
        title: { kind: 'tab', tabKey: 'unknown' },
      };
  }
}
