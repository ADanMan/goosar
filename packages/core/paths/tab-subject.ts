// Семантический резолвер цели вкладки десктопа: коллекция, ресурс, контейнер
// с выбранным элементом, сценарий создания или неизвестное.
import { pageForSegment, type WorkspacePageKey } from './route-icons';

export type TabActorType = 'agent' | 'member' | 'squad';

export type TabSubject =
  | { kind: 'page'; page: WorkspacePageKey }
  /** A single issue detail. */
  | { kind: 'issue'; id: string }
  /** A single project detail. */
  | { kind: 'project'; id: string }
  /** A single autopilot detail. */
  | { kind: 'autopilot'; id: string }
  /** An agent / member / squad detail (has an avatar identity). */
  | { kind: 'actor'; actorType: TabActorType; id: string }
  /** A single skill detail. */
  | { kind: 'skill'; id: string }
  /** A runtime machine detail. */
  | { kind: 'machine'; machineId: string }
  /** A runtime nested under a machine. */
  | { kind: 'runtime'; machineId: string; runtimeId: string }
  /** An attachment preview. `filename` is the `?name=` hint, or null. */
  | { kind: 'attachment'; id: string; filename: string | null }
  /**
   * The Inbox container; `selectedKey` is the `?issue=` selection or null,
   * `archived` is the `?view=archived` sub-list (its own list/cache).
   */
  | { kind: 'inbox'; selectedKey: string | null; archived: boolean }
  /** The Chat container; `sessionId` is the `?session=` selection or null. */
  | { kind: 'chat'; sessionId: string | null }
  /** A creation flow that has not produced a resource yet. */
  | { kind: 'flow'; flow: 'create-agent' }
  /** An unrecognized URL. Never impersonate a real page. */
  | { kind: 'unknown' };

function splitUrl(url: string): { segments: string[]; query: URLSearchParams } {
  const hashIdx = url.indexOf('#');
  const withoutHash = hashIdx === -1 ? url : url.slice(0, hashIdx);
  const queryIdx = withoutHash.indexOf('?');
  const pathname = queryIdx === -1 ? withoutHash : withoutHash.slice(0, queryIdx);
  const search = queryIdx === -1 ? '' : withoutHash.slice(queryIdx + 1);
  return {
    segments: pathname.split('/').filter(Boolean),
    query: new URLSearchParams(search),
  };
}

export function parseTabSubject(url: string): TabSubject {
  const { segments, query } = splitUrl(url);
  const segment = segments[1] ?? '';
  const id = segments[2] ?? '';

  switch (segment) {
    case 'issues':
      return id ? { kind: 'issue', id } : { kind: 'page', page: 'issues' };
    case 'my-issues':
      return { kind: 'page', page: 'myIssues' };
    case 'projects':
      return id ? { kind: 'project', id } : { kind: 'page', page: 'projects' };
    case 'autopilots':
      return id ? { kind: 'autopilot', id } : { kind: 'page', page: 'autopilots' };
    case 'agents':
      if (id === 'new') return { kind: 'flow', flow: 'create-agent' };
      return id ? { kind: 'actor', actorType: 'agent', id } : { kind: 'page', page: 'agents' };
    case 'members':
      return id ? { kind: 'actor', actorType: 'member', id } : { kind: 'unknown' };
    case 'squads':
      return id ? { kind: 'actor', actorType: 'squad', id } : { kind: 'page', page: 'squads' };
    case 'usage':
      return { kind: 'page', page: 'usage' };
    case 'inbox':
      return {
        kind: 'inbox',
        selectedKey: query.get('issue') || null,
        archived: query.get('view') === 'archived',
      };
    case 'chat':
      return { kind: 'chat', sessionId: query.get('session') || null };
    case 'runtimes':
      if (!id) return { kind: 'page', page: 'runtimes' };
      if (segments[3] === 'runtime' && segments[4]) {
        return { kind: 'runtime', machineId: id, runtimeId: segments[4] };
      }
      return { kind: 'machine', machineId: id };
    case 'skills':
      return id ? { kind: 'skill', id } : { kind: 'page', page: 'skills' };
    case 'settings':
      return { kind: 'page', page: 'settings' };
    case 'attachments':
      return id
        ? { kind: 'attachment', id, filename: query.get('name') || null }
        : { kind: 'unknown' };
    default: {
      const page = pageForSegment(segment);
      return page ? { kind: 'page', page } : { kind: 'unknown' };
    }
  }
}

export function tabSubjectKey(subject: TabSubject): string {
  switch (subject.kind) {
    case 'page':
      return `page:${subject.page}`;
    case 'issue':
      return `issue:${subject.id}`;
    case 'project':
      return `project:${subject.id}`;
    case 'autopilot':
      return `autopilot:${subject.id}`;
    case 'actor':
      return `actor:${subject.actorType}:${subject.id}`;
    case 'skill':
      return `skill:${subject.id}`;
    case 'machine':
      return `machine:${subject.machineId}`;
    case 'runtime':
      return `runtime:${subject.machineId}:${subject.runtimeId}`;
    case 'attachment':
      return `attachment:${subject.id}:${subject.filename ?? ''}`;
    case 'inbox':
      return `inbox:${subject.archived ? 'archived' : 'inbox'}:${subject.selectedKey ?? ''}`;
    case 'chat':
      return `chat:${subject.sessionId ?? ''}`;
    case 'flow':
      return `flow:${subject.flow}`;
    case 'unknown':
      return 'unknown';
  }
}
