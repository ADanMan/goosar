'use client';

import { useEffect, useRef, useCallback, useMemo } from 'react';
import { useQuery, useQueryClient, useMutationState } from '@tanstack/react-query';
import type { Comment, TimelineEntry, Reaction } from '@goosar/core/types';
import type {
  CommentCreatedPayload,
  CommentUpdatedPayload,
  CommentDeletedPayload,
  CommentResolvedPayload,
  CommentUnresolvedPayload,
  ActivityCreatedPayload,
  ReactionAddedPayload,
  ReactionRemovedPayload,
} from '@goosar/core/types';
import { issueTimelineOptions, issueKeys } from '@goosar/core/issues/queries';
import {
  useCreateComment,
  useUpdateComment,
  useDeleteComment,
  useResolveComment,
  useToggleCommentReaction,
  type ToggleCommentReactionVars,
} from '@goosar/core/issues/mutations';
import { sortTimelineEntriesAsc } from '@goosar/core/issues/timeline-sort';
import {
  unhandledCommentTriggerOutcomes,
  mentionLabelsByTarget,
} from '@goosar/core/issues/comment-trigger-outcomes';
import { useWSEvent, useWSReconnect, useRealtimePollingInterval } from '@goosar/core/realtime';
import { toast } from 'sonner';
import { useT } from '../../i18n';
import { blockedShortReasonLabel } from '../blocked-trigger-copy';

type TLCache = TimelineEntry[];

function commentToTimelineEntry(c: Comment): TimelineEntry {
  return {
    type: 'comment',
    id: c.id,
    actor_type: c.author_type,
    actor_id: c.author_id,
    content: c.content,
    parent_id: c.parent_id,
    created_at: c.created_at,
    updated_at: c.updated_at,
    comment_type: c.type,
    reactions: c.reactions ?? [],
    attachments: c.attachments ?? [],
    resolved_at: c.resolved_at,
    resolved_by_type: c.resolved_by_type,
    resolved_by_id: c.resolved_by_id,
    source_task_id: c.source_task_id,
  };
}

export function useIssueTimeline(issueId: string, userId?: string) {
  const { t } = useT('issues');
  const qc = useQueryClient();

  const pollingInterval = useRealtimePollingInterval();
  const query = useQuery({
    ...issueTimelineOptions(issueId),
    refetchInterval: pollingInterval,
  });
  const { data, isLoading: loading } = query;

  const timeline = useMemo<TimelineEntry[]>(() => data ?? [], [data]);

  const { mutateAsync: createComment } = useCreateComment(issueId);
  const { mutateAsync: updateComment } = useUpdateComment(issueId);
  const { mutateAsync: deleteCommentAsync } = useDeleteComment(issueId);
  const { mutateAsync: resolveCommentAsync } = useResolveComment(issueId);
  const { mutate: toggleCommentReaction } = useToggleCommentReaction(issueId);

  useWSReconnect(
    useCallback(() => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
    }, [qc, issueId]),
  );

  useWSEvent(
    'comment:created',
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentCreatedPayload;
        if (comment.issue_id !== issueId) return;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) => {
          const entry = commentToTimelineEntry(comment);
          if (!old) return [entry];
          if (old.some((e) => e.id === comment.id)) return old;
          return sortTimelineEntriesAsc([...old, entry]);
        });
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    'comment:updated',
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentUpdatedPayload;
        if (comment.issue_id !== issueId) return;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) =>
          old?.map((e) => (e.id === comment.id ? commentToTimelineEntry(comment) : e)),
        );
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    'comment:resolved',
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentResolvedPayload;
        if (comment.issue_id !== issueId) return;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) =>
          old?.map((e) => (e.id === comment.id ? commentToTimelineEntry(comment) : e)),
        );
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    'comment:unresolved',
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentUnresolvedPayload;
        if (comment.issue_id !== issueId) return;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) =>
          old?.map((e) => (e.id === comment.id ? commentToTimelineEntry(comment) : e)),
        );
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    'comment:deleted',
    useCallback(
      (payload: unknown) => {
        const { comment_id, issue_id } = payload as CommentDeletedPayload;
        if (issue_id !== issueId) return;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) => {
          if (!old) return old;
          const idsToRemove = new Set<string>([comment_id]);
          let changed = true;
          while (changed) {
            changed = false;
            for (const e of old) {
              if (e.parent_id && idsToRemove.has(e.parent_id) && !idsToRemove.has(e.id)) {
                idsToRemove.add(e.id);
                changed = true;
              }
            }
          }
          return old.filter((e) => !idsToRemove.has(e.id));
        });
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    'activity:created',
    useCallback(
      (payload: unknown) => {
        const p = payload as ActivityCreatedPayload;
        if (p.issue_id !== issueId) return;
        const entry = p.entry;
        if (!entry || !entry.id) return;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) => {
          if (!old) return [entry];
          if (old.some((e) => e.id === entry.id)) return old;
          return sortTimelineEntriesAsc([...old, entry]);
        });
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    'reaction:added',
    useCallback(
      (payload: unknown) => {
        const { reaction, issue_id } = payload as ReactionAddedPayload;
        if (issue_id !== issueId) return;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) =>
          old?.map((e) => {
            if (e.id !== reaction.comment_id) return e;
            const existing = e.reactions ?? [];
            if (existing.some((r) => r.id === reaction.id)) return e;
            return { ...e, reactions: [...existing, reaction] };
          }),
        );
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    'reaction:removed',
    useCallback(
      (payload: unknown) => {
        const p = payload as ReactionRemovedPayload;
        if (p.issue_id !== issueId) return;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) =>
          old?.map((e) => {
            if (e.id !== p.comment_id) return e;
            return {
              ...e,
              reactions: (e.reactions ?? []).filter(
                (r) =>
                  !(
                    r.emoji === p.emoji &&
                    r.actor_type === p.actor_type &&
                    r.actor_id === p.actor_id
                  ),
              ),
            };
          }),
        );
      },
      [qc, issueId],
    ),
  );

  const warnUnhandledTriggers = useCallback(
    (triggerOutcomes: unknown, content?: string) => {
      const unhandled = unhandledCommentTriggerOutcomes(triggerOutcomes);
      if (unhandled.length === 0) return;
      if (unhandled.length === 1) {
        const outcome = unhandled[0]!;
        const name = mentionLabelsByTarget(content ?? '').get(
          `${outcome.target_type}:${outcome.target_id}`,
        );
        if (name) {
          toast.warning(
            t(($) => $.comment.posted_partial_trigger_named, {
              name,
              reason: blockedShortReasonLabel(outcome.reason_code, t),
            }),
          );
          return;
        }
      }
      toast.warning(t(($) => $.comment.posted_partial_trigger, { count: unhandled.length }));
    },
    [t],
  );

  const submitComment = useCallback(
    async (
      content: string,
      attachmentIds?: string[],
      suppressAgentIds?: string[],
    ): Promise<boolean> => {
      if (!content.trim() || !userId) return false;
      try {
        const comment = await createComment({ content, attachmentIds, suppressAgentIds });
        warnUnhandledTriggers(comment?.trigger_outcomes, comment?.content);
        return true;
      } catch (err) {
        toast.error(
          err instanceof Error && err.message ? err.message : t(($) => $.comment.send_failed),
        );
        return false;
      }
    },
    [userId, createComment, warnUnhandledTriggers, t],
  );

  const submitReply = useCallback(
    async (
      parentId: string,
      content: string,
      attachmentIds?: string[],
      suppressAgentIds?: string[],
    ): Promise<boolean> => {
      if (!content.trim() || !userId) return false;
      try {
        const comment = await createComment({
          content,
          type: 'comment',
          parentId,
          attachmentIds,
          suppressAgentIds,
        });
        warnUnhandledTriggers(comment?.trigger_outcomes, comment?.content);
        return true;
      } catch (err) {
        toast.error(
          err instanceof Error && err.message ? err.message : t(($) => $.comment.send_reply_failed),
        );
        return false;
      }
    },
    [userId, createComment, warnUnhandledTriggers, t],
  );

  const editComment = useCallback(
    async (
      commentId: string,
      content: string,
      attachmentIds: string[],
      suppressAgentIds?: string[],
    ) => {
      try {
        const comment = await updateComment({
          commentId,
          content,
          attachmentIds,
          suppressAgentIds,
        });
        warnUnhandledTriggers(comment?.trigger_outcomes, comment?.content);
      } catch (err) {
        toast.error(
          err instanceof Error && err.message ? err.message : t(($) => $.comment.update_failed),
        );
      }
    },
    [updateComment, warnUnhandledTriggers, t],
  );

  const deleteComment = useCallback(
    async (commentId: string) => {
      try {
        await deleteCommentAsync(commentId);
      } catch (err) {
        toast.error(
          err instanceof Error && err.message ? err.message : t(($) => $.comment.delete_failed),
        );
      }
    },
    [deleteCommentAsync, t],
  );

  const toggleResolveComment = useCallback(
    async (commentId: string, resolved: boolean) => {
      try {
        await resolveCommentAsync({ commentId, resolved });
      } catch (err) {
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : resolved
              ? t(($) => $.comment.resolve.resolve_failed)
              : t(($) => $.comment.resolve.unresolve_failed),
        );
      }
    },
    [resolveCommentAsync, t],
  );

  const pendingReactionVars = useMutationState({
    filters: {
      mutationKey: ['toggleCommentReaction', issueId],
      status: 'pending',
    },
    select: (m) => m.state.variables as ToggleCommentReactionVars | undefined,
  });

  const optimisticTimeline = useMemo(() => {
    if (pendingReactionVars.length === 0) return timeline;

    return timeline.map((entry) => {
      const pendingForEntry = pendingReactionVars.filter((v) => v && v.commentId === entry.id);
      if (pendingForEntry.length === 0) return entry;

      let reactions = entry.reactions ?? [];
      for (const vars of pendingForEntry) {
        if (!vars) continue;
        if (vars.existing) {
          reactions = reactions.filter((r) => r.id !== vars.existing!.id);
        } else {
          const alreadyExists = reactions.some(
            (r) => r.emoji === vars.emoji && r.actor_type === 'member' && r.actor_id === userId,
          );
          if (!alreadyExists) {
            reactions = [
              ...reactions,
              {
                id: `optimistic-${vars.emoji}`,
                comment_id: vars.commentId,
                actor_type: 'member',
                actor_id: userId ?? '',
                emoji: vars.emoji,
                created_at: '',
              },
            ];
          }
        }
      }
      return { ...entry, reactions };
    });
  }, [timeline, pendingReactionVars, userId]);

  const timelineRef = useRef(timeline);
  useEffect(() => {
    timelineRef.current = timeline;
  }, [timeline]);

  const toggleReaction = useCallback(
    async (commentId: string, emoji: string) => {
      if (!userId) return;
      const entry = timelineRef.current.find((e) => e.id === commentId);
      const existing: Reaction | undefined = (entry?.reactions ?? []).find(
        (r) => r.emoji === emoji && r.actor_type === 'member' && r.actor_id === userId,
      );
      toggleCommentReaction({ commentId, emoji, existing });
    },
    [userId, toggleCommentReaction],
  );

  return {
    timeline: optimisticTimeline,
    loading,
    submitComment,
    submitReply,
    editComment,
    deleteComment,
    toggleResolveComment,
    toggleReaction,
  };
}
