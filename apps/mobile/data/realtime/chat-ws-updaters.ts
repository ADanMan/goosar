// Патчеры WS-кэша для чата: чистые функции над QueryClient без React
// и WS-обвязки; их вызывают realtime-хуки.
import type { QueryClient } from "@tanstack/react-query";
import type {
  ChatDonePayload,
  ChatMessage,
  ChatPendingTask,
  ChatSession,
  ChatSessionDeletedPayload,
  TaskMessagePayload,
  TaskQueuedPayload,
  TaskDispatchPayload,
} from "@goosar/core/types";
import { chatKeys } from "@/data/queries/chat";

export function patchSessionListAfterRename(
  qc: QueryClient,
  wsId: string | null,
  payload: {
    chat_session_id: string;
    title?: string;
    updated_at?: string;
  },
) {
  qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), (old) =>
    old?.map((s) =>
      s.id === payload.chat_session_id
        ? {
            ...s,
            title: payload.title ?? s.title,
            updated_at: payload.updated_at ?? s.updated_at,
          }
        : s,
    ),
  );
}

export function dropSessionFromList(
  qc: QueryClient,
  wsId: string | null,
  payload: ChatSessionDeletedPayload,
) {
  qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), (old) =>
    old?.filter((s) => s.id !== payload.chat_session_id),
  );
  qc.removeQueries({ queryKey: chatKeys.messages(payload.chat_session_id) });
  qc.removeQueries({
    queryKey: chatKeys.pendingTask(payload.chat_session_id),
  });
}

export function flipSessionUnread(
  qc: QueryClient,
  wsId: string | null,
  sessionId: string,
  hasUnread: boolean,
) {
  qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), (old) =>
    old?.map((s) =>
      s.id === sessionId ? { ...s, has_unread: hasUnread } : s,
    ),
  );
}

export function applyChatDoneToCache(
  qc: QueryClient,
  payload: ChatDonePayload,
) {
  if (payload.message_id && payload.content != null && payload.created_at) {
    const assistantMsg: ChatMessage = {
      id: payload.message_id,
      chat_session_id: payload.chat_session_id,
      role: "assistant",
      content: payload.content,
      task_id: payload.task_id,
      created_at: payload.created_at,
      elapsed_ms: payload.elapsed_ms ?? null,
      message_kind: payload.message_kind ?? "message",
    };
    qc.setQueryData<ChatMessage[]>(
      chatKeys.messages(payload.chat_session_id),
      (old) => {
        if (!old) return [assistantMsg];
        if (old.some((m) => m.id === assistantMsg.id)) return old;
        return [...old, assistantMsg];
      },
    );
  }
  qc.invalidateQueries({
    queryKey: chatKeys.messages(payload.chat_session_id),
  });
  qc.setQueryData(chatKeys.pendingTask(payload.chat_session_id), {});
}

export function seedPendingTaskFromQueued(
  qc: QueryClient,
  payload: TaskQueuedPayload,
) {
  if (!payload.chat_session_id) return;
  qc.setQueryData<ChatPendingTask>(
    chatKeys.pendingTask(payload.chat_session_id),
    (old) => ({
      ...(old ?? {}),
      task_id: payload.task_id,
      status: "queued",
    }),
  );
}

export function promotePendingTaskToRunning(
  qc: QueryClient,
  payload: TaskDispatchPayload,
) {
  if (!payload.chat_session_id) return;
  qc.setQueryData<ChatPendingTask>(
    chatKeys.pendingTask(payload.chat_session_id),
    (old) => {
      if (!old || old.task_id !== payload.task_id) return old;
      return { ...old, status: "running" };
    },
  );
}

export function clearPendingTask(
  qc: QueryClient,
  sessionId: string,
) {
  qc.setQueryData(chatKeys.pendingTask(sessionId), {});
}

export function appendTaskMessage(
  qc: QueryClient,
  payload: TaskMessagePayload,
) {
  qc.setQueryData<TaskMessagePayload[]>(
    chatKeys.taskMessages(payload.task_id),
    (old = []) => {
      if (old.some((m) => m.seq === payload.seq)) return old;
      return [...old, payload].sort((a, b) => a.seq - b.seq);
    },
  );
}
