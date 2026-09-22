// Realtime одной задачи: монтируется экраном задачи, обновляет детали и ленту,
// очищается при уходе с экрана.
import { useQueryClient } from "@tanstack/react-query";
import type {
  TaskCancelledPayload,
  TaskCompletedPayload,
  TaskDispatchPayload,
  TaskFailedPayload,
  TaskMessagePayload,
  TaskQueuedPayload,
} from "@goosar/core/types";
import { issueKeys } from "@/data/queries/issue-keys";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  addCommentReaction,
  addIssueReaction,
  appendTimelineEntry,
  clearIssueDetail,
  commentToTimelineEntry,
  patchIssueDetail,
  patchIssueLabels,
  patchMyIssuesList,
  patchTimelineEntry,
  removeCommentCascade,
  removeCommentReaction,
  removeFromMyIssuesList,
  removeIssueReaction,
} from "./issue-ws-updaters";

type TaskEventPayload =
  | TaskQueuedPayload
  | TaskDispatchPayload
  | TaskCompletedPayload
  | TaskFailedPayload
  | TaskCancelledPayload
  | TaskMessagePayload;

export function useIssueRealtime(
  issueId: string | undefined,
  onDeleted?: () => void,
) {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      if (!issueId) return;

      const invalidateThisIssue = () => {
        qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
        qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
      };

      const invalidateTaskQueries = () => {
        qc.invalidateQueries({ queryKey: issueKeys.activeTasks(wsId, issueId) });
        qc.invalidateQueries({ queryKey: issueKeys.tasks(wsId, issueId) });
      };

      const onTaskEvent = (p: unknown) => {
        if ((p as TaskEventPayload).issue_id !== issueId) return;
        invalidateThisIssue();
        invalidateTaskQueries();
      };

      return [
        ws.on("issue:updated", (payload) => {
          if (payload.issue.id !== issueId) return;
          patchIssueDetail(qc, wsId, payload.issue);
          patchMyIssuesList(qc, wsId, payload.issue);
        }),
        ws.on("issue:deleted", (payload) => {
          if (payload.issue_id !== issueId) return;
          clearIssueDetail(qc, wsId, issueId);
          removeFromMyIssuesList(qc, wsId, issueId);
          onDeleted?.();
        }),
        ws.on("issue_labels:changed", (payload) => {
          if (payload.issue_id !== issueId) return;
          patchIssueLabels(qc, wsId, issueId, payload.labels);
        }),

        ws.on("comment:created", (payload) => {
          if (payload.comment.issue_id !== issueId) return;
          appendTimelineEntry(
            qc,
            wsId,
            issueId,
            commentToTimelineEntry(payload.comment),
          );
        }),
        ws.on("comment:updated", (payload) => {
          if (payload.comment.issue_id !== issueId) return;
          const entry = commentToTimelineEntry(payload.comment);
          patchTimelineEntry(
            qc,
            wsId,
            issueId,
            (e) => e.type === "comment" && e.id === payload.comment.id,
            () => entry,
          );
        }),
        ws.on("comment:resolved", (payload) => {
          if (payload.comment.issue_id !== issueId) return;
          const entry = commentToTimelineEntry(payload.comment);
          patchTimelineEntry(
            qc,
            wsId,
            issueId,
            (e) => e.type === "comment" && e.id === payload.comment.id,
            () => entry,
          );
        }),
        ws.on("comment:unresolved", (payload) => {
          if (payload.comment.issue_id !== issueId) return;
          const entry = commentToTimelineEntry(payload.comment);
          patchTimelineEntry(
            qc,
            wsId,
            issueId,
            (e) => e.type === "comment" && e.id === payload.comment.id,
            () => entry,
          );
        }),
        ws.on("comment:deleted", (payload) => {
          if (payload.issue_id !== issueId) return;
          removeCommentCascade(qc, wsId, issueId, payload.comment_id);
        }),
        ws.on("activity:created", (payload) => {
          if (payload.issue_id !== issueId) return;
          appendTimelineEntry(qc, wsId, issueId, payload.entry);
        }),

        ws.on("reaction:added", (payload) => {
          if (payload.issue_id !== issueId) return;
          addCommentReaction(
            qc,
            wsId,
            issueId,
            payload.reaction.comment_id,
            payload.reaction,
          );
        }),
        ws.on("reaction:removed", (payload) => {
          if (payload.issue_id !== issueId) return;
          removeCommentReaction(
            qc,
            wsId,
            issueId,
            payload.comment_id,
            payload.emoji,
            payload.actor_id,
          );
        }),

        ws.on("issue_reaction:added", (payload) => {
          if (payload.issue_id !== issueId) return;
          addIssueReaction(qc, wsId, issueId, payload.reaction);
        }),
        ws.on("issue_reaction:removed", (payload) => {
          if (payload.issue_id !== issueId) return;
          removeIssueReaction(
            qc,
            wsId,
            issueId,
            payload.emoji,
            payload.actor_id,
          );
        }),

        ws.on("task:queued", onTaskEvent),
        ws.on("task:dispatch", onTaskEvent),
        ws.on("task:progress", onTaskEvent),
        ws.on("task:completed", onTaskEvent),
        ws.on("task:failed", onTaskEvent),
        ws.on("task:cancelled", onTaskEvent),

        ws.onReconnect(() => {
          invalidateThisIssue();
          invalidateTaskQueries();
        }),
      ];
    },
    [issueId, qc, onDeleted],
  );
}
