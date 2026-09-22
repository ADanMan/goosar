/**
 * Мутация создания комментария. Работает с плоским кэшем таймлайна
 * (`TimelineEntry[]`, по возрастанию времени): отменяет текущие запросы,
 * сохраняет снимок кэша, добавляет синтетическую запись в конец списка;
 * при ошибке — откат к снимку, при завершении — инвалидация, чтобы
 * синтетическая запись заменилась настоящей с сервера.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  AgentTask,
  CreateIssueRequest,
  Issue,
  IssueReaction,
  Label,
  Reaction,
  TimelineEntry,
  UpdateIssueRequest,
} from "@goosar/core/types";
import { api } from "@/data/api";
import { issueKeys } from "@/data/queries/issues";
import { inboxKeys } from "@/data/queries/inbox";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useFailedCommentsStore } from "@/data/stores/failed-comments-store";

export type ToggleCommentReactionVars = {
  commentId: string;
  emoji: string;
  existing?: Reaction;
};

export type ToggleIssueReactionVars = {
  emoji: string;
  existing?: IssueReaction;
};

export type CreateCommentVars = {
  content: string;
  parentId?: string;
  attachmentIds?: string[];
};

export function useCreateComment(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  return useMutation({
    mutationFn: ({ content, parentId, attachmentIds }: CreateCommentVars) =>
      api.createComment(issueId, content, { parentId, attachmentIds }),
    onMutate: async ({ content, parentId }) => {
      const key = issueKeys.timeline(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<TimelineEntry[]>(key);
      if (!userId) return { prev, key, optimisticId: null };

      const optimisticId = `optimistic-${Date.now()}`;
      const optimistic: TimelineEntry = {
        type: "comment",
        id: optimisticId,
        actor_type: "member",
        actor_id: userId,
        content,
        parent_id: parentId ?? null,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        comment_type: "comment",
        reactions: [],
        attachments: [],
      };

      qc.setQueryData<TimelineEntry[]>(key, (old) =>
        old ? [...old, optimistic] : [optimistic],
      );

      return { prev, key, optimisticId };
    },
    onError: (err, vars, ctx) => {
      if (ctx?.optimisticId) {
        useFailedCommentsStore.getState().markFailed(ctx.optimisticId, {
          content: vars.content,
          parentId: vars.parentId,
          attachmentIds: vars.attachmentIds,
          error: err instanceof Error ? err.message : "Send failed",
        });
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: issueKeys.timeline(wsId, issueId),
      });
    },
  });
}

export function discardFailedComment(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  issueId: string,
  optimisticId: string,
) {
  qc.setQueryData<TimelineEntry[]>(
    issueKeys.timeline(wsId, issueId),
    (old) => (old ? old.filter((e) => e.id !== optimisticId) : old),
  );
  useFailedCommentsStore.getState().clear(optimisticId);
}

export function useToggleCommentReaction(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  return useMutation({
    mutationKey: ["toggleCommentReaction", issueId] as const,
    mutationFn: async ({
      commentId,
      emoji,
      existing,
    }: ToggleCommentReactionVars) => {
      if (existing) {
        await api.removeReaction(commentId, emoji);
        return null;
      }
      return api.addReaction(commentId, emoji);
    },
    onMutate: async ({ commentId, emoji, existing }) => {
      const key = issueKeys.timeline(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<TimelineEntry[]>(key);
      if (!userId) return { prev, key };

      qc.setQueryData<TimelineEntry[]>(key, (old) => {
        if (!old) return old;
        return old.map((entry) => {
          if (entry.id !== commentId) return entry;
          const reactions = entry.reactions ?? [];
          if (existing) {
            return {
              ...entry,
              reactions: reactions.filter((r) => r.id !== existing.id),
            };
          }
          const optimistic: Reaction = {
            id: `optimistic-${emoji}-${Date.now()}`,
            comment_id: commentId,
            actor_type: "member",
            actor_id: userId,
            emoji,
            created_at: new Date().toISOString(),
          };
          return { ...entry, reactions: [...reactions, optimistic] };
        });
      });
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
    },
  });
}

export function useEditComment(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: ({
      commentId,
      content,
      attachmentIds,
    }: {
      commentId: string;
      content: string;
      attachmentIds?: string[];
    }) => api.updateComment(commentId, content, attachmentIds),
    onMutate: async ({ commentId, content }) => {
      const key = issueKeys.timeline(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<TimelineEntry[]>(key);
      qc.setQueryData<TimelineEntry[]>(key, (old) =>
        old?.map((entry) =>
          entry.type === "comment" && entry.id === commentId
            ? {
                ...entry,
                content,
                updated_at: new Date().toISOString(),
              }
            : entry,
        ),
      );
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
    },
  });
}

export function useDeleteComment(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (commentId: string) => api.deleteComment(commentId),
    onMutate: async (commentId) => {
      const key = issueKeys.timeline(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<TimelineEntry[]>(key);
      qc.setQueryData<TimelineEntry[]>(key, (old) =>
        old?.filter(
          (entry) =>
            !(
              entry.type === "comment" &&
              (entry.id === commentId || entry.parent_id === commentId)
            ),
        ),
      );
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
    },
  });
}

export function useResolveComment(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: ({
      commentId,
      resolved,
    }: {
      commentId: string;
      resolved: boolean;
    }) =>
      resolved
        ? api.resolveComment(commentId)
        : api.unresolveComment(commentId),
    onMutate: async ({ commentId, resolved }) => {
      const key = issueKeys.timeline(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<TimelineEntry[]>(key);
      const now = resolved ? new Date().toISOString() : null;
      qc.setQueryData<TimelineEntry[]>(key, (old) =>
        old?.map((entry) =>
          entry.type === "comment" && entry.id === commentId
            ? { ...entry, resolved_at: now }
            : entry,
        ),
      );
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
    },
  });
}

export function useToggleIssueReaction(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  return useMutation({
    mutationKey: ["toggleIssueReaction", issueId] as const,
    mutationFn: async ({ emoji, existing }: ToggleIssueReactionVars) => {
      if (existing) {
        await api.removeIssueReaction(issueId, emoji);
        return null;
      }
      return api.addIssueReaction(issueId, emoji);
    },
    onMutate: async ({ emoji, existing }) => {
      const key = issueKeys.detail(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<Issue>(key);
      if (!userId || !prev) return { prev, key };

      const reactions = prev.reactions ?? [];
      let nextReactions: IssueReaction[];
      if (existing) {
        nextReactions = reactions.filter((r) => r.id !== existing.id);
      } else {
        const optimistic: IssueReaction = {
          id: `optimistic-${emoji}-${Date.now()}`,
          issue_id: issueId,
          actor_type: "member",
          actor_id: userId,
          emoji,
          created_at: new Date().toISOString(),
        };
        nextReactions = [...reactions, optimistic];
      }
      qc.setQueryData<Issue>(key, { ...prev, reactions: nextReactions });
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
    },
  });
}

export function useUpdateIssue(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationKey: ["updateIssue", issueId] as const,
    mutationFn: (patch: UpdateIssueRequest) => api.updateIssue(issueId, patch),
    onMutate: async (patch) => {
      const key = issueKeys.detail(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<Issue>(key);
      if (prev) {
        qc.setQueryData<Issue>(key, { ...prev, ...patch });
      }
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSuccess: (server) => {
      qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), server);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
    },
  });
}

export function useAttachLabel(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationKey: ["attachLabel", issueId] as const,
    mutationFn: ({ label }: { label: Label }) =>
      api.attachLabel(issueId, label.id),
    onMutate: async ({ label }) => {
      const key = issueKeys.detail(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<Issue>(key);
      if (prev) {
        const existing = prev.labels ?? [];
        if (!existing.some((l) => l.id === label.id)) {
          qc.setQueryData<Issue>(key, {
            ...prev,
            labels: [...existing, label],
          });
        }
      }
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSuccess: (server) => {
      const key = issueKeys.detail(wsId, issueId);
      const current = qc.getQueryData<Issue>(key);
      if (current) {
        qc.setQueryData<Issue>(key, { ...current, labels: server.labels });
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
    },
  });
}

export function useDetachLabel(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationKey: ["detachLabel", issueId] as const,
    mutationFn: ({ labelId }: { labelId: string }) =>
      api.detachLabel(issueId, labelId),
    onMutate: async ({ labelId }) => {
      const key = issueKeys.detail(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<Issue>(key);
      if (prev) {
        const existing = prev.labels ?? [];
        qc.setQueryData<Issue>(key, {
          ...prev,
          labels: existing.filter((l) => l.id !== labelId),
        });
      }
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSuccess: (server) => {
      const key = issueKeys.detail(wsId, issueId);
      const current = qc.getQueryData<Issue>(key);
      if (current) {
        qc.setQueryData<Issue>(key, { ...current, labels: server.labels });
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
    },
  });
}

export function useCreateIssue() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (body: CreateIssueRequest) => api.createIssue(body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
      qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
    },
  });
}

export function useDeleteIssue() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (id: string) => api.deleteIssue(id),
    onMutate: async (id) => {
      const listKey = issueKeys.list(wsId);
      const myAllKey = issueKeys.myAll(wsId);
      await Promise.all([
        qc.cancelQueries({ queryKey: listKey }),
        qc.cancelQueries({ queryKey: myAllKey }),
      ]);

      const prevList = qc.getQueryData<Issue[]>(listKey);
      const prevMy = qc.getQueriesData<Issue[]>({ queryKey: myAllKey });

      qc.setQueryData<Issue[]>(listKey, (old) =>
        old ? old.filter((i) => i.id !== id) : old,
      );
      qc.setQueriesData<Issue[]>({ queryKey: myAllKey }, (old) =>
        old ? old.filter((i) => i.id !== id) : old,
      );

      return { prevList, prevMy, listKey, myAllKey };
    },
    onError: (_err, _id, ctx) => {
      if (!ctx) return;
      if (ctx.prevList !== undefined) {
        qc.setQueryData(ctx.listKey, ctx.prevList);
      }
      for (const [key, value] of ctx.prevMy) {
        qc.setQueryData(key, value);
      }
    },
    onSettled: (_data, _err, id) => {
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
      qc.removeQueries({ queryKey: issueKeys.detail(wsId, id) });
      qc.removeQueries({ queryKey: issueKeys.timeline(wsId, id) });
      qc.removeQueries({ queryKey: issueKeys.activeTasks(wsId, id) });
      qc.removeQueries({ queryKey: issueKeys.tasks(wsId, id) });
    },
  });
}

export function useCancelTask(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (taskId: string) => api.cancelTaskById(taskId),
    onMutate: async (taskId) => {
      const activeKey = issueKeys.activeTasks(wsId, issueId);
      await qc.cancelQueries({ queryKey: activeKey });
      const prev = qc.getQueryData<AgentTask[]>(activeKey);
      qc.setQueryData<AgentTask[]>(activeKey, (old) =>
        old ? old.filter((t) => t.id !== taskId) : old,
      );
      return { prev, activeKey };
    },
    onError: (_err, _taskId, ctx) => {
      if (ctx?.prev) qc.setQueryData(ctx.activeKey, ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.activeTasks(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.tasks(wsId, issueId) });
    },
  });
}
