/**
 * Мутации инбокса. Отметка прочитанным применяется в кэш синхронно, до
 * отмены текущих запросов — это важно для iOS: анимация перехода на экран
 * задачи иначе захватывает ещё не обновлённое (непрочитанное) состояние
 * строки. Архивирование одной записи также помечает архивной все прочие
 * строки инбокса с тем же issue_id. Пакетное архивирование и "прочитать
 * всё" обновляют кэш без оптимистичного патча, только инвалидацией.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { InboxItem } from "@goosar/core/types";
import { api } from "@/data/api";
import { inboxKeys } from "@/data/queries/inbox";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useMarkInboxRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (id: string) => api.markInboxRead(id),
    onMutate: async (id) => {
      const key = inboxKeys.list(wsId);
      qc.setQueryData<InboxItem[]>(key, (old) =>
        old?.map((item) => (item.id === id ? { ...item, read: true } : item)),
      );
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<InboxItem[]>(key);
      return { prev, key };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(ctx.key, ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

export function useArchiveInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (id: string) => api.archiveInbox(id),
    onMutate: async (id) => {
      const key = inboxKeys.list(wsId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<InboxItem[]>(key);
      const target = prev?.find((i) => i.id === id);
      const issueId = target?.issue_id ?? null;
      qc.setQueryData<InboxItem[]>(key, (old) =>
        old?.map((item) =>
          item.id === id || (issueId && item.issue_id === issueId)
            ? { ...item, archived: true }
            : item,
        ),
      );
      return { prev, key };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(ctx.key, ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

export function useMarkAllInboxRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: () => api.markAllInboxRead(),
    onMutate: async () => {
      const key = inboxKeys.list(wsId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<InboxItem[]>(key);
      qc.setQueryData<InboxItem[]>(key, (old) =>
        old?.map((item) =>
          !item.archived ? { ...item, read: true } : item,
        ),
      );
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(ctx.key, ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

export function useArchiveAllInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: () => api.archiveAllInbox(),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

export function useArchiveAllReadInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: () => api.archiveAllReadInbox(),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

export function useArchiveCompletedInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: () => api.archiveCompletedInbox(),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}
