// Схлопывание подряд идущих одинаковых записей активности; правило зеркалит
// веб, чтобы число записей в ленте совпадало.
import type { TimelineEntry } from '@goosar/core/types';

const COALESCE_MS = 2 * 60 * 1000;
const NO_TIME_LIMIT_ACTIONS = new Set(['task_completed', 'task_failed']);
const NEVER_COALESCE_ACTIONS = new Set(['squad_leader_evaluated']);

export function coalesceTimeline(entries: TimelineEntry[]): TimelineEntry[] {
  const out: TimelineEntry[] = [];
  for (const entry of entries) {
    if (entry.type === 'activity') {
      const prev = out[out.length - 1];
      if (
        !NEVER_COALESCE_ACTIONS.has(entry.action ?? '') &&
        prev?.type === 'activity' &&
        prev.action === entry.action &&
        prev.actor_type === entry.actor_type &&
        prev.actor_id === entry.actor_id &&
        (NO_TIME_LIMIT_ACTIONS.has(entry.action ?? '') ||
          Math.abs(new Date(entry.created_at).getTime() - new Date(prev.created_at).getTime()) <=
            COALESCE_MS)
      ) {
        out[out.length - 1] = {
          ...entry,
          coalesced_count: (prev.coalesced_count ?? 1) + 1,
        };
        continue;
      }
    }
    out.push(entry);
  }
  return out;
}
