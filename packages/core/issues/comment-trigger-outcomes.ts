import { CommentTriggerOutcomeSchema } from '../api/schemas';
import type { CommentTriggerOutcome } from '../types/comment';

const MENTION_MARKUP_SOURCE =
  '\\[@?(.+?)\\]\\(mention:\\/\\/(member|agent|squad|issue|all)\\/([0-9a-fA-F-]+|all)\\)';

export interface ParsedMention {
  label: string;
  type: string;
  id: string;
}

export function parseMentions(content: string): ParsedMention[] {
  const re = new RegExp(MENTION_MARKUP_SOURCE, 'g');
  const mentions: ParsedMention[] = [];
  for (const match of content.matchAll(re)) {
    const label = match[1];
    const type = match[2];
    const id = match[3];
    if (!label || !type || !id) continue;
    mentions.push({ label, type, id });
  }
  return mentions;
}

export function mentionLabelsByTarget(content: string): Map<string, string> {
  const labels = new Map<string, string>();
  for (const { label, type, id } of parseMentions(content)) {
    labels.set(`${type}:${id}`, label);
  }
  return labels;
}

export function blockedTriggerLabel(
  outcome: { target_type: string; target_id: string },
  labels: Map<string, string>,
): string | undefined {
  return labels.get(`${outcome.target_type}:${outcome.target_id}`);
}

export function parseCommentTriggerOutcomes(raw: unknown): CommentTriggerOutcome[] {
  if (!Array.isArray(raw)) return [];
  const out: CommentTriggerOutcome[] = [];
  for (const item of raw) {
    const parsed = CommentTriggerOutcomeSchema.safeParse(item);
    if (parsed.success) {
      out.push(parsed.data as CommentTriggerOutcome);
    }
  }
  return out;
}

const HANDLED_TRIGGER_STATUSES = new Set(['queued', 'coalesced', 'deferred']);

export function unhandledCommentTriggerOutcomes(raw: unknown): CommentTriggerOutcome[] {
  return parseCommentTriggerOutcomes(raw).filter((o) => !HANDLED_TRIGGER_STATUSES.has(o.status));
}
