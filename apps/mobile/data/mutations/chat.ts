/**
 * Мутации чата: создание сессии, удаление сессии, отметка о прочтении.
 * Отправка сообщения мутацией не является — экран чата реализует свой
 * оптимистичный сценарий вручную, см. экран вкладки чата.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { ChatSession } from "@goosar/core/types";
import { api } from "@/data/api";
import { useWorkspaceStore } from "@/data/workspace-store";
import { chatKeys } from "@/data/queries/chat";

export function useCreateChatSession() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (data: { agent_id: string; title?: string }) =>
      api.createChatSession(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    },
  });
}

export function useDeleteChatSession() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (id: string) => api.deleteChatSession(id),
    onMutate: async (id) => {
      const key = chatKeys.sessions(wsId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<ChatSession[]>(key);
      qc.setQueryData<ChatSession[]>(key, (old) =>
        old ? old.filter((s) => s.id !== id) : old,
      );
      return { prev, key };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(ctx.key, ctx.prev);
    },
    onSettled: (_data, _err, id) => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
      qc.removeQueries({ queryKey: chatKeys.messages(id) });
      qc.removeQueries({ queryKey: chatKeys.pendingTask(id) });
    },
  });
}

export function useMarkChatSessionRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (sessionId: string) => api.markChatSessionRead(sessionId),
    onMutate: async (sessionId) => {
      const key = chatKeys.sessions(wsId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<ChatSession[]>(key);
      qc.setQueryData<ChatSession[]>(key, (old) =>
        old?.map((s) =>
          s.id === sessionId
            ? { ...s, has_unread: false, unread_count: 0 }
            : s,
        ),
      );
      return { prev, key };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(ctx.key, ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    },
  });
}
