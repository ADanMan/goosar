// Группировка записей ленты по веткам ответов для плоского мобильного списка.
import type { TimelineEntry } from '@goosar/core/types';

export interface TimelineRow {
  entry: TimelineEntry;
  replies: TimelineEntry[];
}

export function buildTimelineRows(entries: TimelineEntry[]): TimelineRow[] {
  const commentIds = new Set<string>();
  for (const e of entries) {
    if (e.type === 'comment') commentIds.add(e.id);
  }

  const topLevel: TimelineEntry[] = [];
  const childrenByParent = new Map<string, TimelineEntry[]>();

  for (const e of entries) {
    if (e.type === 'comment' && e.parent_id && commentIds.has(e.parent_id)) {
      const list = childrenByParent.get(e.parent_id) ?? [];
      list.push(e);
      childrenByParent.set(e.parent_id, list);
    } else {
      topLevel.push(e);
    }
  }

  function collectDescendants(parentId: string): TimelineEntry[] {
    const out: TimelineEntry[] = [];
    const queue: string[] = [parentId];
    while (queue.length > 0) {
      const pid = queue.shift()!;
      const kids = childrenByParent.get(pid);
      if (!kids) continue;
      for (const child of kids) {
        out.push(child);
        queue.push(child.id);
      }
    }
    return out;
  }

  return topLevel.map((entry) => ({
    entry,
    replies: entry.type === 'comment' ? collectDescendants(entry.id) : [],
  }));
}
