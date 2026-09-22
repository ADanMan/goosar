// Патчеры WS-кэша задач: чистые функции над QueryClient, вызываемые
// из use-issue-realtime и use-my-issues-realtime.
import type { QueryClient } from "@tanstack/react-query";
import type {
  Comment,
  Issue,
  IssueReaction,
  Label,
  Reaction,
  TimelineEntry,
} from "@goosar/core/types";
import { issueKeys } from "@/data/queries/issue-keys";

type TimelinePredicate = (entry: TimelineEntry) => boolean;
type TimelineMutate = (entry: TimelineEntry) => TimelineEntry;

export function patchIssueDetail(
  qc: QueryClient,
  wsId: string,
  partial: Partial<Issue> & { id: string },
) {
  qc.setQueryData<Issue>(issueKeys.detail(wsId, partial.id), (old) =>
    old ? { ...old, ...partial } : old,
  );
}

export function clearIssueDetail(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  qc.removeQueries({ queryKey: issueKeys.detail(wsId, issueId) });
  qc.removeQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
}

export function appendTimelineEntry(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  entry: TimelineEntry,
) {
  qc.setQueryData<TimelineEntry[]>(
    issueKeys.timeline(wsId, issueId),
    (old) => {
      if (!old) return old;
      if (old.some((e) => e.id === entry.id && e.type === entry.type)) {
        return old;
      }
      const next = [...old, entry];
      next.sort((a, b) => {
        if (a.created_at !== b.created_at) return a.created_at < b.created_at ? -1 : 1;
        return a.id < b.id ? -1 : 1;
      });
      return next;
    },
  );
}

export function patchTimelineEntry(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  predicate: TimelinePredicate,
  mutate: TimelineMutate,
) {
  qc.setQueryData<TimelineEntry[]>(
    issueKeys.timeline(wsId, issueId),
    (old) => (old ? old.map((e) => (predicate(e) ? mutate(e) : e)) : old),
  );
}

export function removeTimelineEntry(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  predicate: TimelinePredicate,
) {
  qc.setQueryData<TimelineEntry[]>(
    issueKeys.timeline(wsId, issueId),
    (old) => (old ? old.filter((e) => !predicate(e)) : old),
  );
}

export function removeCommentCascade(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  commentId: string,
) {
  qc.setQueryData<TimelineEntry[]>(
    issueKeys.timeline(wsId, issueId),
    (old) => {
      if (!old) return old;
      const removed = new Set<string>([commentId]);
      let changed = true;
      while (changed) {
        changed = false;
        for (const e of old) {
          if (
            e.type === "comment" &&
            e.parent_id &&
            removed.has(e.parent_id) &&
            !removed.has(e.id)
          ) {
            removed.add(e.id);
            changed = true;
          }
        }
      }
      return old.filter(
        (e) => !(e.type === "comment" && removed.has(e.id)),
      );
    },
  );
}

export function patchMyIssuesList(
  qc: QueryClient,
  wsId: string,
  partial: Partial<Issue> & { id: string },
) {
  qc.setQueriesData<Issue[]>({ queryKey: issueKeys.myAll(wsId) }, (old) =>
    old ? old.map((i) => (i.id === partial.id ? { ...i, ...partial } : i)) : old,
  );
}

export function removeFromMyIssuesList(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  qc.setQueriesData<Issue[]>({ queryKey: issueKeys.myAll(wsId) }, (old) =>
    old ? old.filter((i) => i.id !== issueId) : old,
  );
}

export function patchIssuesList(
  qc: QueryClient,
  wsId: string,
  partial: Partial<Issue> & { id: string },
) {
  qc.setQueryData<Issue[]>(issueKeys.list(wsId), (old) =>
    old ? old.map((i) => (i.id === partial.id ? { ...i, ...partial } : i)) : old,
  );
}

export function prependToIssuesList(
  qc: QueryClient,
  wsId: string,
  issue: Issue,
) {
  qc.setQueryData<Issue[]>(issueKeys.list(wsId), (old) => {
    if (!old) return old;
    if (old.some((i) => i.id === issue.id)) return old;
    return [issue, ...old];
  });
}

export function removeFromIssuesList(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  qc.setQueryData<Issue[]>(issueKeys.list(wsId), (old) =>
    old ? old.filter((i) => i.id !== issueId) : old,
  );
}

export function addCommentReaction(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  commentId: string,
  reaction: Reaction,
) {
  patchTimelineEntry(
    qc,
    wsId,
    issueId,
    (e) => e.type === "comment" && e.id === commentId,
    (e) => ({
      ...e,
      reactions: [
        ...(e.reactions ?? []).filter((r) => r.id !== reaction.id),
        reaction,
      ],
    }),
  );
}

export function removeCommentReaction(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  commentId: string,
  emoji: string,
  actorId: string,
) {
  patchTimelineEntry(
    qc,
    wsId,
    issueId,
    (e) => e.type === "comment" && e.id === commentId,
    (e) => ({
      ...e,
      reactions: (e.reactions ?? []).filter(
        (r) => !(r.emoji === emoji && r.actor_id === actorId),
      ),
    }),
  );
}

export function addIssueReaction(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  reaction: IssueReaction,
) {
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) => {
    if (!old) return old;
    const existing = old.reactions ?? [];
    if (existing.some((r) => r.id === reaction.id)) return old;
    return { ...old, reactions: [...existing, reaction] };
  });
}

export function removeIssueReaction(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  emoji: string,
  actorId: string,
) {
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old
      ? {
          ...old,
          reactions: (old.reactions ?? []).filter(
            (r) => !(r.emoji === emoji && r.actor_id === actorId),
          ),
        }
      : old,
  );
}

export function patchIssueLabels(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  labels: Label[],
) {
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old ? { ...old, labels } : old,
  );
  qc.setQueriesData<Issue[]>({ queryKey: issueKeys.myAll(wsId) }, (old) =>
    old
      ? old.map((i) => (i.id === issueId ? { ...i, labels } : i))
      : old,
  );
  qc.setQueryData<Issue[]>(issueKeys.list(wsId), (old) =>
    old
      ? old.map((i) => (i.id === issueId ? { ...i, labels } : i))
      : old,
  );
}

export function commentToTimelineEntry(comment: Comment): TimelineEntry {
  return {
    type: "comment",
    id: comment.id,
    actor_type: comment.author_type,
    actor_id: comment.author_id,
    created_at: comment.created_at,
    content: comment.content,
    parent_id: comment.parent_id,
    updated_at: comment.updated_at,
    comment_type: comment.type,
    reactions: comment.reactions,
    attachments: comment.attachments,
    resolved_at: comment.resolved_at,
    resolved_by_type: comment.resolved_by_type,
    resolved_by_id: comment.resolved_by_id,
    source_task_id: comment.source_task_id,
  };
}
