import type { TimelineEntry } from '@goosar/core/types';
import { sortTimelineEntriesAsc } from '@goosar/core/issues/timeline-sort';

export function collectThreadReplies(
  rootId: string,
  repliesByParent: Map<string, TimelineEntry[]>,
): TimelineEntry[] {
  const out: TimelineEntry[] = [];
  const walk = (id: string) => {
    const children = repliesByParent.get(id) ?? [];
    for (const child of children) {
      out.push(child);
      walk(child.id);
    }
  };
  walk(rootId);
  return sortTimelineEntriesAsc(out);
}

export type ThreadResolution =
  { kind: 'none' } | { kind: 'root' } | { kind: 'reply'; resolutionId: string };

export function deriveThreadResolution(
  root: TimelineEntry,
  replies: TimelineEntry[],
): ThreadResolution {
  if (root.resolved_at) return { kind: 'root' };
  let chosen: TimelineEntry | null = null;
  for (const reply of replies) {
    if (!reply.resolved_at) continue;
    if (!chosen || reply.resolved_at > chosen.resolved_at!) chosen = reply;
  }
  return chosen ? { kind: 'reply', resolutionId: chosen.id } : { kind: 'none' };
}

export function rootCommentIds(entries: readonly TimelineEntry[]): string[] {
  return entries.filter((e) => e.type === 'comment' && !e.parent_id).map((e) => e.id);
}

export function resolvedThreadRootIds(entries: readonly TimelineEntry[]): string[] {
  const roots: TimelineEntry[] = [];
  const repliesByParent = new Map<string, TimelineEntry[]>();
  for (const e of entries) {
    if (e.type !== 'comment') continue;
    if (!e.parent_id) {
      roots.push(e);
    } else {
      const list = repliesByParent.get(e.parent_id) ?? [];
      list.push(e);
      repliesByParent.set(e.parent_id, list);
    }
  }
  return roots
    .filter(
      (root) =>
        deriveThreadResolution(root, collectThreadReplies(root.id, repliesByParent)).kind !==
        'none',
    )
    .map((root) => root.id);
}
