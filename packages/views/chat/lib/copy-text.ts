import type { ChatMessage } from '@goosar/core/types';
import type { ChatTimelineItem } from '@goosar/core/chat';

export function splitTimeline(items: ChatTimelineItem[]): {
  preface: ChatTimelineItem[];
  middle: ChatTimelineItem[];
  final: ChatTimelineItem[];
} {
  const firstNonTextIdx = items.findIndex((i) => i.type !== 'text');
  if (firstNonTextIdx === -1) {
    return { preface: [], middle: [], final: items };
  }
  let lastNonTextIdx = items.length - 1;
  while (lastNonTextIdx >= 0 && items[lastNonTextIdx]!.type === 'text') {
    lastNonTextIdx--;
  }
  return {
    preface: items.slice(0, firstNonTextIdx),
    middle: items.slice(firstNonTextIdx, lastNonTextIdx + 1),
    final: items.slice(lastNonTextIdx + 1),
  };
}

export function extractCopyText(message: ChatMessage, timeline: ChatTimelineItem[]): string {
  if (timeline.length === 0) return message.content ?? '';
  const { preface, final } = splitTimeline(timeline);
  const pieces = [...preface, ...final].map((i) => i.content ?? '').filter((s) => s.length > 0);
  if (pieces.length === 0) return message.content ?? '';
  return pieces.join('\n\n');
}
