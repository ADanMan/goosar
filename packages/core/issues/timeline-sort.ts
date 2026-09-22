import type { TimelineEntry } from '@goosar/core/types';

export function sortTimelineEntriesAsc(entries: TimelineEntry[]): TimelineEntry[] {
  entries.sort((a, b) => {
    if (a.created_at !== b.created_at) {
      return a.created_at < b.created_at ? -1 : 1;
    }
    return a.id < b.id ? -1 : 1;
  });
  return entries;
}
